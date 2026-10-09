package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// membershipOnlySQL 判断一条 entitlement 生效的计费模式是不是"会员到期"：
// entitlement 自己填了 billing_mode 就用它，没填就跟随项目配置，都没有则默认会员模式。
// 与 model.UserEntitlement.Mode() / projsetting.BillingMode() 的优先级保持一致。
const membershipOnlySQL = "COALESCE(NULLIF(user_entitlement.billing_mode, ''), ps.billing_mode, 'membership') = 'membership'"

// modeLabel 计费模式中文名（用于错误提示）。
func modeLabel(mode string) string {
	switch mode {
	case model.BillingPerCall:
		return "按次"
	case model.BillingCredits:
		return "积分"
	default:
		return "会员"
	}
}

// consumeQuota 按生效模式扣减用户在某项目的额度，并累计调用次数。
// membership 模式只累计次数、不动有效期（= v1 行为）。
// 返回扣减后的余额（membership 模式返回 0），供扣费流水记录 balance_after。
func consumeQuota(userID uint64, projectID, mode string, units int) (int, error) {
	if units <= 0 {
		units = 1
	}
	updates := map[string]any{"total_calls": gorm.Expr("total_calls + 1")}
	switch mode {
	case model.BillingPerCall:
		updates["remaining_calls"] = gorm.Expr("remaining_calls - ?", units)
	case model.BillingCredits:
		updates["credits"] = gorm.Expr("credits - ?", units)
	}
	if err := database.DB.Model(&model.UserEntitlement{}).
		Where("user_id = ? AND project_id = ?", userID, projectID).
		Updates(updates).Error; err != nil {
		return 0, err
	}
	// 读回余额给流水用（并发下可能有极小误差，账本只需要可读、可对账）
	var ent model.UserEntitlement
	if err := database.DB.Select("remaining_calls", "credits").
		Where("user_id = ? AND project_id = ?", userID, projectID).First(&ent).Error; err != nil {
		return 0, nil
	}
	if mode == model.BillingPerCall {
		return ent.RemainingCalls, nil
	}
	if mode == model.BillingCredits {
		return ent.Credits, nil
	}
	return 0, nil
}

// billingKindOf 把计费模式映射成流水里的额度类型（membership 不产生额度变动）。
func billingKindOf(mode string) string {
	switch mode {
	case model.BillingPerCall:
		return model.CardKindCalls
	case model.BillingCredits:
		return model.CardKindCredits
	default:
		return ""
	}
}

// MembershipItem 会员总览里的一格：某用户 × 某项目。
type MembershipItem struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	BillingMode     string `json:"billing_mode"`     // 实际生效的模式（用户覆盖优先，否则项目默认）
	BillingOverride string `json:"billing_override"` // 用户级覆盖值，'' = 跟随项目默认
	ExpiresAt       string `json:"expires_at"`       // 空 = 该项目下无记录
	HasTime         bool   `json:"has_time"`
	RemainingCalls  int    `json:"remaining_calls"`
	Credits         int    `json:"credits"`
	TotalCalls      int64  `json:"total_calls"`
	ExternalUID     string `json:"external_uid"`
	Remark          string `json:"remark"`
	LastCardID      uint64 `json:"last_card_id"`     // 最近一次充值所用卡密 id（0=没充过）
	LastRechargeAt  string `json:"last_recharge_at"` // 最近一次充值时间（空=没充过）
	Bound           bool   `json:"bound"`
	MachineCode     string `json:"machine_code"`
	TodayLogin      int64  `json:"today_login_count"`
}

// MembershipRow 总览里的一行：一个用户 + 他在各项目的情况。
type MembershipRow struct {
	UserID      uint64           `json:"user_id"`
	Username    string           `json:"username"`
	Status      int8             `json:"status"`
	Items       []MembershipItem `json:"items"`
	ValidCount  int              `json:"valid_count"`   // 仍然有效的项目数
	ExpiringIn7 int              `json:"expiring_in7"`  // 7 天内到期的项目数
	ExpiredNum  int              `json:"expired_count"` // 已过期的项目数
}

