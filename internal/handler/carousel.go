package handler

import (
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 轮播图目标尺寸（需求：裁剪成 380*65 尺寸的 jpg）
const (
	carouselWidth  = 380
	carouselHeight = 65
)

// CarouselResponse 轮播图列表项
type CarouselResponse struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"project_id"`
	ImageURL  string `json:"image_url"`
	Link      string `json:"link"`
	CreatedAt string `json:"created_at"`
}

// ListCarousels 轮播图（按项目）：查询列表
func (h *Handler) ListCarousels(c *gin.Context) {
	projectID, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var carousels []model.Carousel
	if err := database.DB.Where("project_id = ?", projectID).Order("id DESC").Find(&carousels).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]CarouselResponse, 0, len(carousels))
	for _, ca := range carousels {
		list = append(list, CarouselResponse{
			ID:        ca.ID,
			ProjectID: ca.ProjectID,
			ImageURL:  h.fileURL(ca.ImagePath),
			Link:      ca.Link,
			CreatedAt: ca.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// CreateCarousel 添加轮播图【multipart】：image=图片 + link=跳转连接 + apply_all=1 则同时为所有项目添加
func (h *Handler) CreateCarousel(c *gin.Context) {
	projectID, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	applyAll := c.PostForm("apply_all") == "1" || c.PostForm("apply_all") == "true"
	link := strings.TrimSpace(c.PostForm("link"))

	fileHeader, err := c.FormFile("image")
	if err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：请选择图片")
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		util.Fail(c, util.CodeDBError, "读取上传文件失败")
		return
	}
	defer src.Close()

	// 裁剪为 380×65 jpg
	relPath, err := h.processCarouselImage(src)
	if err != nil {
		util.Fail(c, util.CodeParamError, err.Error())
		return
	}

	// 目标项目列表
	projectIDs := []string{projectID}
	if applyAll {
		var projects []model.Project
		if err := database.DB.Where("deleted_at IS NULL").Select("id").Find(&projects).Error; err != nil {
			util.Fail(c, util.CodeDBError, "系统错误")
			return
		}
		if len(projects) == 0 {
			util.Fail(c, util.CodeNotFound, "暂无项目可添加")
			return
		}
		projectIDs = projectIDs[:0]
		for _, p := range projects {
			projectIDs = append(projectIDs, p.ID)
		}
	}

	created := make([]uint64, 0, len(projectIDs))
	for _, pid := range projectIDs {
		ca := model.Carousel{ProjectID: pid, ImagePath: relPath, Link: link}
		if err := database.DB.Create(&ca).Error; err != nil {
			// 部分失败不影响已创建项
			continue
		}
		created = append(created, ca.ID)
	}
	util.OK(c, gin.H{"count": len(created), "ids": created, "image_url": h.fileURL(relPath)})
}

// DeleteCarousel 删除轮播图
func (h *Handler) DeleteCarousel(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var ca model.Carousel
	if err := database.DB.First(&ca, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "轮播图不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Delete(&ca).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	util.OK(c, nil)
}

// processCarouselImage 读取上传图片并处理为 380×65 jpg，返回存储相对路径。
func (h *Handler) processCarouselImage(src io.Reader) (string, error) {
	img, err := imaging.Decode(src)
	if err != nil {
		return "", fmt.Errorf("图片解码失败，请上传有效图片")
	}
	// Fill：缩放并居中裁剪到精确 380×65
	img = imaging.Fill(img, carouselWidth, carouselHeight, imaging.Center, imaging.Lanczos)

	dir := filepath.Join(h.uploadRoot(), "image")
	if err := ensureDir(dir); err != nil {
		return "", fmt.Errorf("创建上传目录失败")
	}
	name := fmt.Sprintf("%d.jpg", timeNow().UnixMilli())
	relPath := filepath.ToSlash(filepath.Join("uploads", "image", name))
	absPath := filepath.Join(dir, name)

	f, err := os.Create(absPath)
	if err != nil {
		return "", fmt.Errorf("保存文件失败")
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		return "", fmt.Errorf("图片编码失败")
	}
	return relPath, nil
}
