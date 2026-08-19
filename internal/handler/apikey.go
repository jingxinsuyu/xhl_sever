package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"xhl-server/internal/baidu/qrlogin"
	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ApiKeyItem 后台 API Key 列表项。
type ApiKeyItem struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	Remark    string `json:"remark"`
	Status    int8   `json:"status"` // 1 启用 / 0 禁用
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListApiKeys 后台查询 API Key【参数】project_id 按项目，keyword 名称模糊，page、page_size 分页。
func (h *Handler) ListApiKeys(c *gin.Context) {
	page, pageSize := parsePage(c)
	projectID := strings.TrimSpace(c.Query("project_id"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := database.DB.Model(&model.ApiKey{})
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var rows []model.ApiKey
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]ApiKeyItem, 0, len(rows))
	for _, r := range rows {
		list = append(list, ApiKeyItem{
			ID:        r.ID,
			ProjectID: r.ProjectID,
			Name:      r.Name,
			Key:       r.Key,
			Remark:    r.Remark,
			Status:    r.Status,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// CreateApiKeyRequest 创建 API Key 请求。
type CreateApiKeyRequest struct {
	ProjectID string `json:"project_id" binding:"required"` // 所属项目 id
	Name      string `json:"name" binding:"required"`       // 名称
	Remark    string `json:"remark"`                        // 备注
}

// CreateApiKey 后台创建 API Key：服务端生成 sk- 开头的 key，返回完整 key（仅创建时可见）。
func (h *Handler) CreateApiKey(c *gin.Context) {
	var req CreateApiKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、name 不能为空")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Name = strings.TrimSpace(req.Name)
	req.Remark = strings.TrimSpace(req.Remark)
	if !isProjectID(req.ProjectID) {
		util.Fail(c, util.CodeParamError, "project_id 应为 6 位数字")
		return
	}
	// 校验项目存在（不能给不存在的项目发 key）
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}

	key, err := uniqueApiKey()
	if err != nil {
		util.Fail(c, util.CodeDBError, "生成 key 失败")
		return
	}
	ak := model.ApiKey{
		ProjectID: req.ProjectID,
		Name:      req.Name,
		Key:       key,
		Remark:    req.Remark,
		Status:    1,
	}
	if err := database.DB.Create(&ak).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	util.OK(c, gin.H{"id": ak.ID, "key": ak.Key})
}

// UpdateApiKeyStatusRequest 更新 API Key 状态请求。
type UpdateApiKeyStatusRequest struct {
	Status int8 `json:"status" binding:"oneof=0 1"` // 1 启用 / 0 禁用
}

// UpdateApiKeyStatus 后台启用/禁用 API Key。
func (h *Handler) UpdateApiKeyStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateApiKeyStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：status 不能为空")
		return
	}
	var ak model.ApiKey
	if err := database.DB.First(&ak, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "API Key 不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Model(&ak).Update("status", req.Status).Error; err != nil {
		util.Fail(c, util.CodeDBError, "操作失败")
		return
	}
	util.OK(c, nil)
}

// DeleteApiKey 后台删除 API Key。
func (h *Handler) DeleteApiKey(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var ak model.ApiKey
	if err := database.DB.First(&ak, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "API Key 不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Delete(&ak).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	util.OK(c, nil)
}

// OpenQrLoginRequest 第三方扫码确认请求（明文，不加密）。
type OpenQrLoginRequest struct {
	QrURL string `json:"qrUrl" binding:"required"` // 二维码内容（登录链接，含 sign）
	Ck    string `json:"ck" binding:"required"`    // 百度账号 cookie 串
}

// OpenQrLogin 第三方扫码确认（JSON 版，非 SSE）。
// 鉴权：请求头 xhlkey（按项目绑定的 API Key）。项目上下文取自 key 的 ProjectID。
// 成功后将 cookie 写入 cookie 库（user_id=0 第三方来源，随机账号/密码占位）。
func (h *Handler) OpenQrLogin(c *gin.Context) {
	ak := middleware.GetApiKey(c)
	if ak == nil {
		util.Fail(c, util.CodeUnauthorized, "xhlkey 无效")
		return
	}
	var req OpenQrLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：qrUrl、ck 不能为空")
		return
	}
	req.QrURL = strings.TrimSpace(req.QrURL)
	req.Ck = strings.TrimSpace(req.Ck)
	if req.QrURL == "" || req.Ck == "" {
		util.Fail(c, util.CodeParamError, "参数错误：qrUrl、ck 不能为空")
		return
	}

	// 按 key 所属项目校验项目存在
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", ak.ProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}

	// 代理池：未配置则不走代理
	proxyAddr := ""
	if h.proxyURL() != "" {
		proxyAddr = h.fetchProxy()
		if proxyAddr == "" {
			util.Fail(c, util.CodeNoPermission, "加载网络环境失败1000")
			return
		}
	}

	res, err := qrlogin.Confirm(req.QrURL, req.Ck, proxyAddr)
	if err != nil {
		util.Fail(c, util.CodeParamError, "登录失败："+err.Error())
		return
	}
	if res.OK {
		// 第三方来源 user_id=0，随机账号/密码占位（关键是 cookie）
		h.saveCkData(0, randomCredential(), randomCredential(), req.Ck)
	}
	util.OK(c, gin.H{"ok": res.OK, "errno": res.Errno, "code": res.Code, "message": res.Message})
}

// generateApiKey 生成 DeepSeek 风格 key：sk- + 32 位 hex（16 随机字节）。
func generateApiKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(b), nil
}

// uniqueApiKey 生成不重复的 key（uniqueIndex 冲突时重试）。
func uniqueApiKey() (string, error) {
	for i := 0; i < 5; i++ {
		key, err := generateApiKey()
		if err != nil {
			return "", err
		}
		var count int64
		if err := database.DB.Model(&model.ApiKey{}).Where("key = ?", key).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return key, nil
		}
	}
	return "", errors.New("生成 key 冲突")
}

// randomCredential 随机占位账号/密码：7~9 位字母数字（对齐客户端 transferCredential）。
var credentialChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomCredential() string {
	n := 7 + randIntn(3) // 7~9
	b := make([]byte, n)
	for i := range b {
		b[i] = credentialChars[randIntn(len(credentialChars))]
	}
	return string(b)
}

// randIntn 返回 [0, n) 的随机整数（crypto/rand）。
func randIntn(n int) int {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	return int(b[0]) % n
}
