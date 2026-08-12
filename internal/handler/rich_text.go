package handler

import (
	"errors"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RichTextResponse 富文本广告内容
type RichTextResponse struct {
	ProjectID uint64 `json:"project_id"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

// GetRichText 富文本广告（按项目）：查询，每个项目初始为空
func (h *Handler) GetRichText(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var ad model.RichTextAd
	err := database.DB.Where("project_id = ?", projectID).First(&ad).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.OK(c, RichTextResponse{ProjectID: projectID, Content: ""})
			return
		}
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	util.OK(c, RichTextResponse{
		ProjectID: ad.ProjectID,
		Content:   ad.Content,
		UpdatedAt: ad.UpdatedAt.Format("2006-01-02 15:04:05"),
	})
}

// SaveRichTextRequest 保存富文本广告请求
type SaveRichTextRequest struct {
	Content string `json:"content"`
}

// SaveRichText 富文本广告（按项目）：编辑保存（upsert）
func (h *Handler) SaveRichText(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req SaveRichTextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}

	var ad model.RichTextAd
	err := database.DB.Where("project_id = ?", projectID).First(&ad).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ad = model.RichTextAd{ProjectID: projectID, Content: req.Content}
		if err := database.DB.Create(&ad).Error; err != nil {
			util.Fail(c, util.CodeDBError, "保存失败")
			return
		}
	} else {
		if err := database.DB.Model(&ad).Update("content", req.Content).Error; err != nil {
			util.Fail(c, util.CodeDBError, "保存失败")
			return
		}
	}
	util.OK(c, RichTextResponse{ProjectID: projectID, Content: req.Content})
}
