package handler

import (
	"errors"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 兑换/注册相关业务错误
var (
	ErrCardInvalid     = errors.New("card invalid")
	ErrCardUsed        = errors.New("card already used")
	ErrCardTypeDeleted = errors.New("card type deleted")
	ErrCardProjectMism = errors.New("card project mismatch")
	ErrUsernameExists  = errors.New("username exists")
)

// ExchangeRequest 兑换卡密请求
type ExchangeRequest struct {
	ProjectID string `json:"project_id" binding:"required"` // 项目 id（6 位数字，卡密需属于该项目）
	CDKey     string `json:"cdkey" binding:"required"`
	Username  string `json:"username" binding:"required"` // 兑换到哪个用户名（无需登录）
}

// Exchange 兑换（激活）卡密【无需登录，按用户名兑换】。卡密必须属于请求的项目。
func (h *Handler) Exchange(c *gin.Context) {
	var req ExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：username、project_id、cdkey 不能为空")
		return
	}
	cdkey := strings.TrimSpace(req.CDKey)
	req.Username = strings.TrimSpace(req.Username)
	if cdkey == "" || req.Username == "" {
		util.Fail(c, util.CodeParamError, "参数错误：username、cdkey 不能为空")
		return
	}

	now := timeNow()
	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if user.Status == model.UserStatusFrozen {
		util.Fail(c, util.CodeAccountLocked, "账号已被冻结")
		return
	}

	var ct *model.CardType
	var cardID uint64
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		ct, cardID, err = redeemCard(tx, &user, cdkey, req.ProjectID, now)
		return err
	})
	if err != nil {
		code, msg := mapRedeemError(err)
		util.Fail(c, code, msg)
		return
	}

	ent := getEntitlement(user.ID, req.ProjectID)
	kind, amount := ct.KindAmount()
	// 扣费流水：卡密兑换进账本（reason=card），ref_id 指向用掉的那张卡
	balanceAfter := 0
	switch kind {
	case model.CardKindCalls:
		if ent != nil {
			balanceAfter = ent.RemainingCalls
		}
	case model.CardKindCredits:
		if ent != nil {
			balanceAfter = ent.Credits
		}
	}
	h.logBilling(BillingEntry{
		ProjectID: req.ProjectID, UserID: user.ID, Username: user.Username,
		Kind: kind, Delta: amount, BalanceAfter: balanceAfter,
		Reason: model.BillReasonCard, RefID: cardID, Remark: "卡密兑换：" + ct.Name,
	})
	// v1 字段（days / expires_at）保持不变；v2 追加 kind/amount 与发放后的余额，供新客户端展示
	util.OK(c, gin.H{
		"type_id":         ct.ID,
		"type_name":       ct.Name,
		"days":            ct.Days,
		"project_id":      ct.ProjectID,
		"expires_at":      formatTimePtr(entExpiresAt(ent)),
		"kind":            kind,
		"amount":          amount,
		"remaining_calls": entRemainingCalls(ent),
		"credits":         entCredits(ent),
	})
}

// UserUnbindRequest 用户解绑请求
type UserUnbindRequest struct {
	ProjectID     string `json:"project_id" binding:"required"` // 按项目解绑
	Username      string `json:"username" binding:"required"`
	SuperPassword string `json:"super_password" binding:"required"`
}

// UserUnbind 用户解绑（按项目）：校验超级密码后清空该用户该项目下的机器码绑定。
func (h *Handler) UserUnbind(c *gin.Context) {
	var req UserUnbindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、username、super_password 不能为空")
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if !util.CheckPassword(user.SuperPassword, req.SuperPassword) {
		util.Fail(c, util.CodeInvalidCred, "超级密码错误")
		return
	}

	// 项目必须存在且未删除，读取解绑限制
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	// 自助解绑每日次数限制（管理员解绑不受此限制）
	if project.UnbindLimit > 0 && h.exceedDailyUnbindLimit(project.UnbindLimit, req.ProjectID, user.ID) {
		util.Fail(c, util.CodeUnbindLimitExceed, "今日自助解绑次数已达上限")
		return
	}

	if err := database.DB.Where("user_id = ? AND project_id = ?", user.ID, req.ProjectID).
		Delete(&model.UserBinding{}).Error; err != nil {
		util.Fail(c, util.CodeDBError, "解绑失败")
		return
	}
	util.OK(c, gin.H{"unbound": true})
}

