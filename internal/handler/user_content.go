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

// UserCarouselItem 用户端轮播图项
type UserCarouselItem struct {
	ID       uint64 `json:"id"`
	ImageURL string `json:"image_url"`
	Link     string `json:"link"`
}

// GetUserCarousels 获取项目轮播图数据（无需登录）。
// 参数 project_id；返回该项目的轮播图列表 [{id, image_url, link}]，链接为空表示不跳转。
func (h *Handler) GetUserCarousels(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	var carousels []model.Carousel
	if err := database.DB.Where("project_id = ?", projectID).Order("id DESC").Find(&carousels).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]UserCarouselItem, 0, len(carousels))
	for _, ca := range carousels {
		list = append(list, UserCarouselItem{
			ID:       ca.ID,
			ImageURL: h.fileURL(ca.ImagePath),
			Link:     ca.Link,
		})
	}
	util.OK(c, list)
}

// GetUserAd 获取项目广告内容（富文本，无需登录）。
// 参数 project_id；返回 {project_id, content, updated_at}，无内容时 content 为空字符串。
func (h *Handler) GetUserAd(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	var ad model.RichTextAd
	err := database.DB.Where("project_id = ?", projectID).First(&ad).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.OK(c, gin.H{"project_id": projectID, "content": "", "updated_at": ""})
			return
		}
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	util.OK(c, gin.H{
		"project_id": ad.ProjectID,
		"content":    ad.Content,
		"updated_at": ad.UpdatedAt.Format("2006-01-02 15:04:05"),
	})
}
