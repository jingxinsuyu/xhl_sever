package util

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 业务错误码
const (
	CodeOK               = 0    // 成功
	CodeParamError       = 1001 // 参数错误
	CodeUnauthorized     = 1002 // 未登录 / token 无效
	CodeForbidden        = 1003 // 无权限
	CodeNotFound         = 1004 // 资源不存在
	CodeDBError          = 1005 // 数据库错误
	CodeConflict         = 1006 // 冲突（重复用户名等）
	CodeInvalidCred      = 1007 // 用户名或密码错误
	CodeAccountLocked    = 1008 // 账号被冻结/禁用
	CodeCardUsed         = 1009 // 卡密已使用
	CodeCardInvalid      = 1010 // 卡密无效
	CodeCardTypeDeleted  = 1011 // 卡密类型已删除
	CodeCaptchaError     = 1012 // 图形验证码错误或已过期
	CodeRateLimited      = 1013 // 登录过于频繁（限流）
	CodeDeviceNotBound   = 1014 // 设备未绑定（换设备需先解绑）
	CodeLoginLimitExceed = 1015 // 今日登录次数已达上限
	CodeNoPermission     = 1016 // 无该项目权限
	CodeUnbindLimitExceed = 1017 // 今日自助解绑次数已达上限
	CodeMembershipExpired = 1018 // 会员已过期
)

// Response 统一响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK 成功响应
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeOK, Message: "ok", Data: data})
}

// Fail 失败响应（默认 HTTP 200，业务码区分）
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, Response{Code: code, Message: msg})
}

// Page 分页数据结构
type Page struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// NewPage 构造分页数据
func NewPage(list any, total int64, page, pageSize int) Page {
	return Page{List: list, Total: total, Page: page, PageSize: pageSize}
}
