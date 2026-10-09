package handler

import (
	"errors"
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 代理（代理商）：每个项目一套档案，**不是登录账号**，不能登录后台。
// 用途：开卡时挂到某个代理名下 → 按「代理 × 卡密类型」的单价统计这批卡的价值 → 勾选后结算。

// ListAgents 代理列表（带开卡张数/已用张数与价值统计）。
//
//	GET /api/admin/projects/:id/agents
func (h *Handler) ListAgents(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	var agents []model.Agent
	if err := database.DB.Where("project_id = ?", projectID).Order("id DESC").Find(&agents).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	prices := agentPriceMap()
	list := make([]gin.H, 0, len(agents))
	for i := range agents {
		a := agents[i]
		st := agentStatOf(projectID, a.ID, prices)
		list = append(list, gin.H{
			"id": a.ID, "project_id": a.ProjectID, "name": a.Name, "contact": a.Contact,
			"phone": a.Phone, "remark": a.Remark, "status": a.Status,
			"created_at": a.CreatedAt.Format("2006-01-02 15:04:05"),
			"stats":      st,
		})
	}
	util.OK(c, list)
}

// CreateAgent 新建代理
//
//	POST /api/admin/projects/:id/agents  {name, contact, phone, remark}
func (h *Handler) CreateAgent(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	var req struct {
		Name    string `json:"name" binding:"required"`
		Contact string `json:"contact"`
		Phone   string `json:"phone"`
		Remark  string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：name 不能为空")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 32 {
		util.Fail(c, util.CodeParamError, "代理名称 1-32 字")
		return
	}
	var dup int64
	database.DB.Model(&model.Agent{}).Where("project_id = ? AND name = ?", projectID, name).Count(&dup)
	if dup > 0 {
		util.Fail(c, util.CodeParamError, "同名代理已存在")
		return
	}
	a := model.Agent{
		ProjectID: projectID, Name: name,
		Contact: strings.TrimSpace(req.Contact), Phone: strings.TrimSpace(req.Phone),
		Remark: strings.TrimSpace(req.Remark), Status: 1,
	}
	if err := database.DB.Create(&a).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	util.OK(c, gin.H{"id": a.ID, "name": a.Name})
}

// UpdateAgent 修改代理（名称/联系人/联系方式/备注/状态）
//
//	PUT /api/admin/agents/:id
func (h *Handler) UpdateAgent(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var a model.Agent
	if err := database.DB.First(&a, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "代理不存在")
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Contact *string `json:"contact"`
		Phone   *string `json:"phone"`
		Remark  *string `json:"remark"`
		Status  *int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len([]rune(n)) > 32 {
			util.Fail(c, util.CodeParamError, "代理名称 1-32 字")
			return
		}
		updates["name"] = n
	}
	if req.Contact != nil {
		updates["contact"] = strings.TrimSpace(*req.Contact)
	}
	if req.Phone != nil {
		updates["phone"] = strings.TrimSpace(*req.Phone)
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if len(updates) == 0 {
		util.OK(c, gin.H{"id": a.ID})
		return
	}
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", a.ID).Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	util.OK(c, gin.H{"id": a.ID})
}

// DeleteAgent 删除代理（已开过卡的代理不允许删除，避免历史卡失去归属）
//
//	DELETE /api/admin/agents/:id
func (h *Handler) DeleteAgent(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var cnt int64
	database.DB.Model(&model.Card{}).Where("agent_id = ?", id).Count(&cnt)
	if cnt > 0 {
		util.Fail(c, util.CodeParamError, "该代理名下已有 "+strconv.FormatInt(cnt, 10)+" 张卡，不能删除（可改成停用）")
		return
	}
	if err := database.DB.Delete(&model.Agent{}, id).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	database.DB.Where("agent_id = ?", id).Delete(&model.AgentPrice{})
	util.OK(c, nil)
}

// ListAgentPrices 代理的「每种卡密类型」单价（返回本项目所有类型 + 该代理已设的价）
//
//	GET /api/admin/agents/:id/prices
func (h *Handler) ListAgentPrices(c *gin.Context) {
	agent, ok := h.agentOf(c)
	if !ok {
		return
	}
	var types []model.CardType
	database.DB.Where("project_id = ? AND deleted_at IS NULL", agent.ProjectID).Order("id ASC").Find(&types)
	var prices []model.AgentPrice
	database.DB.Where("agent_id = ?", agent.ID).Find(&prices)
	pm := make(map[uint64]int64, len(prices))
	for _, p := range prices {
		pm[p.CardTypeID] = p.PriceCents
	}
	list := make([]gin.H, 0, len(types))
	for _, t := range types {
		kind, amount := t.KindAmount()
		list = append(list, gin.H{
			"card_type_id": t.ID, "type_name": t.Name, "kind": kind, "amount": amount,
			"price_cents": pm[t.ID],
		})
	}
	util.OK(c, gin.H{"agent": gin.H{"id": agent.ID, "name": agent.Name, "project_id": agent.ProjectID}, "list": list})
}

// SaveAgentPrices 批量保存代理单价
//
//	PUT /api/admin/agents/:id/prices  {prices:[{card_type_id, price_cents}]}
func (h *Handler) SaveAgentPrices(c *gin.Context) {
	agent, ok := h.agentOf(c)
	if !ok {
		return
	}
	var req struct {
		Prices []struct {
			CardTypeID uint64 `json:"card_type_id"`
			PriceCents int64  `json:"price_cents"`
		} `json:"prices"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	for _, p := range req.Prices {
		if p.CardTypeID == 0 || p.PriceCents < 0 {
			continue
		}
		var row model.AgentPrice
		err := database.DB.Where("agent_id = ? AND card_type_id = ?", agent.ID, p.CardTypeID).First(&row).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			database.DB.Create(&model.AgentPrice{AgentID: agent.ID, CardTypeID: p.CardTypeID, PriceCents: p.PriceCents})
		case err == nil:
			database.DB.Model(&model.AgentPrice{}).Where("id = ?", row.ID).Update("price_cents", p.PriceCents)
		}
	}
	util.OK(c, nil)
}

// AgentStats 单个代理的统计：开卡张数 / 已用张数 / 卡价值 / 已结算与未结算金额
//
//	GET /api/admin/agents/:id/stats
func (h *Handler) AgentStats(c *gin.Context) {
	agent, ok := h.agentOf(c)
	if !ok {
		return
	}
	st := agentStatOf(agent.ProjectID, agent.ID, agentPriceMap())
	util.OK(c, st)
}

// SettleCards 结算：把勾选的卡按「代理 × 类型」单价汇总成一张结算单，并把卡标记为已结算。
//
//	POST /api/admin/cards/settle  {card_ids:[1,2,3], remark:"10 月对账"}
// 约束：一次只能结算同一个代理名下的卡；已结算过的卡不能重复结算。
func (h *Handler) SettleCards(c *gin.Context) {
	var req struct {
		CardIDs []uint64 `json:"card_ids" binding:"required"`
		Remark  string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.CardIDs) == 0 {
		util.Fail(c, util.CodeParamError, "参数错误：card_ids 不能为空")
		return
	}
	if len(req.CardIDs) > 5000 {
		util.Fail(c, util.CodeParamError, "一次最多结算 5000 张")
		return
	}

	var cards []model.Card
	if err := database.DB.Where("id IN ?", req.CardIDs).Find(&cards).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询卡密失败")
		return
	}
	if len(cards) == 0 {
		util.Fail(c, util.CodeNotFound, "卡密不存在")
		return
	}
	agentID, projectID := cards[0].AgentID, cards[0].ProjectID
	for _, cd := range cards {
		if cd.AgentID != agentID {
			util.Fail(c, util.CodeParamError, "一次只能结算同一个代理名下的卡")
			return
		}
		if cd.SettleID != 0 {
			util.Fail(c, util.CodeParamError, "有卡已经结算过了，请重新查询后再选")
			return
		}
	}
	if agentID == 0 {
		util.Fail(c, util.CodeParamError, "这些卡没有代理归属，不需要结算")
		return
	}
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "代理不存在")
		return
	}

	// 按类型汇总金额：Σ(张数 × 该代理在该类型上的单价)
	pm := agentPriceMap()
	byType := map[uint64]int{}
	used := 0
	for _, cd := range cards {
		byType[cd.TypeID]++
		if cd.UserID != nil {
			used++
		}
	}
	var amount int64
	for tid, n := range byType {
		amount += int64(n) * pm[priceKey(agentID, tid)]
	}
	operator := ""
	if cl := middleware.GetClaims(c); cl != nil {
		operator = cl.Username
	}

	st := model.CardSettlement{
		ProjectID: projectID, AgentID: agentID, AgentName: agent.Name,
		CardCount: len(cards), UsedCount: used, AmountCents: amount,
		Remark: strings.TrimSpace(req.Remark), Operator: operator,
	}
	if err := database.DB.Create(&st).Error; err != nil {
		util.Fail(c, util.CodeDBError, "结算失败")
		return
	}
	now := timeNow()
	database.DB.Model(&model.Card{}).Where("id IN ?", req.CardIDs).
		Updates(map[string]any{"settle_id": st.ID, "settled_at": now})

	h.recordAudit(c, model.AuditCardGenerate, projectID, "settlement:"+strconv.FormatUint(st.ID, 10),
		gin.H{"agent": agent.Name, "cards": len(cards), "amount_cents": amount})
	util.OK(c, gin.H{
		"settlement_id": st.ID, "agent_id": agentID, "agent_name": agent.Name,
		"card_count": len(cards), "used_count": used, "amount_cents": amount,
	})
}

// ListSettlements 代理的结算记录
//
//	GET /api/admin/agents/:id/settlements
func (h *Handler) ListSettlements(c *gin.Context) {
	agent, ok := h.agentOf(c)
	if !ok {
		return
	}
	var list []model.CardSettlement
	database.DB.Where("agent_id = ?", agent.ID).Order("id DESC").Limit(200).Find(&list)
	util.OK(c, list)
}

// ---------- 内部工具 ----------

// agentOf 取路径里的代理（供本文件各接口复用）
func (h *Handler) agentOf(c *gin.Context) (model.Agent, bool) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return model.Agent{}, false
	}
	var a model.Agent
	if err := database.DB.First(&a, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "代理不存在")
		return model.Agent{}, false
	}
	return a, true
}

// priceKey 代理单价的内存键
func priceKey(agentID, cardTypeID uint64) uint64 { return agentID<<32 | (cardTypeID & 0xFFFFFFFF) }

// agentPriceMap 一次性把全部代理单价读进内存（量很小）
func agentPriceMap() map[uint64]int64 {
	var rows []model.AgentPrice
	database.DB.Find(&rows)
	m := make(map[uint64]int64, len(rows))
	for _, r := range rows {
		m[priceKey(r.AgentID, r.CardTypeID)] = r.PriceCents
	}
	return m
}

// agentStatOf 统计代理名下的卡：张数、已用张数、卡价值（按代理单价）、已结算/未结算金额
func agentStatOf(projectID string, agentID uint64, prices map[uint64]int64) gin.H {
	type row struct {
		TypeID uint64
		Cnt    int64
		Used   int64
		Settled int64
	}
	var rows []row
	database.DB.Model(&model.Card{}).
		Select("type_id, COUNT(*) AS cnt, SUM(CASE WHEN user_id IS NOT NULL THEN 1 ELSE 0 END) AS used, "+
			"SUM(CASE WHEN settle_id > 0 THEN 1 ELSE 0 END) AS settled").
		Where("project_id = ? AND agent_id = ?", projectID, agentID).
		Group("type_id").Scan(&rows)

	var cards, used, settledCards, value, settledValue, unsettledValue int64
	for _, r := range rows {
		p := prices[priceKey(agentID, r.TypeID)]
		cards += r.Cnt
		used += r.Used
		settledCards += r.Settled
		value += r.Cnt * p
		settledValue += r.Settled * p
		unsettledValue += (r.Cnt - r.Settled) * p
	}
	return gin.H{
		"agent_id": agentID,
		"cards":    cards, "used_cards": used,
		"settled_cards": settledCards, "unsettled_cards": cards - settledCards,
		"value_cents": value, "settled_value_cents": settledValue, "unsettled_value_cents": unsettledValue,
	}
}
