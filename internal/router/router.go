package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"xhl-server/internal/config"
	"xhl-server/internal/handler"
	"xhl-server/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// New 组装路由
func New(cfg *config.Config, rdb *redis.Client) *gin.Engine {
	r := gin.Default()
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
	})

	// 静态资源（上传的图片 / 软件文件，锚定软件根目录）
	r.Static("/uploads", cfg.UploadDir())

	// 前端静态托管 + SPA 回退（Vue history 路由）：非 /api、/uploads 的路径，
	// 存在则返回静态文件，否则回退 index.html，保证前端路由刷新不 404。
	// 这样前端 dist 由后端直接托管，无需额外 nginx。
	webDir := cfg.StaticDir()
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		// 精确匹配 /api/、/uploads/ 前缀（不能只判 /api，否则 /apikeys 这类前端路由被误拦）
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/uploads/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
			return
		}
		fp := filepath.Join(webDir, filepath.Clean(p))
		if fi, err := os.Stat(fp); err == nil && !fi.IsDir() {
			c.File(fp)
			return
		}
		c.File(filepath.Join(webDir, "index.html"))
	})

	h := handler.New(cfg, rdb)

	// 图形验证码（无需登录）
	r.GET("/api/captcha", h.GetCaptcha)

	// 百度扫码确认（需登录 + 100001 项目会员，SSE 流式）
	r.POST("/api/xhl/qrlogin", middleware.AuthUser(cfg.JWT.Secret), h.QrLogin)

	// 第三方开放接口（API Key 鉴权，xhlkey 请求头；明文 JSON，按 key 所属项目）
	r.POST("/api/open/qrlogin", middleware.AuthApiKey(), h.OpenQrLogin)

	user := r.Group("/api/user")
	{
		user.POST("/init", middleware.AuthUser(cfg.JWT.Secret), h.UserInit) // 登录有效性校验
		user.POST("/register", h.Register)
		user.POST("/login", h.UserLogin)
		user.POST("/exchange", h.Exchange) // 兑换按用户名，无需登录
		user.POST("/unbind", h.UserUnbind)

		// 用户端内容接口（无需登录）
		user.GET("/carousels", h.GetUserCarousels)
		user.GET("/ad", h.GetUserAd)
		user.GET("/update", h.UserUpdate) // 检测更新：按项目+平台返回下载链接
	}

	admin := r.Group("/api/admin")
	{
		// 登录无需鉴权
		admin.POST("/login", h.AdminLogin)

		// 管理员管理（仅超级管理员可增改/冻结）
		admin.GET("/admins", middleware.AuthAdmin(cfg.JWT.Secret), h.ListAdmins)
		admin.POST("/admins", middleware.AuthSuper(cfg.JWT.Secret), h.CreateAdmin)
		admin.PUT("/admins/:id", middleware.AuthSuper(cfg.JWT.Secret), h.UpdateAdmin)
		admin.PUT("/admins/:id/status", middleware.AuthSuper(cfg.JWT.Secret), h.UpdateAdminStatus)

		// 用户管理
		admin.GET("/users", middleware.AuthAdmin(cfg.JWT.Secret), h.ListUsers)
		admin.GET("/users/:id/membership", middleware.AuthAdmin(cfg.JWT.Secret), h.GetUserMembership)
		admin.POST("/users/:id/clear-login", middleware.AuthAdmin(cfg.JWT.Secret), h.ClearUserLoginCount)
		admin.PUT("/users/:id/status", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateUserStatus)
		admin.PUT("/users/:id/password", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateUserPassword)
		admin.POST("/users/:id/unbind", middleware.AuthAdmin(cfg.JWT.Secret), h.UnbindUser)

		// 项目管理
		admin.GET("/projects", middleware.AuthAdmin(cfg.JWT.Secret), h.ListProjects)
		admin.POST("/projects", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateProject)
		admin.PUT("/projects/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateProject)
		admin.DELETE("/projects/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteProject)

		// 版本管理（按项目）
		admin.GET("/projects/:id/versions", middleware.AuthAdmin(cfg.JWT.Secret), h.ListVersions)
		admin.POST("/projects/:id/versions", middleware.AuthAdmin(cfg.JWT.Secret), h.UploadVersion)
		admin.DELETE("/versions/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteVersion)

		// 项目变量（按项目）
		admin.GET("/projects/:id/variables", middleware.AuthAdmin(cfg.JWT.Secret), h.ListVariables)
		admin.POST("/projects/:id/variables", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateVariable)
		admin.PUT("/variables/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateVariable)
		admin.DELETE("/variables/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteVariable)

		// 轮播图（按项目）
		admin.GET("/projects/:id/carousels", middleware.AuthAdmin(cfg.JWT.Secret), h.ListCarousels)
		admin.POST("/projects/:id/carousels", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateCarousel)
		admin.DELETE("/carousels/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteCarousel)

		// 富文本广告（按项目）
		admin.GET("/projects/:id/rich-text", middleware.AuthAdmin(cfg.JWT.Secret), h.GetRichText)
		admin.PUT("/projects/:id/rich-text", middleware.AuthAdmin(cfg.JWT.Secret), h.SaveRichText)

		// 卡密类型（按项目）
		admin.GET("/card-types", middleware.AuthAdmin(cfg.JWT.Secret), h.ListCardTypes)
		admin.POST("/card-types", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateCardType)
		admin.DELETE("/card-types/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteCardType)

		// 卡密（按项目）
		admin.GET("/cards", middleware.AuthAdmin(cfg.JWT.Secret), h.ListCards)
		admin.POST("/cards/generate", middleware.AuthAdmin(cfg.JWT.Secret), h.GenerateCards)
		admin.DELETE("/cards/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteCard)

		// 代理池配置（所有项目公共，Redis 存储）
		admin.GET("/proxy-config", middleware.AuthAdmin(cfg.JWT.Secret), h.GetProxyConfig)
		admin.PUT("/proxy-config", middleware.AuthAdmin(cfg.JWT.Secret), h.SaveProxyConfig)

		// cookie 库（扫码登录存储的百度账号凭证）
		admin.GET("/ckdata", middleware.AuthAdmin(cfg.JWT.Secret), h.ListCkData)
		admin.POST("/ckdata/export", middleware.AuthAdmin(cfg.JWT.Secret), h.ExportCkData)

		// 第三方 API Key（按项目发放）
		admin.GET("/apikeys", middleware.AuthAdmin(cfg.JWT.Secret), h.ListApiKeys)
		admin.POST("/apikeys", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateApiKey)
		admin.PUT("/apikeys/:id/status", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateApiKeyStatus)
		admin.DELETE("/apikeys/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteApiKey)
	}

	return r
}
