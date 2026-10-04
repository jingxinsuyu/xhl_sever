package util

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token 类型
const (
	TokenTypeAdmin = "admin" // 后台管理员 token
	TokenTypeUser  = "user"  // 用户端 token
)

// Claims 自定义 JWT 载荷
type Claims struct {
	Type      string `json:"typ"`      // Token 类型：admin / user
	UserID    uint64 `json:"uid"`      // 用户/管理员 id（超级管理员为 0）
	Username  string `json:"username"` // 用户名
	Role      string `json:"role"`     // super / admin / user
	ProjectID string `json:"pid"`      // 项目 id（用户 token 按项目隔离版本）
	Ver       int64  `json:"ver"`      // token 版本（按用户+项目自增，旧 token 失效；管理员恒为 0）
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT。projectID 仅用户 token 使用（按项目隔离版本），管理员传空。
func GenerateToken(secret string, expireHours int, typ string, userID uint64, username, role, projectID string, ver int64) (string, error) {
	claims := Claims{
		Type:      typ,
		UserID:    userID,
		Username:  username,
		Role:      role,
		ProjectID: projectID,
		Ver:       ver,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "xhl-server",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析并校验 JWT
func ParseToken(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("非法的签名算法")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("token 无效")
	}
	return claims, nil
}