// redeemCard 事务内兑换核心逻辑：
// 1. 行锁按 cdkey 查卡密，校验未被使用；
// 2. 校验卡密类型未删除；
// 3. 若 expectProjectID > 0，校验卡密所属项目一致；
// 4. 按类型发放额度：days 累加该项目到期时间（已过期从当前起算）、calls 加剩余次数、credits 加积分；
// 5. 更新用户权限 + 标记卡密已使用。
func redeemCard(tx *gorm.DB, user *model.User, cdkey string, expectProjectID string, now time.Time) (*model.CardType, uint64, error) {
	var card model.Card
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("cdkey = ?", cdkey).First(&card).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrCardInvalid
		}
		return nil, 0, err
	}
	if card.IsUsed() {
		return nil, 0, ErrCardUsed
	}

	var ct model.CardType
	if err := tx.Where("id = ? AND deleted_at IS NULL", card.TypeID).First(&ct).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrCardTypeDeleted
		}
		return nil, 0, err
	}
	if expectProjectID != "" && ct.ProjectID != expectProjectID {
		return nil, 0, ErrCardProjectMism
	}

	// 行锁用户行防并发累加错乱
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(user, user.ID).Error; err != nil {
		return nil, 0, err
	}

	ent, err := lockOrCreateEntitlement(tx, user.ID, ct.ProjectID)
	if err != nil {
		return nil, 0, err
	}

	// 按卡密类型发放：days 是 v1 老行为，calls/credits 是 v2 新增
	kind, amount := ct.KindAmount()
	// 三种模式都记录"最近一次充值用的哪张卡"，后台对账/追溯用
	updates := map[string]any{
		"last_card_id":     card.ID,
		"last_recharge_at": now,
	}
	switch kind {
	case model.CardKindCalls:
		ent.RemainingCalls += amount
		updates["remaining_calls"] = ent.RemainingCalls
	case model.CardKindCredits:
		ent.Credits += amount
		updates["credits"] = ent.Credits
	default:
		ent.AddDays(amount, now)
		updates["expires_at"] = ent.ExpiresAt
	}
	if err := tx.Model(&ent).Updates(updates).Error; err != nil {
		return nil, 0, err
	}

	uid := user.ID
	if err := tx.Model(&model.Card{}).Where("id = ?", card.ID).Updates(map[string]any{
		"user_id":        uid,
		"used_at":        now,
		"granted_kind":   kind,
		"granted_amount": amount,
	}).Error; err != nil {
		return nil, 0, err
	}
	return &ct, card.ID, nil
}

// lockOrCreateEntitlement 行锁读取用户在某项目的权限记录，不存在则创建
func lockOrCreateEntitlement(tx *gorm.DB, userID uint64, projectID string) (*model.UserEntitlement, error) {
	var ent model.UserEntitlement
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND project_id = ?", userID, projectID).First(&ent).Error
	if err == nil {
		return &ent, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	ent = model.UserEntitlement{
		UserID:    userID,
		ProjectID: projectID,
		ExpiresAt: time.Now().AddDate(0, 0, 0), // 占位，随后 AddDays 覆盖
	}
	if err := tx.Create(&ent).Error; err != nil {
		return nil, err
	}
	return &ent, nil
}

// getEntitlement 读取用户在某项目的权限记录（不存在返回 nil）
func getEntitlement(userID uint64, projectID string) *model.UserEntitlement {
	var ent model.UserEntitlement
	if err := database.DB.Where("user_id = ? AND project_id = ?", userID, projectID).First(&ent).Error; err != nil {
		return nil
	}
	return &ent
}

func entExpiresAt(ent *model.UserEntitlement) *time.Time {
	if ent == nil {
		return nil
	}
	return &ent.ExpiresAt
}

// entitlementExpiresAt 读取用户在某项目的到期时间（不存在返回 nil）
func entitlementExpiresAt(userID uint64, projectID string) *time.Time {
	return entExpiresAt(getEntitlement(userID, projectID))
}

// entRemainingCalls / entCredits 读取发放后的额度（ent 为 nil 时返回 0）
func entRemainingCalls(ent *model.UserEntitlement) int {
	if ent == nil {
		return 0
	}
	return ent.RemainingCalls
}

func entCredits(ent *model.UserEntitlement) int {
	if ent == nil {
		return 0
	}
	return ent.Credits
}

// mapRedeemError 兑换/注册业务的错误码映射
func mapRedeemError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrCardInvalid):
		return util.CodeCardInvalid, "卡密无效"
	case errors.Is(err, ErrCardUsed):
		return util.CodeCardUsed, "该卡密已被使用"
	case errors.Is(err, ErrCardTypeDeleted):
		return util.CodeCardTypeDeleted, "该卡密类型已停用"
	case errors.Is(err, ErrCardProjectMism):
		return util.CodeParamError, "卡密不属于该项目"
	case errors.Is(err, ErrUsernameExists):
		return util.CodeConflict, "用户名已存在"
	default:
		return util.CodeDBError, "系统错误"
	}
}
