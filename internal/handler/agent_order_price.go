package handler

import (
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// UpdateAgentOrderPrice 改订单单价（补/改下单时的单价快照）并重算订单金额。
// 用途：开卡时该代理还没设价 → 快照 0，事后用这里把单价补上，订单金额与代理统计就一致了。
//
//	PUT /api/admin/agent-orders/:id/price  {unit_cents: 1500}
func (h *Handler) UpdateAgentOrderPrice(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req struct {
		UnitCents *int64  `json:"unit_cents"`
		Remark    *string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	var o model.AgentOrder
	if err := database.DB.First(&o, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "订单不存在")
		return
	}
	updates := map[string]any{}
	if req.UnitCents != nil {
		if *req.UnitCents < 0 || *req.UnitCents > 100000000 {
			util.Fail(c, util.CodeParamError, "单价不合法")
			return
		}
		updates["unit_cents"] = *req.UnitCents
		updates["amount_cents"] = *req.UnitCents * int64(o.Count)
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	if len(updates) == 0 {
		util.OK(c, gin.H{"id": o.ID})
		return
	}
	if err := database.DB.Model(&model.AgentOrder{}).Where("id = ?", o.ID).Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	operator := ""
	if cl := middleware.GetClaims(c); cl != nil {
		operator = cl.Username
	}
	h.recordAudit(c, model.AuditCardGenerate, o.ProjectID, "agent_order:"+strconv.FormatUint(o.ID, 10),
		gin.H{"unit_cents": updates["unit_cents"], "operator": operator})
	util.OK(c, gin.H{"id": o.ID, "unit_cents": updates["unit_cents"], "amount_cents": updates["amount_cents"]})
}
