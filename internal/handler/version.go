package handler

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 版本平台合法值
func validPlatform(p string) bool {
	switch p {
	case model.PlatformAndroid, model.PlatformPC, model.PlatformIOS:
		return true
	}
	return false
}

// VersionResponse 版本列表项
type VersionResponse struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"project_id"`
	Platform  string `json:"platform"` // android / pc / ios
	Version   string `json:"version"`
	FileName  string `json:"file_name"`
	FileURL   string `json:"file_url"`
	FileSize  int64  `json:"file_size"`
	CreatedAt string `json:"created_at"`
}

// ListVersions 版本管理（按项目）：查询项目版本列表（可选按平台过滤）
func (h *Handler) ListVersions(c *gin.Context) {
	projectID, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))

	query := database.DB.Where("project_id = ?", projectID)
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	var versions []model.Version
	if err := query.Order("id DESC").Find(&versions).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]VersionResponse, 0, len(versions))
	for _, v := range versions {
		list = append(list, VersionResponse{
			ID:        v.ID,
			ProjectID: v.ProjectID,
			Platform:  v.Platform,
			Version:   v.Version,
			FileName:  v.FileName,
			FileURL:   h.fileURL(v.FilePath),
			FileSize:  v.FileSize,
			CreatedAt: v.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// UploadVersion 更新版本【multipart】：字段 platform=平台(android/pc/ios) + version=版本号 + 上传更新文件
func (h *Handler) UploadVersion(c *gin.Context) {
	projectID, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}

	// 项目必须存在且未删除
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", projectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在")
		return
	}

	platform := strings.TrimSpace(c.PostForm("platform"))
	if !validPlatform(platform) {
		util.Fail(c, util.CodeParamError, "参数错误：platform 只能为 android / pc / ios")
		return
	}

	versionNo := strings.TrimSpace(c.PostForm("version"))
	if versionNo == "" {
		util.Fail(c, util.CodeParamError, "参数错误：version（版本号）不能为空")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：请选择上传文件")
		return
	}
	if fileHeader.Size <= 0 {
		util.Fail(c, util.CodeParamError, "上传文件不能为空")
		return
	}

	dir := filepath.Join(h.uploadRoot(), "app", projectID)
	if err := ensureDir(dir); err != nil {
		util.Fail(c, util.CodeDBError, "创建上传目录失败")
		return
	}

	filename := sanitizeFileName(fileHeader.Filename)
	relPath := filepath.Join("uploads", "app", projectID,
		fmt.Sprintf("%d_%s", timeNow().UnixMilli(), filename))
	absPath := filepath.Join(h.uploadRoot(), "app", projectID,
		fmt.Sprintf("%d_%s", timeNow().UnixMilli(), filename))

	if err := c.SaveUploadedFile(fileHeader, absPath); err != nil {
		util.Fail(c, util.CodeDBError, "保存文件失败")
		return
	}

	v := model.Version{
		ProjectID: projectID,
		Platform:  platform,
		Version:   versionNo,
		FileName:  filename,
		FilePath:  filepath.ToSlash(relPath),
		FileSize:  fileHeader.Size,
	}
	if err := database.DB.Create(&v).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存版本信息失败")
		return
	}
	util.OK(c, gin.H{
		"id":        v.ID,
		"platform":  v.Platform,
		"version":   v.Version,
		"file_name": v.FileName,
		"file_url":  h.fileURL(v.FilePath),
		"file_size": v.FileSize,
	})
}

// UserUpdate 用户端检测更新【无需登录】：按 项目+平台(android/pc/ios) 返回最新版本下载链接。
// 参数 project_id、platform；该平台暂无版本时 data 为 null。
func (h *Handler) UserUpdate(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if !validPlatform(platform) {
		util.Fail(c, util.CodeParamError, "参数错误：platform 只能为 android / pc / ios")
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

	var v model.Version
	err := database.DB.Where("project_id = ? AND platform = ?", projectID, platform).Order("id DESC").First(&v).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.OK(c, nil) // 该平台暂无版本
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	util.OK(c, gin.H{
		"platform":   v.Platform,
		"version":    v.Version,
		"file_name":  v.FileName,
		"file_url":   h.fileURL(v.FilePath),
		"file_size":  v.FileSize,
		"created_at": v.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// DeleteVersion 删除版本记录
func (h *Handler) DeleteVersion(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var v model.Version
	if err := database.DB.First(&v, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "版本不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Delete(&v).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	util.OK(c, nil)
}

// sanitizeFileName 清洗文件名，去除路径分隔符与危险字符
func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\x00', '\n', '\r':
			return '_'
		}
		return r
	}, name)
	if name == "" {
		name = "file"
	}
	return name
}