// membershipItemOf 组装一格（mode 为该用户在该项目生效的计费模式）。
func membershipItemOf(p model.Project, mode string, ent *model.UserEntitlement, bind *model.UserBinding, now time.Time) MembershipItem {
	it := MembershipItem{
		ProjectID:   p.ID,
		ProjectName: p.Name,
		BillingMode: mode,
	}
	if ent != nil && ent.ID != 0 {
		it.ExpiresAt = ent.ExpiresAt.Format("2006-01-02 15:04:05")
		it.HasTime = ent.ExpiresAt.After(now)
		it.RemainingCalls = ent.RemainingCalls
		it.Credits = ent.Credits
		it.TotalCalls = ent.TotalCalls
		it.ExternalUID = ent.ExternalUID
		it.Remark = ent.Remark
		it.BillingOverride = ent.BillingMode // '' = 跟随项目默认
		it.LastCardID = ent.LastCardID
		if ent.LastRechargeAt != nil {
			it.LastRechargeAt = ent.LastRechargeAt.Format("2006-01-02 15:04:05")
		}
	}
	if bind != nil {
		it.Bound = true
		it.MachineCode = bind.MachineCode
	}
	return it
}

// loadProjectModes 一次性取出所有项目配置，避免逐项目查询。
func loadProjectModes() map[string]model.ProjectSetting {
	var sets []model.ProjectSetting
	database.DB.Find(&sets)
	m := make(map[string]model.ProjectSetting, len(sets))
	for _, s := range sets {
		m[s.ProjectID] = s
	}
	return m
}

