package handler

import (
	"errors"
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateCardTypeRequest 创建卡密类型请求。
// 兼容 v1：老客户端只传 {project_id, name, days}，等价于 kind=days、amount=days。
type CreateCardTypeRequest struct {
	ProjectID string `json:"project_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Days      int    `json:"days"`   // 兼容字段：kind=days 时的天数
	Kind      string `json:"kind"`   // v2：days | calls | credits（缺省 days）
	Amount    int    `json:"amount"` // v2：对应 kind 的数量（缺省用 days）
}

// CardTypeResponse 卡密类型响应
type CardTypeResponse struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Days      int    `json:"days"`
	Kind      string `json:"kind"`   // v2 新增
	Amount    int    `json:"amount"` // v2 新增
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListCardTypes 查询卡密类型【参数】project_id(必填) keyword 模糊搜索（仅未删除）
func (h *Handler) ListCardTypes(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := database.DB.Model(&model.CardType{}).Where("project_id = ? AND deleted_at IS NULL", projectID)
	if keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}

	var types []model.CardType
	if err := query.Order("id DESC").Find(&types).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]CardTypeResponse, 0, len(types))
	for _, t := range types {
		kind, amount := t.KindAmount() // 归一化：老数据（只有 days）也能正确显示
		list = append(list, CardTypeResponse{
			ID:        t.ID,
			ProjectID: t.ProjectID,
			Name:      t.Name,
			Days:      t.Days,
			Kind:      kind,
			Amount:    amount,
			CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: t.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// CreateCardType 创建卡密类型。
// 【参数】project_id、name、kind(days/calls/credits)、amount；
// 兼容 v1 的 days 字段：kind 为空时按 days 处理，amount 为空时用 days。
func (h *Handler) CreateCardType(c *gin.Context) {
	var req CreateCardTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、name 不能为空")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		util.Fail(c, util.CodeParamError, "参数错误：类型名不能为空")
		return
	}

	// kind/amount 归一化（老请求只有 days）
	kind := strings.TrimSpace(req.Kind)
	switch kind {
	case model.CardKindCalls, model.CardKindCredits:
		// ok
	default:
		kind = model.CardKindDays
	}
	amount := req.Amount
	if amount == 0 {
		amount = req.Days
	}
	if amount < 1 {
		util.Fail(c, util.CodeParamError, "数量必须大于 0（天数/次数/积分）")
		return
	}
	if kind == model.CardKindDays && req.Days > 0 && req.Amount > 0 && req.Days != req.Amount {
		util.Fail(c, util.CodeParamError, "参数错误：days 与 amount 不一致")
		return
	}

	// 项目必须存在且未删除
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在")
		return
	}

	var count int64
	database.DB.Model(&model.CardType{}).
		Where("project_id = ? AND name = ? AND deleted_at IS NULL", req.ProjectID, req.Name).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "类型名称已存在")
		return
	}

	ct := model.CardType{ProjectID: req.ProjectID, Name: req.Name, Kind: kind, Amount: amount}
	if kind == model.CardKindDays {
		ct.Days = amount // 双写：老客户端/老接口仍读 days
	}
	if err := database.DB.Create(&ct).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	h.recordAudit(c, model.AuditCardTypeCreate, ct.ProjectID, "card_type:"+strconv.FormatUint(ct.ID, 10),
		gin.H{"name": ct.Name, "kind": kind, "amount": amount})
	util.OK(c, gin.H{"id": ct.ID, "kind": kind, "amount": amount, "days": ct.Days})
}

// DeleteCardType 删除卡密类型（伪删除），防止卡密生成使用后被删掉或者报错
func (h *Handler) DeleteCardType(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var ct model.CardType
	if err := database.DB.First(&ct, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "卡密类型不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if ct.DeletedAt != nil {
		util.Fail(c, util.CodeConflict, "卡密类型已删除，请勿重复操作")
		return
	}
	now := timeNow()
	if err := database.DB.Model(&ct).Update("deleted_at", &now).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	h.recordAudit(c, model.AuditCardTypeDelete, ct.ProjectID, "card_type:"+strconv.FormatUint(ct.ID, 10),
		gin.H{"name": ct.Name})
	util.OK(c, nil)
}
