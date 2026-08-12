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
	ErrCardInvalid       = errors.New("card invalid")
	ErrCardUsed          = errors.New("card already used")
	ErrCardTypeDeleted   = errors.New("card type deleted")
	ErrCardProjectMism   = errors.New("card project mismatch")
	ErrUsernameExists    = errors.New("username exists")
)

// ExchangeRequest 兑换卡密请求
type ExchangeRequest struct {
	ProjectID uint64 `json:"project_id" binding:"required"` // 项目 id（卡密需属于该项目）
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
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		ct, err = redeemCard(tx, &user, cdkey, req.ProjectID, now)
		return err
	})
	if err != nil {
		code, msg := mapRedeemError(err)
		util.Fail(c, code, msg)
		return
	}

	ent := getEntitlement(user.ID, req.ProjectID)
	util.OK(c, gin.H{
		"type_id":    ct.ID,
		"type_name":  ct.Name,
		"days":       ct.Days,
		"project_id": ct.ProjectID,
		"expires_at": formatTimePtr(entExpiresAt(ent)),
	})
}

// UserUnbindRequest 用户解绑请求
type UserUnbindRequest struct {
	ProjectID    uint64 `json:"project_id" binding:"required"` // 按项目解绑
	Username     string `json:"username" binding:"required"`
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
// 4. 按类型 days 累加用户在该项目下的到期时间（已过期从当前起算）；
// 5. 更新用户权限 + 标记卡密已使用。
func redeemCard(tx *gorm.DB, user *model.User, cdkey string, expectProjectID uint64, now time.Time) (*model.CardType, error) {
	var card model.Card
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("cdkey = ?", cdkey).First(&card).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCardInvalid
		}
		return nil, err
	}
	if card.IsUsed() {
		return nil, ErrCardUsed
	}

	var ct model.CardType
	if err := tx.Where("id = ? AND deleted_at IS NULL", card.TypeID).First(&ct).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCardTypeDeleted
		}
		return nil, err
	}
	if expectProjectID > 0 && ct.ProjectID != expectProjectID {
		return nil, ErrCardProjectMism
	}

	// 行锁用户行防并发累加错乱
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(user, user.ID).Error; err != nil {
		return nil, err
	}

	ent, err := lockOrCreateEntitlement(tx, user.ID, ct.ProjectID)
	if err != nil {
		return nil, err
	}
	ent.AddDays(ct.Days, now)
	if err := tx.Model(&ent).Update("expires_at", ent.ExpiresAt).Error; err != nil {
		return nil, err
	}

	uid := user.ID
	if err := tx.Model(&model.Card{}).Where("id = ?", card.ID).Updates(map[string]any{
		"user_id": uid,
		"used_at": now,
	}).Error; err != nil {
		return nil, err
	}
	return &ct, nil
}

// lockOrCreateEntitlement 行锁读取用户在某项目的权限记录，不存在则创建
func lockOrCreateEntitlement(tx *gorm.DB, userID, projectID uint64) (*model.UserEntitlement, error) {
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
func getEntitlement(userID, projectID uint64) *model.UserEntitlement {
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
func entitlementExpiresAt(userID, projectID uint64) *time.Time {
	return entExpiresAt(getEntitlement(userID, projectID))
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
