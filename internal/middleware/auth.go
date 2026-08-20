package middleware

import (
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// ContextClaims 上下文中的 Claims 键
const ContextClaims = "claims"

// ContextApiKey 上下文中的 ApiKey 键
const ContextApiKey = "apikey"

// AuthAdmin 管理员鉴权：校验 Bearer token，必须是 admin 类型
func AuthAdmin(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseAndValidate(c, jwtSecret, util.TokenTypeAdmin)
		if !ok {
			return
		}
		// 普通管理员被禁用时禁止操作
		if claims.Role == model.RoleAdmin && !isAdminEnabled(c, claims.Username) {
			util.Fail(c, util.CodeAccountLocked, "管理员已被禁用")
			c.Abort()
			return
		}
		c.Set(ContextClaims, claims)
		c.Next()
	}
}

// AuthSuper 超级管理员鉴权：必须是超级管理员（由 yaml 配置）
func AuthSuper(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseAndValidate(c, jwtSecret, util.TokenTypeAdmin)
		if !ok {
			return
		}
		if claims.Role != model.RoleSuper {
			util.Fail(c, util.CodeForbidden, "仅超级管理员可操作")
			c.Abort()
			return
		}
		c.Set(ContextClaims, claims)
		c.Next()
	}
}

// AuthApiKey 第三方开放接口鉴权：校验请求头 xhlkey（API Key，按项目绑定）。
// 命中启用状态的 key 后，把 *model.ApiKey（含其 ProjectID）放入 context，供接口以该项目为上下文操作。
func AuthApiKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader("xhlkey"))
		if key == "" {
			util.Fail(c, util.CodeUnauthorized, "缺少 xhlkey 请求头")
			c.Abort()
			return
		}
		var ak model.ApiKey
		if err := database.DB.Where("`key` = ? AND status = 1", key).First(&ak).Error; err != nil {
			util.Fail(c, util.CodeUnauthorized, "xhlkey 无效")
			c.Abort()
			return
		}
		c.Set(ContextApiKey, &ak)
		c.Next()
	}
}

// GetApiKey 从上下文获取 API Key。
func GetApiKey(c *gin.Context) *model.ApiKey {
	if v, ok := c.Get(ContextApiKey); ok {
		if ak, ok := v.(*model.ApiKey); ok {
			return ak
		}
	}
	return nil
}

// AuthUser 用户端鉴权：校验 Bearer token 类型 + token 版本（登录后旧 token 失效）
func AuthUser(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseAndValidate(c, jwtSecret, util.TokenTypeUser)
		if !ok {
			return
		}
		if claims.Role != model.RoleUser {
			util.Fail(c, util.CodeUnauthorized, "用户身份校验失败")
			c.Abort()
			return
		}
		// 版本校验：登录会自增 token_version，旧 token 版本不匹配即失效
		var user model.User
		if err := database.DB.Select("id", "token_version", "status").First(&user, claims.UserID).Error; err != nil {
			util.Fail(c, util.CodeUnauthorized, "token 无效")
			c.Abort()
			return
		}
		if user.TokenVersion != claims.Ver {
			util.Fail(c, util.CodeUnauthorized, "token 已失效，请重新登录")
			c.Abort()
			return
		}
		c.Set(ContextClaims, claims)
		c.Next()
	}
}

func parseAndValidate(c *gin.Context, jwtSecret, expectType string) (*util.Claims, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		c.Abort()
		return nil, false
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		util.Fail(c, util.CodeUnauthorized, "Authorization 格式错误，应为 Bearer <token>")
		c.Abort()
		return nil, false
	}
	claims, err := util.ParseToken(jwtSecret, strings.TrimSpace(parts[1]))
	if err != nil {
		util.Fail(c, util.CodeUnauthorized, "token 无效或已过期")
		c.Abort()
		return nil, false
	}
	if claims.Type != expectType {
		util.Fail(c, util.CodeUnauthorized, "token 类型不匹配")
		c.Abort()
		return nil, false
	}
	return claims, true
}

// isAdminEnabled 校验普通管理员是否被禁用（超级管理员不受影响）
func isAdminEnabled(c *gin.Context, username string) bool {
	var admin model.Admin
	if err := database.DB.Where("username = ?", username).First(&admin).Error; err != nil {
		return false
	}
	return admin.Status == 1
}

// GetClaims 从上下文获取 Claims
func GetClaims(c *gin.Context) *util.Claims {
	if v, ok := c.Get(ContextClaims); ok {
		if claims, ok := v.(*util.Claims); ok {
			return claims
		}
	}
	return nil
}
