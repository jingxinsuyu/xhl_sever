package handler

import (
	"os"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
)

// Handler 聚合所有接口处理器，持有全局配置、Redis 客户端与图形验证码实例
type Handler struct {
	Config  *config.Config
	Redis   *redis.Client
	Captcha *base64Captcha.Captcha
}

// New 创建 Handler
func New(cfg *config.Config, rdb *redis.Client) *Handler {
	ttl := time.Duration(cfg.Security.CaptchaExpireSeconds) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	store := &captchaRedisStore{rdb: rdb, ttl: ttl}
	// 图片尺寸不变（80×240），改用 TTF 字体渲染：数字更大、更清晰
	// NewDriverString(高, 宽, 噪点数, 干扰线, 长度, 数字源, 背景, 字体存储, 字体)
	driver := base64Captcha.NewDriverString(80, 240, 25, 0, 4, "0123456789", nil, nil, []string{"actionj.ttf"})
	return &Handler{
		Config:  cfg,
		Redis:   rdb,
		Captcha: base64Captcha.NewCaptcha(driver, store),
	}
}

// timeNow 当前时间
func timeNow() time.Time { return time.Now() }

// clientIP 获取真实客户端 IP：优先取 nginx 转发的头，兜底取连接地址
func clientIP(c *gin.Context) string {
	ip := strings.TrimSpace(c.GetHeader("X-Real-IP"))
	if ip == "" {
		xff := strings.TrimSpace(c.GetHeader("X-Forwarded-For"))
		if xff != "" {
			ip = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	return ip
}

// parsePage 解析分页参数，默认 page=1、page_size=10，最大 100
func parsePage(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02 15:04:05")
	return &s
}

// parseID 解析路径 id 参数
func parseID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// parseProjectID 解析路径中的项目 id（6 位数字字符串，弃用自增 id 后）。
func parseProjectID(c *gin.Context, name string) (string, bool) {
	id := strings.TrimSpace(c.Param(name))
	if !isProjectID(id) {
		return "", false
	}
	return id, true
}

// isProjectID 校验项目 id：6 位纯数字。
func isProjectID(id string) bool {
	if len(id) != 6 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// uploadRoot 上传根目录（绝对路径，锚定软件根目录，不受启动目录影响）
func (h *Handler) uploadRoot() string {
	return h.Config.UploadDir()
}

// ensureDir 确保目录存在
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// fileURL 相对存储路径 → 对外可访问 URL（BaseURL 为空则返回相对 /uploads/ 路径）
func (h *Handler) fileURL(relPath string) string {
	if relPath == "" {
		return ""
	}
	rel := strings.TrimLeft(relPath, "/")
	if rel == "" {
		return ""
	}
	base := strings.TrimRight(h.Config.Upload.BaseURL, "/")
	if base == "" {
		return "/" + rel
	}
	return base + "/" + rel
}