// ListMemberships 会员总览：一次查询聚合「用户 × 项目」，替代原来逐个点进用户详情。
//
//	GET /api/admin/memberships?keyword=&project_id=&filter=expired|expiring&page=&page_size=
func (h *Handler) ListMemberships(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	filterProject := strings.TrimSpace(c.Query("project_id"))
	filter := strings.TrimSpace(c.Query("filter"))
	now := timeNow()

	uq := database.DB.Model(&model.User{})
	if keyword != "" {
		uq = uq.Where("username LIKE ?", "%"+keyword+"%")
	}
	// 到期筛选：在 DB 侧用子查询，保证分页正确。
	// 只统计"会员到期"模式的项目：按次/积分项目没有"过期"概念，
	// 否则一个还有 500 积分余额的用户会被算成"已过期"，误导性很强。
	switch filter {
	case "expired":
		uq = uq.Where("id IN (?)", database.DB.Model(&model.UserEntitlement{}).
			Select("user_entitlement.user_id").
			Joins("LEFT JOIN project_setting AS ps ON ps.project_id = user_entitlement.project_id").
			Where("user_entitlement.expires_at < ?", now).
			Where(membershipOnlySQL))
	case "expiring":
		uq = uq.Where("id IN (?)", database.DB.Model(&model.UserEntitlement{}).
			Select("user_entitlement.user_id").
			Joins("LEFT JOIN project_setting AS ps ON ps.project_id = user_entitlement.project_id").
			Where("user_entitlement.expires_at >= ? AND user_entitlement.expires_at <= ?", now, now.AddDate(0, 0, 7)).
			Where(membershipOnlySQL))
	}

	var total int64
	if err := uq.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	var users []model.User
	if err := uq.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	// 项目列
	var projects []model.Project
	if err := database.DB.Where("deleted_at IS NULL").Order("id ASC").Find(&projects).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	if filterProject != "" {
		kept := projects[:0]
		for _, p := range projects {
			if p.ID == filterProject {
				kept = append(kept, p)
			}
		}
		projects = kept
	}

	// 批量取本页用户的 entitlement / binding（避免 N+1）
	userIDs := make([]uint64, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}
	entMap := map[string]*model.UserEntitlement{}
	bindMap := map[string]*model.UserBinding{}
	if len(userIDs) > 0 {
		var ents []model.UserEntitlement
		database.DB.Where("user_id IN ?", userIDs).Find(&ents)
		for i := range ents {
			entMap[fmt.Sprintf("%d:%s", ents[i].UserID, ents[i].ProjectID)] = &ents[i]
		}
		var binds []model.UserBinding
		database.DB.Where("user_id IN ?", userIDs).Find(&binds)
		for i := range binds {
			k := fmt.Sprintf("%d:%s", binds[i].UserID, binds[i].ProjectID)
			if _, exists := bindMap[k]; !exists {
				bindMap[k] = &binds[i]
			}
		}
	}
	modes := loadProjectModes()

	rows := make([]MembershipRow, 0, len(users))
	for _, u := range users {
		row := MembershipRow{UserID: u.ID, Username: u.Username, Status: u.Status, Items: make([]MembershipItem, 0, len(projects))}
		for _, p := range projects {
			k := fmt.Sprintf("%d:%s", u.ID, p.ID)
			ent := entMap[k]
			mode := model.BillingMembership
			if s, ok := modes[p.ID]; ok {
				mode = s.BillingModeOrDefault()
			}
			if ent != nil {
				mode = ent.Mode(mode)
			}
			it := membershipItemOf(p, mode, ent, bindMap[k], now)
			it.TodayLogin = h.todayLoginCount(p.ID, u.ID)
			row.Items = append(row.Items, it)

			// 有效/临期/过期三个计数只针对"会员到期"模式的项目。
			// 按次、积分项目的额度不看时间，算进来会得出"还有 500 积分但已过期"这种自相矛盾的结论。
			if ent != nil && ent.ID != 0 && mode == model.BillingMembership {
				if ent.ExpiresAt.After(now) {
					row.ValidCount++
					if ent.ExpiresAt.Before(now.AddDate(0, 0, 7)) {
						row.ExpiringIn7++
					}
				} else {
					row.ExpiredNum++
				}
			}
		}
		rows = append(rows, row)
	}

	util.OK(c, util.NewPage(rows, total, page, pageSize))
}

// projectMode 取某项目对某用户生效的计费模式。
func projectMode(projectID string, ent *model.UserEntitlement) string {
	mode := projsetting.BillingMode(projectID)
	if ent != nil {
		mode = ent.Mode(mode)
	}
	return mode
}

// projectUnit 项目单价：per_call 每次消耗几个次数，credits 每次扣多少积分。
// 未配置时按 1，保证"按次/积分"模式一定可扣。
func projectUnit(projectID string) int {
	if p := projsetting.Price(projectID); p > 0 {
		return p
	}
	return 1
}

// UpdateEntitlementRequest 设置用户在某项目上的「计费模式覆盖 + 外部用户号 + 备注」。
// billing_mode 空字符串 = 跟随项目默认（清除覆盖）。
// external_uid / remark 用指针，区分"没传这个字段"和"传了空值要清空"。
type UpdateEntitlementRequest struct {
	ProjectID   string  `json:"project_id" binding:"required"`
	BillingMode string  `json:"billing_mode"`
	ExternalUID *string `json:"external_uid"`
	Remark      *string `json:"remark"`
}

