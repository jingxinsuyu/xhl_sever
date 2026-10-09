package handler

import (
	"encoding/json"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// recordAudit 落一条后台操作审计（v1 完全没有留痕）。
// 失败只忽略，绝不影响业务主流程。
func (h *Handler) recordAudit(c *gin.Context, action, projectID, target string, detail any) {
	var adminID uint64
	var adminName string
	if claims := middleware.GetClaims(c); claims != nil {
		adminID = claims.UserID
		adminName = claims.Username
	}
	d := ""
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			d = string(b)
		}
	}
	_ = database.DB.Create(&model.AuditLog{
		AdminID:   adminID,
		AdminName: adminName,
		ProjectID: projectID,
		Action:    action,
		Target:    target,
		Detail:    d,
		IP:        clientIP(c),
	}).Error
}

// ListAuditLogs 操作日志【参数】project_id / action / keyword(管理员名或对象) / 分页。
//
//	GET /api/admin/audit-logs
func (h *Handler) ListAuditLogs(c *gin.Context) {
	page, pageSize := parsePage(c)
	projectID := strings.TrimSpace(c.Query("project_id"))
	action := strings.TrimSpace(c.Query("action"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	q := database.DB.Model(&model.AuditLog{})
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if action != "" {
		q = q.Where("action = ?", action)
	}
	if keyword != "" {
		q = q.Where("(admin_name LIKE ? OR target LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	var rows []model.AuditLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		list = append(list, gin.H{
			"id":         r.ID,
			"admin_id":   r.AdminID,
			"admin_name": r.AdminName,
			"project_id": r.ProjectID,
			"action":     r.Action,
			"target":     r.Target,
			"detail":     r.Detail,
			"ip":         r.IP,
			"created_at": r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}
