package handler

import (
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

// parseTimeLoose 解析 "2006-01-02 15:04:05" 或 "2006-01-02"。
func parseTimeLoose(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// 项目内的三个新页面（项目用户 / 调用记录 / 扣费记录）+ 项目统计。
// 全部是新增接口，不动任何老接口。

// ---------- 项目用户 ----------

// ProjectUserItem 项目用户一行：只统计"在这个项目里有记录"的用户。
type ProjectUserItem struct {
	UserID          uint64 `json:"user_id"`
	Username        string `json:"username"`
	Status          int8   `json:"status"`
	BillingMode     string `json:"billing_mode"` // 该项目对这个人生效的模式
	ExpiresAt       string `json:"expires_at"`
	HasTime         bool   `json:"has_time"`
	RemainingCalls  int    `json:"remaining_calls"`
	Credits         int    `json:"credits"`
	ExternalUID     string `json:"external_uid"`
	Remark          string `json:"remark"`
	Bound           bool   `json:"bound"`
	MachineCode     string `json:"machine_code"`
	TodayCalls      int64  `json:"today_calls"`      // 今日调用次数（来自调用记录）
	TotalCalls      int64  `json:"total_calls"`      // 累计调用次数（来自调用记录）
	LastCallAt      string `json:"last_call_at"`     // 最近一次调用时间
	ConsumedCredits int64  `json:"consumed_credits"` // 累计消耗积分
	ConsumedCalls   int64  `json:"consumed_calls"`   // 累计消耗次数
}

// ListProjectUsers 项目用户【参数】keyword / filter=expired|expiring|bound|unbound / page / page_size。
//
//	GET /api/admin/projects/:id/users
func (h *Handler) ListProjectUsers(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !isProjectID(projectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	filter := strings.TrimSpace(c.Query("filter"))
	now := timeNow()

	// 该项目"有记录"的用户 = 有会员行 或 有绑定行（含已过期的老会员）
	var ents []model.UserEntitlement
	database.DB.Where("project_id = ? AND user_id > 0", projectID).Find(&ents)
	var binds []model.UserBinding
	database.DB.Where("project_id = ? AND user_id > 0", projectID).Find(&binds)

	entMap := make(map[uint64]*model.UserEntitlement, len(ents))
	for i := range ents {
		entMap[ents[i].UserID] = &ents[i]
	}
	bindMap := make(map[uint64]*model.UserBinding, len(binds))
	for i := range binds {
		if _, ok := bindMap[binds[i].UserID]; !ok {
			bindMap[binds[i].UserID] = &binds[i]
		}
	}

	// 分页和筛选都在 SQL 里做（否则 total 会和筛完的列表对不上）：
	// 「有会员 或 有绑定」+ 关键字 +（已过期/7天内到期/已绑定/未绑定）
	scoped := "(EXISTS (SELECT 1 FROM user_entitlement e WHERE e.user_id = u.id AND e.project_id = ?)" +
		" OR EXISTS (SELECT 1 FROM user_binding b WHERE b.user_id = u.id AND b.project_id = ?))"
	uq := database.DB.Table("user AS u").Where(scoped, projectID, projectID)
	if keyword != "" {
		uq = uq.Where("u.username LIKE ?", "%"+keyword+"%")
	}
	// 生效模式 = 用户覆盖优先，否则项目默认（与 model.UserEntitlement.Mode 一致）
	effMode := "COALESCE(NULLIF(e.billing_mode, ''), ps.billing_mode, 'membership')"
	switch filter {
	case "expired":
		// 只有"会员到期"模式、且真的有过会员行、且已过期的用户才算
		uq = uq.Where("EXISTS (SELECT 1 FROM user_entitlement e"+
			" LEFT JOIN project_setting ps ON ps.project_id = e.project_id"+
			" WHERE e.user_id = u.id AND e.project_id = ? AND "+effMode+" = 'membership' AND e.expires_at <= ?)",
			projectID, now)
	case "expiring":
		uq = uq.Where("EXISTS (SELECT 1 FROM user_entitlement e"+
			" LEFT JOIN project_setting ps ON ps.project_id = e.project_id"+
			" WHERE e.user_id = u.id AND e.project_id = ? AND "+effMode+" = 'membership'"+
			" AND e.expires_at > ? AND e.expires_at <= ?)",
			projectID, now, now.AddDate(0, 0, 7))
	case "bound":
		uq = uq.Where("EXISTS (SELECT 1 FROM user_binding b WHERE b.user_id = u.id AND b.project_id = ?)", projectID)
	case "unbound":
		uq = uq.Where("NOT EXISTS (SELECT 1 FROM user_binding b WHERE b.user_id = u.id AND b.project_id = ?)", projectID)
	}

	var total int64
	if err := uq.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	if total == 0 {
		util.OK(c, util.NewPage([]ProjectUserItem{}, 0, page, pageSize))
		return
	}
	var users []model.User
	if err := uq.Select("u.id, u.username, u.status").
		Order("u.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	pageIDs := make([]uint64, 0, len(users))
	for _, u := range users {
		pageIDs = append(pageIDs, u.ID)
	}

	// 聚合：今日调用 / 累计调用 / 最近调用 / 累计消耗（只算当前这一页的用户，避免全表扫）
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	type callAgg struct {
		UserID uint64
		Today  int64
		Total  int64
		LastAt *time.Time
	}
	callAggs := map[uint64]callAgg{}
	if len(pageIDs) > 0 {
		var rows []callAgg
		database.DB.Model(&model.CallLog{}).
			Select("user_id, SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS today, COUNT(*) AS total, MAX(created_at) AS last_at", todayStart).
			Where("project_id = ? AND user_id IN ?", projectID, pageIDs).
			Group("user_id").Scan(&rows)
		for _, r := range rows {
			callAggs[r.UserID] = r
		}
	}
	type sumAgg struct {
		UserID uint64
		Kind   string
		Sum    int64
	}
	sums := map[uint64]map[string]int64{}
	if len(pageIDs) > 0 {
		var rows []sumAgg
		database.DB.Model(&model.BillingLog{}).
			Select("user_id, kind, SUM(-delta) AS sum").
			Where("project_id = ? AND user_id IN ? AND reason = ? AND delta < 0", projectID, pageIDs, model.BillReasonCall).
			Group("user_id, kind").Scan(&rows)
		for _, r := range rows {
			if sums[r.UserID] == nil {
				sums[r.UserID] = map[string]int64{}
			}
			sums[r.UserID][r.Kind] = r.Sum
		}
	}

	pmode := projsetting.BillingMode(projectID)
	list := make([]ProjectUserItem, 0, len(users))
	for _, u := range users {
		ent := entMap[u.ID]
		bind := bindMap[u.ID]
		mode := pmode
		if ent != nil {
			mode = ent.Mode(mode)
		}
		item := ProjectUserItem{
			UserID:      u.ID,
			Username:    u.Username,
			Status:      u.Status,
			BillingMode: mode,
		}
		if ent != nil {
			item.ExpiresAt = ent.ExpiresAt.Format("2006-01-02 15:04:05")
			item.HasTime = ent.ExpiresAt.After(now)
			item.RemainingCalls = ent.RemainingCalls
			item.Credits = ent.Credits
			item.ExternalUID = ent.ExternalUID
			item.Remark = ent.Remark
		}
		if bind != nil {
			item.Bound = true
			item.MachineCode = bind.MachineCode
		}
		if a, ok := callAggs[u.ID]; ok {
			item.TodayCalls = a.Today
			item.TotalCalls = a.Total
			if a.LastAt != nil {
				item.LastCallAt = a.LastAt.Format("2006-01-02 15:04:05")
			}
		}
		if s := sums[u.ID]; s != nil {
			item.ConsumedCredits = s[model.CardKindCredits]
			item.ConsumedCalls = s[model.CardKindCalls]
		}
		// 筛选已经在 SQL 里做掉了（这样 total 和列表是一致的），这里不再二次过滤
		list = append(list, item)
	}

	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// ---------- 调用记录 ----------

// ListProjectCallLogs 调用记录【参数】keyword(用户名/key名) / action / source / result=ok|fail /
// start_time / end_time / page / page_size。
//
//	GET /api/admin/projects/:id/call-logs
func (h *Handler) ListProjectCallLogs(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !isProjectID(projectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	page, pageSize := parsePage(c)

	q := database.DB.Model(&model.CallLog{}).Where("project_id = ?", projectID)
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(username LIKE ? OR api_key_name LIKE ? OR message LIKE ?)", like, like, like)
	}
	if a := strings.TrimSpace(c.Query("action")); a != "" {
		q = q.Where("action = ?", a)
	}
	if s := strings.TrimSpace(c.Query("source")); s != "" {
		q = q.Where("source = ?", s)
	}
	switch strings.TrimSpace(c.Query("result")) {
	case "ok":
		q = q.Where("ok = ?", true)
	case "fail":
		q = q.Where("ok = ?", false)
	}
	q = applyTimeRange(q, c)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	var rows []model.CallLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		list = append(list, gin.H{
			"id": r.ID, "action": r.Action, "source": r.Source,
			"user_id": r.UserID, "username": r.Username,
			"api_key_id": r.ApiKeyID, "api_key_name": r.ApiKeyName,
			"ok": r.Ok, "errno": r.Errno, "message": r.Message,
			"cost_kind": r.CostKind, "cost": r.Cost,
			"ip": r.IP, "duration_ms": r.DurationMS,
			"created_at": r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// ---------- 扣费记录 ----------

// ListProjectBillingLogs 扣费记录【参数】keyword / kind / reason / start_time / end_time / page / page_size。
//
//	GET /api/admin/projects/:id/billing-logs
func (h *Handler) ListProjectBillingLogs(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !isProjectID(projectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	page, pageSize := parsePage(c)

	q := database.DB.Model(&model.BillingLog{}).Where("project_id = ?", projectID)
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(username LIKE ? OR remark LIKE ?)", like, like)
	}
	if k := strings.TrimSpace(c.Query("kind")); k != "" {
		q = q.Where("kind = ?", k)
	}
	if r := strings.TrimSpace(c.Query("reason")); r != "" {
		q = q.Where("reason = ?", r)
	}
	switch strings.TrimSpace(c.Query("direction")) {
	case "in":
		q = q.Where("delta > 0")
	case "out":
		q = q.Where("delta < 0")
	}
	q = applyTimeRange(q, c)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	var rows []model.BillingLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		list = append(list, gin.H{
			"id": r.ID, "user_id": r.UserID, "username": r.Username,
			"api_key_id": r.ApiKeyID, "kind": r.Kind, "delta": r.Delta,
			"balance_after": r.BalanceAfter, "reason": r.Reason,
			"ref_id": r.RefID, "remark": r.Remark,
			"created_at": r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// ---------- 项目统计 ----------

// GetProjectStats 项目统计：今日 + 近 N 天趋势。
//
//	GET /api/admin/projects/:id/stats?days=7
func (h *Handler) GetProjectStats(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !isProjectID(projectID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	days := 7
	if v := strings.TrimSpace(c.Query("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	now := timeNow()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := todayStart.AddDate(0, 0, -(days - 1))

	// 项目自身信息
	var p model.Project
	if err := database.DB.Where("id = ?", projectID).Limit(1).Find(&p).Error; err != nil || p.ID == "" {
		util.Fail(c, util.CodeNotFound, "项目不存在")
		return
	}

	// 调用与扣费聚合：按时间段各算一份（今日 / 昨日 / 近7天 / 累计）
	aggOf := func(fromT, toT time.Time) gin.H {
		type agg struct {
			Total int64
			Ok    int64
			Users int64
		}
		var a agg
		q := database.DB.Model(&model.CallLog{}).
			Select("COUNT(*) AS total, SUM(CASE WHEN ok THEN 1 ELSE 0 END) AS ok, COUNT(DISTINCT user_id) AS users").
			Where("project_id = ?", projectID)
		if !fromT.IsZero() {
			q = q.Where("created_at >= ?", fromT)
		}
		if !toT.IsZero() {
			q = q.Where("created_at < ?", toT)
		}
		q.Scan(&a)

		type sumRow struct {
			Kind string
			Sum  int64
		}
		var sums []sumRow
		sq := database.DB.Model(&model.BillingLog{}).
			Select("kind, SUM(-delta) AS sum").
			Where("project_id = ? AND delta < 0", projectID)
		if !fromT.IsZero() {
			sq = sq.Where("created_at >= ?", fromT)
		}
		if !toT.IsZero() {
			sq = sq.Where("created_at < ?", toT)
		}
		sq.Group("kind").Scan(&sums)
		cost := map[string]int64{}
		for _, r := range sums {
			cost[r.Kind] = r.Sum
		}

		// 兑换次数：本项目「已被兑换」的卡密数（card.used_at 非空，兑换时写入）
		var redeem int64
		rq := database.DB.Model(&model.Card{}).
			Where("project_id = ? AND used_at IS NOT NULL", projectID)
		if !fromT.IsZero() {
			rq = rq.Where("used_at >= ?", fromT)
		}
		if !toT.IsZero() {
			rq = rq.Where("used_at < ?", toT)
		}
		rq.Count(&redeem)

		return gin.H{
			"calls": a.Total, "ok": a.Ok, "fail": a.Total - a.Ok,
			"active_users": a.Users, "credits": cost[model.CardKindCredits],
			"calls_cost": cost[model.CardKindCalls], "redeem": redeem,
		}
	}

	yesterdayStart := todayStart.AddDate(0, 0, -1)
	weekStart := todayStart.AddDate(0, 0, -6) // 含今天，共 7 天
	todayAgg := aggOf(todayStart, time.Time{})
	yesterdayAgg := aggOf(yesterdayStart, todayStart)
	last7Agg := aggOf(weekStart, time.Time{})
	allAgg := aggOf(time.Time{}, time.Time{})

	// 用户数（该项目有会员或绑定的用户）
	var entUsers, bindUsers int64
	database.DB.Model(&model.UserEntitlement{}).Where("project_id = ? AND user_id > 0", projectID).Count(&entUsers)
	database.DB.Model(&model.UserBinding{}).Where("project_id = ? AND user_id > 0", projectID).Count(&bindUsers)

	// 近 N 天趋势
	type dayRow struct {
		D     string
		Total int64
		Ok    int64
	}
	var rows []dayRow
	database.DB.Model(&model.CallLog{}).
		Select("DATE_FORMAT(created_at, '%Y-%m-%d') AS d, COUNT(*) AS total, SUM(CASE WHEN ok THEN 1 ELSE 0 END) AS ok").
		Where("project_id = ? AND created_at >= ?", projectID, from).
		Group("d").Order("d ASC").Scan(&rows)
	byDay := map[string]dayRow{}
	for _, r := range rows {
		byDay[r.D] = r
	}

	// 近 N 天每日兑换次数
	type redeemRow struct {
		D      string
		Redeem int64
	}
	var rrows []redeemRow
	database.DB.Model(&model.Card{}).
		Select("DATE_FORMAT(used_at, '%Y-%m-%d') AS d, COUNT(*) AS redeem").
		Where("project_id = ? AND used_at IS NOT NULL AND used_at >= ?", projectID, from).
		Group("d").Order("d ASC").Scan(&rrows)
	redeemByDay := map[string]int64{}
	for _, r := range rrows {
		redeemByDay[r.D] = r.Redeem
	}

	series := make([]gin.H, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		r := byDay[d]
		series = append(series, gin.H{
			"date": d, "calls": r.Total, "ok": r.Ok, "fail": r.Total - r.Ok,
			"redeem": redeemByDay[d],
		})
	}

	// 卡密 / key / 版本数量（项目概览用）
	var cardCount, cardTypeCount, keyCount, versionCount int64
	database.DB.Model(&model.Card{}).Where("project_id = ?", projectID).Count(&cardCount)
	database.DB.Model(&model.CardType{}).Where("project_id = ? AND deleted_at IS NULL", projectID).Count(&cardTypeCount)
	database.DB.Model(&model.ApiKey{}).Where("project_id = ?", projectID).Count(&keyCount)
	database.DB.Model(&model.Version{}).Where("project_id = ?", projectID).Count(&versionCount)

	util.OK(c, gin.H{
		"project":   gin.H{"id": p.ID, "name": p.Name},
		"today":     todayAgg,
		"yesterday": yesterdayAgg,
		"last7":     last7Agg,
		"total":     allAgg,
		"counts": gin.H{
			"users": entUsers + bindUsers, "entitlement_users": entUsers, "bound_users": bindUsers,
			"cards": cardCount, "card_types": cardTypeCount, "keys": keyCount, "versions": versionCount,
		},
		"series": series,
	})
}

// applyTimeRange 统一处理 start_time / end_time（格式 2006-01-02 或 2006-01-02 15:04:05）。
func applyTimeRange(q *gorm.DB, c *gin.Context) *gorm.DB {
	if s := strings.TrimSpace(c.Query("start_time")); s != "" {
		if t, ok := parseTimeLoose(s); ok {
			q = q.Where("created_at >= ?", t)
		}
	}
	if e := strings.TrimSpace(c.Query("end_time")); e != "" {
		if t, ok := parseTimeLoose(e); ok {
			// 只给日期时按当天 23:59:59 处理，避免"选了结束日却查不到当天"
			if len(strings.TrimSpace(e)) <= 10 {
				t = t.Add(24*time.Hour - time.Second)
			}
			q = q.Where("created_at <= ?", t)
		}
	}
	return q
}