// UpdateUserEntitlement 设置/清除用户在某项目的计费覆盖与外部用户号。
// 这是 v2 设计里「项目级默认 + 用户级可覆盖」的后半句——在此之前只有读取方，
// 没有任何地方能写入 user_entitlement.billing_mode / external_uid。
//
//	PUT /api/admin/users/:id/entitlement
func (h *Handler) UpdateUserEntitlement(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateEntitlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if !isProjectID(req.ProjectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	// 计费覆盖：空 = 跟随项目；其余必须是三种合法模式
	mode := strings.TrimSpace(req.BillingMode)
	switch mode {
	case "", model.BillingMembership, model.BillingPerCall, model.BillingCredits:
	default:
		util.Fail(c, util.CodeParamError, "billing_mode 只能是空（跟随项目）/ membership / per_call / credits")
		return
	}
	if req.ExternalUID != nil {
		uid := strings.TrimSpace(*req.ExternalUID)
		if len([]rune(uid)) > 64 {
			util.Fail(c, util.CodeParamError, "external_uid 最长 64 个字符")
			return
		}
		req.ExternalUID = &uid
	}
	if req.Remark != nil {
		rm := strings.TrimSpace(*req.Remark)
		if len([]rune(rm)) > 255 {
			util.Fail(c, util.CodeParamError, "备注最长 255 个字符")
			return
		}
		req.Remark = &rm
	}

	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).Limit(1).Find(&project).Error; err != nil || project.ID == "" {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "用户不存在")
		return
	}

	now := timeNow()
	var ent model.UserEntitlement
	if err := database.DB.Where("user_id = ? AND project_id = ?", user.ID, req.ProjectID).
		Limit(1).Find(&ent).Error; err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if ent.ID == 0 {
		ent = model.UserEntitlement{UserID: user.ID, ProjectID: req.ProjectID, ExpiresAt: now}
		if err := database.DB.Create(&ent).Error; err != nil {
			util.Fail(c, util.CodeDBError, "创建会员记录失败")
			return
		}
	}

	updates := map[string]any{"billing_mode": mode}
	if req.ExternalUID != nil {
		updates["external_uid"] = *req.ExternalUID
	}
	if req.Remark != nil {
		updates["remark"] = *req.Remark
	}
	if err := database.DB.Model(&model.UserEntitlement{}).Where("id = ?", ent.ID).
		Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	database.DB.Where("id = ?", ent.ID).Limit(1).Find(&ent)

	h.recordAudit(c, model.AuditUserEntitlement, req.ProjectID,
		"user:"+strconv.FormatUint(user.ID, 10),
		gin.H{"billing_mode": mode, "external_uid": ent.ExternalUID, "remark": ent.Remark,
			"username": user.Username})

	util.OK(c, membershipItemOf(project, projectMode(req.ProjectID, &ent), &ent, nil, now))
}

// GetUserMemberships 用户会员详情（v2 版）：比老 /membership 多返回计费模式/次数/积分/外部 UID。
// 老接口 /api/admin/users/:id/membership 保持原样不动。
//
//	GET /api/admin/users/:id/memberships
func (h *Handler) GetUserMemberships(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "用户不存在")
		return
	}
	var projects []model.Project
	database.DB.Where("deleted_at IS NULL").Order("id ASC").Find(&projects)

	var ents []model.UserEntitlement
	database.DB.Where("user_id = ?", user.ID).Find(&ents)
	entMap := make(map[string]*model.UserEntitlement, len(ents))
	for i := range ents {
		entMap[ents[i].ProjectID] = &ents[i]
	}
	var binds []model.UserBinding
	database.DB.Where("user_id = ?", user.ID).Find(&binds)
	bindMap := make(map[string]*model.UserBinding, len(binds))
	for i := range binds {
		if _, exists := bindMap[binds[i].ProjectID]; !exists {
			bindMap[binds[i].ProjectID] = &binds[i]
		}
	}
	modes := loadProjectModes()
	now := timeNow()

	items := make([]MembershipItem, 0, len(projects))
	for _, p := range projects {
		mode := model.BillingMembership
		if s, ok2 := modes[p.ID]; ok2 {
			mode = s.BillingModeOrDefault()
		}
		ent := entMap[p.ID]
		if ent != nil {
			mode = ent.Mode(mode)
		}
		it := membershipItemOf(p, mode, ent, bindMap[p.ID], now)
		it.TodayLogin = h.todayLoginCount(p.ID, user.ID)
		items = append(items, it)
	}

	util.OK(c, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"status":   user.Status,
		"items":    items,
	})
}

