package handler

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// 代理订单：每次「批量生成代理卡」自动落一张订单（单价快照）。
// 结算在这里做：可整单/部分结算，可只结「已使用」的；订单行尾可导出该单的卡与结算状态。

// ListAgentOrders 订单列表（带已结算/未结算张数与金额）
//
//	GET /api/admin/projects/:id/agent-orders?agent_id=&status=all|unsettled|settled|partial&card_type_id=
func (h *Handler) ListAgentOrders(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不能为空")
		return
	}
	q := database.DB.Model(&model.AgentOrder{}).Where("project_id = ?", projectID)
	if v := strings.TrimSpace(c.Query("agent_id")); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			q = q.Where("agent_id = ?", id)
		}
	}
	if v := strings.TrimSpace(c.Query("card_type_id")); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			q = q.Where("card_type_id = ?", id)
		}
	}
	var orders []model.AgentOrder
	if err := q.Order("id DESC").Limit(500).Find(&orders).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	status := strings.TrimSpace(c.Query("status")) // all / unsettled / settled / partial
	list := make([]gin.H, 0, len(orders))
	for i := range orders {
		o := orders[i]
		var total, settled int64
		database.DB.Model(&model.Card{}).Where("order_id = ?", o.ID).Count(&total)
		database.DB.Model(&model.Card{}).Where("order_id = ? AND settle_id > 0", o.ID).Count(&settled)
		unsettled := total - settled
		st := "unsettled"
		if total > 0 && settled == total {
			st = "settled"
		} else if settled > 0 {
			st = "partial"
		}
		if status != "" && status != "all" && status != st {
			continue
		}
		unit := o.UnitCents
		list = append(list, gin.H{
			"id": o.ID, "project_id": o.ProjectID, "agent_id": o.AgentID, "agent_name": o.AgentName,
			"card_type_id": o.CardTypeID, "type_name": o.TypeName,
			"count": o.Count, "unit_cents": unit, "amount_cents": o.AmountCents,
			"settled_count": settled, "unsettled_count": unsettled,
			"settled_amount_cents": settled * unit, "unsettled_amount_cents": unsettled * unit,
			"settle_status": st, "remark": o.Remark, "operator": o.Operator,
			"created_at": o.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// SettleAgentOrders 结算：对勾选的订单（可多选）结算它们**未结算**的卡。
//
//	POST /api/admin/agent-orders/settle  {order_ids:[1,2], used_only:true, remark:"10月对账"}
//
// ExportAgentOrder 导出订单：该订单每张卡一行，标明已结算/未结算（CSV，Excel 可直接打开）。
//
//	GET /api/admin/agent-orders/:id/export
func (h *Handler) ExportAgentOrder(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var o model.AgentOrder
	if err := database.DB.First(&o, id).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "订单不存在")
		return
	}
	type row struct {
		CDKey     string `gorm:"column:cdkey"`
		UserID    *uint64
		Username  string
		UsedAt    *string
		SettleID  uint64
		SettledAt *string
	}
	var rows []row
	database.DB.Table("card AS cd").
		Select("cd.cdkey, cd.user_id, cd.settle_id, u.username AS username, "+
			"DATE_FORMAT(cd.used_at, '%Y-%m-%d %H:%i:%s') AS used_at, "+
			"DATE_FORMAT(cd.settled_at, '%Y-%m-%d %H:%i:%s') AS settled_at").
		Joins("LEFT JOIN user AS u ON u.id = cd.user_id").
		Where("cd.order_id = ?", o.ID).Order("cd.id ASC").Scan(&rows)

	// format=txt → 与表格同样的字段,制表符分隔(粘到 Excel/WPS 自动分列);
	// 默认 csv 表格(逗号分隔,Excel 双击直接打开)
	if strings.TrimSpace(c.Query("format")) == "txt" {
		var tbuf bytes.Buffer
		tbuf.WriteString("\xEF\xBB\xBF")
		unitYuanTxt := strconv.FormatFloat(float64(o.UnitCents)/100, 'f', 2, 64)
		tbuf.WriteString(strings.Join([]string{"订单号", "代理", "卡密类型", "单价(元)", "卡号", "结算状态", "使用人", "使用时间", "结算单号", "结算时间"}, "\t") + "\r\n")
		for _, r := range rows {
			state := "未结算"
			if r.SettleID > 0 {
				state = "已结算"
			}
			user := r.Username
			if user == "" {
				user = "-"
			}
			usedAt := "-"
			if r.UsedAt != nil {
				usedAt = *r.UsedAt
			}
			settleNo := "-"
			settledAt := "-"
			if r.SettleID > 0 {
				settleNo = strconv.FormatUint(r.SettleID, 10)
				if r.SettledAt != nil {
					settledAt = *r.SettledAt
				}
			}
			tbuf.WriteString(strings.Join([]string{
				strconv.FormatUint(o.ID, 10), o.AgentName, o.TypeName, unitYuanTxt, r.CDKey,
				state, user, usedAt, settleNo, settledAt,
			}, "\t") + "\r\n")
		}
		tname := "agent-order-" + strconv.FormatUint(o.ID, 10) + ".txt"
		c.Header("Content-Disposition", "attachment; filename=\""+tname+"\"")
		c.Data(200, "text/plain; charset=utf-8", tbuf.Bytes())
		return
	}

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM：Excel 打开中文不乱码
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"订单号", "代理", "卡密类型", "单价(元)", "卡号", "结算状态", "使用人", "使用时间", "结算单号", "结算时间"})
	unitYuan := strconv.FormatFloat(float64(o.UnitCents)/100, 'f', 2, 64)
	for _, r := range rows {
		state := "未结算"
		if r.SettleID > 0 {
			state = "已结算"
		}
		user := r.Username
		if user == "" {
			user = "-"
		}
		usedAt := "-"
		if r.UsedAt != nil {
			usedAt = *r.UsedAt
		}
		settleNo := "-"
		settledAt := "-"
		if r.SettleID > 0 {
			settleNo = strconv.FormatUint(r.SettleID, 10)
			if r.SettledAt != nil {
				settledAt = *r.SettledAt
			}
		}
		_ = w.Write([]string{
			strconv.FormatUint(o.ID, 10), o.AgentName, o.TypeName, unitYuan, r.CDKey,
			state, user, usedAt, settleNo, settledAt,
		})
	}
	w.Flush()

	name := "agent-order-" + strconv.FormatUint(o.ID, 10) + ".csv"
	c.Header("Content-Disposition", "attachment; filename=\""+name+"\"")
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}
