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

// SettleOrderByCards 按「卡密」结算某张订单：弹窗里一行一个卡密，只结算这些卡。
// 只认属于本订单、且未结算的卡；其他一律跳过并说明原因。
//
//	POST /api/admin/agent-orders/settle-by-cards
//	{order_id, cdkeys:["xxx","yyy"], remark:"10月对账"}
//
// 返回 settled = **本次真正结算掉的卡密**（不是整单的卡），failed = 跳过的及原因。
func (h *Handler) SettleOrderByCards(c *gin.Context) {
	var req struct {
		OrderID uint64   `json:"order_id" binding:"required"`
		CDKeys  []string `json:"cdkeys" binding:"required"`
		Remark  string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.OrderID == 0 || len(req.CDKeys) == 0 {
		util.Fail(c, util.CodeParamError, "参数错误：order_id、cdkeys 不能为空")
		return
	}
	if len(req.CDKeys) > 5000 {
		util.Fail(c, util.CodeParamError, "一次最多结算 5000 个卡密")
		return
	}
	var o model.AgentOrder
	if err := database.DB.First(&o, req.OrderID).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "订单不存在")
		return
	}

	// 去重（同一张卡输入多次只算一次）
	seen := make(map[string]bool, len(req.CDKeys))
	keys := make([]string, 0, len(req.CDKeys))
	for _, k := range req.CDKeys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		util.Fail(c, util.CodeParamError, "没有有效的卡密")
		return
	}

	var cards []model.Card
	if err := database.DB.Where("cdkey IN ?", keys).Find(&cards).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询卡密失败")
		return
	}
	byKey := make(map[string]model.Card, len(cards))
	for _, cd := range cards {
		byKey[cd.CDKey] = cd
	}

	type failed struct {
		CDKey  string `json:"cdkey"`
		Reason string `json:"reason"`
	}
	settled := make([]string, 0, len(keys))
	failedList := make([]failed, 0)
	ids := make([]uint64, 0, len(keys))
	used := 0
	for _, k := range keys {
		cd, ok := byKey[k]
		if !ok {
			failedList = append(failedList, failed{k, "卡密不存在"})
			continue
		}
		if cd.OrderID != o.ID {
			failedList = append(failedList, failed{k, "不属于该订单"})
			continue
		}
		if cd.SettleID != 0 {
			failedList = append(failedList, failed{k, "已结算过"})
			continue
		}
		ids = append(ids, cd.ID)
		settled = append(settled, k)
		if cd.UserID != nil {
			used++
		}
	}
	if len(ids) == 0 {
		util.Fail(c, util.CodeNotFound, "这些卡密里没有可结算的（可能已结算或不属于该订单）")
		return
	}

	amount := int64(len(ids)) * o.UnitCents
	operator := ""
	if cl := middleware.GetClaims(c); cl != nil {
		operator = cl.Username
	}
	st := model.CardSettlement{
		ProjectID: o.ProjectID, AgentID: o.AgentID, AgentName: o.AgentName, OrderID: o.ID,
		CardCount: len(ids), UsedCount: used, AmountCents: amount,
		Remark: strings.TrimSpace(req.Remark), Operator: operator,
	}
	if err := database.DB.Create(&st).Error; err != nil {
		util.Fail(c, util.CodeDBError, "结算失败")
		return
	}
	now := timeNow()
	database.DB.Model(&model.Card{}).Where("id IN ?", ids).
		Updates(map[string]any{"settle_id": st.ID, "settled_at": now})

	h.recordAudit(c, model.AuditCardGenerate, o.ProjectID, "settlement:"+strconv.FormatUint(st.ID, 10),
		gin.H{"order": o.ID, "agent": o.AgentName, "cards": len(ids), "amount_cents": amount})

	util.OK(c, gin.H{
		"settlement_id": st.ID, "order_id": o.ID, "unit_cents": o.UnitCents,
		"settled": settled, "failed": failedList,
		"card_count": len(ids), "used_count": used, "amount_cents": amount,
	})
}
