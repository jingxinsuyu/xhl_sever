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

	// 后台静态资源 + SPA 回退（见 registerAdminFiles）。
	registerAdminFiles(r, cfg.StaticDir(), cfg.AdminPath())

	h := handler.New(cfg, rdb)

	// 图形验证码（无需登录）
	r.GET("/api/captcha", h.GetCaptcha)

	// 百度扫码确认（需登录 + 100001 项目会员，SSE 流式）
	r.POST("/api/xhl/qrlogin", middleware.AuthUser(cfg.JWT.Secret), h.QrLogin)

	// 滑动拼图验证码（需登录；扫码每 25 次触发一次）
	r.POST("/api/xhl/captcha", middleware.AuthUser(cfg.JWT.Secret), h.GenSlideCaptcha)
	r.POST("/api/xhl/captcha/verify", middleware.AuthUser(cfg.JWT.Secret), h.VerifySlideCaptcha)

	// 第三方开放接口（API Key 鉴权，xhlkey 请求头；明文 JSON，按 key 所属项目）
	r.POST("/api/open/qrlogin", middleware.AuthApiKey(), h.OpenQrLogin)
	// BDUSS → 网盘 cookie（仅项目 100001 的 API Key 可调用）
	r.POST("/api/open/netdisk-cookie", middleware.AuthApiKey(), h.OpenNetdiskCookie)

	// fdev 签发服务（项目 100004 的 API Key 专用）：只做「加密出包」+ rkey，
	// 不代为请求 sofire、不解密响应（客户端本地带代理发、本地解密）。
	r.POST("/api/open/fdev/issue", middleware.AuthApiKey(), h.OpenFdevIssue)
	r.POST("/api/open/fdev/rkey", middleware.AuthApiKey(), h.OpenFdevRkey)

	// 开放平台剩余积分查询（query 传 key，无需鉴权）
	r.GET("/api/open/balance", h.OpenApiKeyBalance)

	user := r.Group("/api/user")
	{
		user.POST("/init", middleware.AuthUser(cfg.JWT.Secret), h.UserInit) // 登录有效性校验
		user.POST("/register", h.Register)
		user.POST("/login", h.UserLogin)
		user.POST("/exchange", h.Exchange) // 兑换按用户名，无需登录
		user.POST("/unbind", h.UserUnbind)
		user.POST("/check-scan", middleware.AuthUser(cfg.JWT.Secret), h.CheckScan) // 扫码前账号检测

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
		admin.GET("/ckdata/options", middleware.AuthAdmin(cfg.JWT.Secret), h.ListCkDataOptions)
		admin.POST("/ckdata/export", middleware.AuthAdmin(cfg.JWT.Secret), h.ExportCkData)

		// 第三方 API Key（按项目发放）
		admin.GET("/apikeys", middleware.AuthAdmin(cfg.JWT.Secret), h.ListApiKeys)
		admin.POST("/apikeys", middleware.AuthAdmin(cfg.JWT.Secret), h.CreateApiKey)
		admin.POST("/apikeys/recharge", middleware.AuthAdmin(cfg.JWT.Secret), h.RechargeApiKey)
		admin.POST("/apikeys/adjust", middleware.AuthAdmin(cfg.JWT.Secret), h.AdjustApiKeyBalance)
		admin.PUT("/apikeys/:id/status", middleware.AuthAdmin(cfg.JWT.Secret), h.UpdateApiKeyStatus)
		admin.DELETE("/apikeys/:id", middleware.AuthAdmin(cfg.JWT.Secret), h.DeleteApiKey)
	}

	return r
}

// registerAdminFiles 注册后台前端静态资源与 SPA 回退。
// adminPath 非空时：后台只在该随机前缀下提供（如 /ef16e15c12bb/），根路径和其它路径一律 404，
// 避免后台入口被直接扫到；为空时保持旧行为（挂在根路径，便于本地调试）。
func registerAdminFiles(r *gin.Engine, webDir, adminPath string) {
	if adminPath != "" {
		// 后台前端资源（哈希文件名）与图标只在前缀下暴露
		r.Static(adminPath+"/assets", filepath.Join(webDir, "assets"))
		r.StaticFile(adminPath+"/favicon.ico", filepath.Join(webDir, "favicon.ico"))
	}
	r.NoRoute(noRouteHandler(webDir, adminPath))
}

// noRouteHandler 返回 SPA 回退处理器：非 /api、/uploads 的路径，
// 存在则返回静态文件，否则回退 index.html，保证前端 history 路由刷新不 404。
func noRouteHandler(webDir, adminPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		// 精确匹配 /api/、/uploads/ 前缀（不能只判 /api，否则 /apikeys 这类前端路由被误拦）
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/uploads/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
			return
		}
		if adminPath != "" {
			// 只有后台前缀下的路径才提供页面，其余（含根路径 /）返回 404
			if p != adminPath && !strings.HasPrefix(p, adminPath+"/") {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
				return
			}
			if p == adminPath {
				c.Redirect(http.StatusFound, adminPath+"/") // 补末尾斜杠，保证前端路由 base 正确
				return
			}
			rel := strings.TrimPrefix(p, adminPath)
			fp := filepath.Join(webDir, filepath.Clean(rel))
			if fi, err := os.Stat(fp); err == nil && !fi.IsDir() {
				c.File(fp)
				return
			}
			serveIndex(c, webDir)
			return
		}
		fp := filepath.Join(webDir, filepath.Clean(p))
		if fi, err := os.Stat(fp); err == nil && !fi.IsDir() {
			c.File(fp)
			return
		}
		serveIndex(c, webDir)
	}
}

// serveIndex 输出后台入口 index.html，并禁用缓存。
// index.html 引用了带内容哈希的资源文件名，若被浏览器启发式缓存，
// 发新版后用户仍会跑旧前端（旧 SPA 会把地址跳到 /login，看起来像"后台还能从根路径进"）。
func serveIndex(c *gin.Context, webDir string) {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.File(filepath.Join(webDir, "index.html"))
}
