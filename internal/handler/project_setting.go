package handler

import (
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// projectSettingView 统一的项目配置返回结构。
func projectSettingView(s *model.ProjectSetting) gin.H {
	return gin.H{
		"project_id":   s.ProjectID,
		"billing_mode": s.BillingModeOrDefault(),
		"price":        s.Price,
		"login_limit":  s.LoginLimit,
		"unbind_limit": s.UnbindLimit,
		"call_limit":   s.CallLimit,
		"proxy_mode":   s.ProxyModeOrDefault(),
		"proxy_url":    s.ProxyURL,
		"features":     s.Features,
		"updated_at":   s.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func validBillingMode(v string) bool {
	switch v {
	case "", model.BillingMembership, model.BillingPerCall, model.BillingCredits:
		return true
	}
	return false
}

func validProxyMode(v string) bool {
	switch v {
	case "", model.ProxyInherit, model.ProxyNone, model.ProxyFixed, model.ProxyPool:
		return true
	}
	return false
}

// GetProjectSetting 读取项目配置（不存在则惰性建默认行，行为等同升级前）。
//
//	GET /api/admin/projects/:id/setting
func (h *Handler) GetProjectSetting(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	s, err := projsetting.Get(id)
	if err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}
	util.OK(c, projectSettingView(s))
}

// SaveProjectSetting 保存项目配置：计费模式/单价、三个限额、代理、功能开关。
// 三个 limit 会同时双写回 project 表，老接口读到的一致。
//
//	PUT /api/admin/projects/:id/setting
func (h *Handler) SaveProjectSetting(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var req model.ProjectSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	if !validBillingMode(req.BillingMode) {
		util.Fail(c, util.CodeParamError, "计费模式只能是 membership / per_call / credits")
		return
	}
	if !validProxyMode(req.ProxyMode) {
		util.Fail(c, util.CodeParamError, "代理模式只能是 inherit / none / fixed / pool")
		return
	}
	// 计费相关校验
	mode := req.BillingModeOrDefault()
	if mode == model.BillingPerCall || mode == model.BillingCredits {
		if req.Price <= 0 {
			util.Fail(c, util.CodeParamError, "按次/积分计费时 price 必须大于 0")
			return
		}
	}
	// 代理相关校验
	pm := req.ProxyModeOrDefault()
	req.ProxyURL = strings.TrimSpace(req.ProxyURL)
	if (pm == model.ProxyFixed || pm == model.ProxyPool) && req.ProxyURL == "" {
		util.Fail(c, util.CodeParamError, "代理模式为 fixed/pool 时 proxy_url 不能为空")
		return
	}

	before, _ := projsetting.Get(id)
	after, err := projsetting.Update(id, &req)
	if err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}

	h.recordAudit(c, model.AuditProjectSetting, id, "project:"+id, gin.H{
		"before": projectSettingView(before),
		"after":  projectSettingView(after),
	})
	util.OK(c, projectSettingView(after))
}

// NextProjectID 返回下一个可用的 6 位项目 id（含已被软删占用的一起避开）。
//
//	GET /api/admin/project-next-id
func (h *Handler) NextProjectID(c *gin.Context) {
	var ids []string
	if err := database.DB.Model(&model.Project{}).Pluck("id", &ids).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	used := make(map[string]struct{}, len(ids))
	for _, v := range ids {
		used[v] = struct{}{}
	}
	const start = 100001
	const end = 999999
	for i := start; i <= end; i++ {
		cand := strconv.Itoa(i)
		if _, ok := used[cand]; !ok {
			util.OK(c, gin.H{"id": cand})
			return
		}
	}
	util.Fail(c, util.CodeDBError, "没有可用的项目 id")
}

// CheckProjectID 校验项目 id 是否可用（6 位数字 + 未被占用，含软删占用）。
//
//	GET /api/admin/project-check-id?id=100005
func (h *Handler) CheckProjectID(c *gin.Context) {
	id := strings.TrimSpace(c.Query("id"))
	if !isProjectID(id) {
		util.OK(c, gin.H{"id": id, "available": false, "reason": "项目 id 必须是 6 位数字"})
		return
	}
	var cnt int64
	if err := database.DB.Model(&model.Project{}).Where("id = ?", id).Count(&cnt).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	// 名字是否已被别的未删项目占用（提示用）
	var nameCnt int64
	_ = database.DB.Model(&model.Project{}).
		Where("id <> ? AND deleted_at IS NULL", id).Count(&nameCnt).Error
	if cnt > 0 {
		util.OK(c, gin.H{"id": id, "available": false, "reason": "该 id 已被占用（含已删除项目）"})
		return
	}
	util.OK(c, gin.H{"id": id, "available": true, "reason": ""})
}

// RestoreProject 恢复被软删除的项目（v1 只能删不能恢复）。
//
//	POST /api/admin/projects/:id/restore
func (h *Handler) RestoreProject(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var p model.Project
	if err := database.DB.Where("id = ?", id).Limit(1).Find(&p).Error; err != nil || p.ID == "" {
		util.Fail(c, util.CodeNotFound, "项目不存在")
		return
	}
	if p.DeletedAt == nil {
		util.Fail(c, util.CodeConflict, "该项目未被删除")
		return
	}
	if err := database.DB.Model(&model.Project{}).Where("id = ?", id).
		Update("deleted_at", nil).Error; err != nil {
		util.Fail(c, util.CodeDBError, "恢复失败")
		return
	}
	// 确保配置行存在
	_, _ = projsetting.Get(id)
	h.recordAudit(c, model.AuditProjectRestore, id, "project:"+id, nil)
	util.OK(c, nil)
}
