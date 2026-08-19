package handler

import (
	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// UserInit 初始化接口：客户端打开时校验登录是否失效、是否有项目权限。
// 需登录；token 失效返回 1002，项目不存在返回 1004，非会员/过期返回 1018。
// 成功返回最新的会员信息（到期时间 / 今日登录次数）。
func (h *Handler) UserInit(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", qrLoginProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}
	ent := getEntitlement(claims.UserID, qrLoginProjectID)
	if ent == nil || !ent.IsValid(timeNow()) {
		util.Fail(c, util.CodeMembershipExpired, "会员已过期，无法登录")
		return
	}
	util.OK(c, gin.H{
		"username":          claims.Username,
		"project_id":        qrLoginProjectID,
		"has_time":          true,
		"expires_at":        formatTimePtr(&ent.ExpiresAt),
		"today_login_count": h.todayLoginCount(qrLoginProjectID, claims.UserID),
	})
}