// GrantUserRequest 直接给用户在指定项目加/减 天数、次数、积分。
type GrantUserRequest struct {
	ProjectID string `json:"project_id" binding:"required"`
	Kind      string `json:"kind" binding:"required"`   // days | calls | credits
	Amount    int    `json:"amount" binding:"required"` // >0 加，<0 扣
	Remark    string `json:"remark"`
}

// GrantUser 管理员直接调整用户在某项目的会员：加天数 / 加次数 / 加积分。
// 这是 v1 最硬的一处缺失（以前只能发卡密或改数据库）。
//
//	POST /api/admin/users/:id/grant
func (h *Handler) GrantUser(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req GrantUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、kind、amount 不能为空")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if !isProjectID(req.ProjectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	switch req.Kind {
	case "days", "calls", "credits":
	default:
		util.Fail(c, util.CodeParamError, "kind 只能是 days / calls / credits")
		return
	}
	if req.Amount == 0 {
		util.Fail(c, util.CodeParamError, "amount 不能为 0")
		return
	}

	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).Limit(1).Find(&project).Error; err != nil || project.ID == "" {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "用户不存在")
		return
	}

	now := timeNow()
	var ent model.UserEntitlement
	if err := database.DB.Where("user_id = ? AND project_id = ?", user.ID, req.ProjectID).
		Limit(1).Find(&ent).Error; err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if ent.ID == 0 {
		ent = model.UserEntitlement{UserID: user.ID, ProjectID: req.ProjectID, ExpiresAt: now}
		if err := database.DB.Create(&ent).Error; err != nil {
			util.Fail(c, util.CodeDBError, "创建会员记录失败")
			return
		}
	}

	updates := map[string]any{"last_recharge_at": &now}
	if strings.TrimSpace(req.Remark) != "" {
		updates["remark"] = strings.TrimSpace(req.Remark)
	}
	switch req.Kind {
	case "days":
		base := ent.ExpiresAt
		if !base.After(now) {
			base = now
		}
		updates["expires_at"] = base.AddDate(0, 0, req.Amount)
	case "calls":
		n := ent.RemainingCalls + req.Amount
		if n < 0 {
			util.Fail(c, util.CodeParamError, "扣减次数超过剩余次数")
			return
		}
		updates["remaining_calls"] = n
	case "credits":
		n := ent.Credits + req.Amount
		if n < 0 {
			util.Fail(c, util.CodeParamError, "扣减积分超过剩余积分")
			return
		}
		updates["credits"] = n
	}

	if err := database.DB.Model(&model.UserEntitlement{}).Where("id = ?", ent.ID).
		Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	database.DB.Where("id = ?", ent.ID).Limit(1).Find(&ent)

	h.recordAudit(c, model.AuditUserGrant, req.ProjectID,
		"user:"+strconv.FormatUint(user.ID, 10),
		gin.H{"kind": req.Kind, "amount": req.Amount, "remark": req.Remark, "username": user.Username})

	// 扣费流水：管理员发放也要进账本（reason=grant），这样"额度从哪来"可对账
	balanceAfter := 0
	switch req.Kind {
	case "calls":
		balanceAfter = ent.RemainingCalls
	case "credits":
		balanceAfter = ent.Credits
	}
	h.logBilling(BillingEntry{
		ProjectID: req.ProjectID, UserID: user.ID, Username: user.Username,
		Kind: req.Kind, Delta: req.Amount, BalanceAfter: balanceAfter,
		Reason: model.BillReasonGrant, Remark: firstNonEmpty(req.Remark, "管理员发放"),
	})

	util.OK(c, membershipItemOf(project, projectMode(req.ProjectID, &ent), &ent, nil, now))
}

// firstNonEmpty 取第一个非空字符串（流水备注兜底用）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
