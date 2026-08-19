package handler

import (
	"errors"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateCardTypeRequest 创建卡密类型请求
type CreateCardTypeRequest struct {
	ProjectID string `json:"project_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Days      int    `json:"days" binding:"required"` // 充值天数
}

// CardTypeResponse 卡密类型响应
type CardTypeResponse struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Days      int    `json:"days"`
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
		list = append(list, CardTypeResponse{
			ID:        t.ID,
			ProjectID: t.ProjectID,
			Name:      t.Name,
			Days:      t.Days,
			CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: t.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// CreateCardType 创建卡密类型【参数】项目id 类型名 充值天数
func (h *Handler) CreateCardType(c *gin.Context) {
	var req CreateCardTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、name、days 不能为空")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		util.Fail(c, util.CodeParamError, "参数错误：类型名不能为空")
		return
	}
	if req.Days < 1 {
		util.Fail(c, util.CodeParamError, "days 必须大于 0")
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

	ct := model.CardType{ProjectID: req.ProjectID, Name: req.Name, Days: req.Days}
	if err := database.DB.Create(&ct).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	util.OK(c, gin.H{"id": ct.ID})
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
	util.OK(c, nil)
}
