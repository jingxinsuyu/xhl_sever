package handler

import (
	"errors"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateProjectRequest 添加项目请求
type CreateProjectRequest struct {
	ID          string `json:"id" binding:"required"` // 6 位数字，用户自定义
	Name        string `json:"name" binding:"required"`
	Remark      string `json:"remark"`
	LoginLimit  int    `json:"login_limit"`  // 0 不限制；N 该用户今日只能登录 N 次
	UnbindLimit int    `json:"unbind_limit"` // 0 不限制；N 该用户今日只能自助解绑 N 次
	CallLimit   int    `json:"call_limit"`   // 0 不限制；N 该用户今日只能调用 N 次
}

// UpdateProjectRequest 编辑项目请求
type UpdateProjectRequest struct {
	Name        string `json:"name"`
	Remark      string `json:"remark"`
	LoginLimit  int    `json:"login_limit"`
	UnbindLimit int    `json:"unbind_limit"`
	CallLimit   int    `json:"call_limit"`
}

// ProjectResponse 项目列表项
type ProjectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Remark      string  `json:"remark"`
	LoginLimit  int     `json:"login_limit"`
	UnbindLimit int     `json:"unbind_limit"`
	CallLimit   int     `json:"call_limit"`
	CreatedBy   string  `json:"created_by"`
	DeletedAt   *string `json:"deleted_at"` // v2：软删时间（前台"显示已删除/恢复"用）
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// projectResponse 项目 → 返回结构（列表与详情共用）。
func projectResponse(p model.Project) ProjectResponse {
	return ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Remark:      p.Remark,
		LoginLimit:  p.LoginLimit,
		UnbindLimit: p.UnbindLimit,
		CallLimit:   p.CallLimit,
		CreatedBy:   p.CreatedBy,
		DeletedAt:   formatTimePtr(p.DeletedAt),
		CreatedAt:   p.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   p.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// GetProject 读取单个项目（项目工作台用）。
//
//	GET /api/admin/projects/:id
func (h *Handler) GetProject(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var p model.Project
	if err := database.DB.Where("id = ?", id).Limit(1).Find(&p).Error; err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if p.ID == "" {
		util.Fail(c, util.CodeNotFound, "项目不存在")
		return
	}
	util.OK(c, projectResponse(p))
}

// ListProjects 查询项目【参数】keyword 模糊搜索，include_deleted=1 时含已删除，page、page_size 分页
func (h *Handler) ListProjects(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	includeDeleted := strings.TrimSpace(c.Query("include_deleted")) == "1"

	query := database.DB.Model(&model.Project{})
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if keyword != "" {
		query = query.Where("(name LIKE ? OR remark LIKE ? OR id LIKE ?)", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var projects []model.Project
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&projects).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]ProjectResponse, 0, len(projects))
	for _, p := range projects {
		list = append(list, projectResponse(p))
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// CreateProject 添加项目【参数】id(6位数字) 项目名 备注 限制登录次数
func (h *Handler) CreateProject(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：id、name 不能为空")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if !isProjectID(req.ID) {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 必须为 6 位数字")
		return
	}
	if req.Name == "" {
		util.Fail(c, util.CodeParamError, "参数错误：项目名不能为空")
		return
	}
	if req.LoginLimit < 0 || req.UnbindLimit < 0 || req.CallLimit < 0 {
		util.Fail(c, util.CodeParamError, "login_limit / unbind_limit / call_limit 不能为负数")
		return
	}

	// v2：软删项目仍占用主键，必须连"已删除"一起查重，否则重建同 id 会撞主键报 1005
	var idCount int64
	database.DB.Model(&model.Project{}).Where("id = ?", req.ID).Count(&idCount)
	if idCount > 0 {
		util.Fail(c, util.CodeConflict, "项目 id 已被占用（可能存在于已删除项目，可用 /api/admin/project-check-id 查询）")
		return
	}
	var count int64
	database.DB.Model(&model.Project{}).Where("name = ? AND deleted_at IS NULL", req.Name).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "项目名已存在")
		return
	}

	// v2：记录创建人（审计）
	createdBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		createdBy = claims.Username
	}
	p := model.Project{
		ID: req.ID, Name: req.Name, Remark: req.Remark,
		LoginLimit: req.LoginLimit, UnbindLimit: req.UnbindLimit, CallLimit: req.CallLimit,
		CreatedBy: createdBy,
	}
	if err := database.DB.Create(&p).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	// v2：立即建好配置行（计费/代理默认值），避免后续读到"缺配置"
	_, _ = projsetting.Get(p.ID)
	h.recordAudit(c, model.AuditProjectCreate, p.ID, "project:"+p.ID, gin.H{
		"name": p.Name, "remark": p.Remark,
		"login_limit": p.LoginLimit, "unbind_limit": p.UnbindLimit, "call_limit": p.CallLimit,
	})
	util.OK(c, gin.H{"id": p.ID})
}

// UpdateProject 编辑项目【参数】项目名 备注 限制登录次数
func (h *Handler) UpdateProject(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		util.Fail(c, util.CodeParamError, "参数错误：项目名不能为空")
		return
	}
	if req.LoginLimit < 0 || req.UnbindLimit < 0 || req.CallLimit < 0 {
		util.Fail(c, util.CodeParamError, "login_limit / unbind_limit / call_limit 不能为负数")
		return
	}

	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", id).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	var count int64
	database.DB.Model(&model.Project{}).
		Where("name = ? AND deleted_at IS NULL AND id <> ?", req.Name, id).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "项目名已存在")
		return
	}

	if err := database.DB.Model(&project).Updates(map[string]any{
		"name":         req.Name,
		"remark":       req.Remark,
		"login_limit":  req.LoginLimit,
		"unbind_limit": req.UnbindLimit,
		"call_limit":   req.CallLimit,
	}).Error; err != nil {
		util.Fail(c, util.CodeDBError, "修改失败")
		return
	}
	// 三个限额同时同步到 project_setting，保证新配置页看到的一致（其它字段保持原值）
	if cur, err := projsetting.Get(id); err == nil && cur != nil {
		cur.LoginLimit = req.LoginLimit
		cur.UnbindLimit = req.UnbindLimit
		cur.CallLimit = req.CallLimit
		_, _ = projsetting.Update(id, cur)
	}
	h.recordAudit(c, model.AuditProjectUpdate, id, "project:"+id, gin.H{
		"name": req.Name, "remark": req.Remark,
		"login_limit": req.LoginLimit, "unbind_limit": req.UnbindLimit, "call_limit": req.CallLimit,
	})
	util.OK(c, nil)
}

// DeleteProject 删除项目（伪删除，前端不显示即可）
func (h *Handler) DeleteProject(c *gin.Context) {
	id, ok := parseProjectID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：项目 id 不合法")
		return
	}
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", id).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	now := timeNow()
	if err := database.DB.Model(&project).Update("deleted_at", &now).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	// 统计受影响资源，便于后台二次确认；数据本身不级联删除（与 v1 一致）
	var cards, keys, vers int64
	database.DB.Model(&model.Card{}).Where("project_id = ?", id).Count(&cards)
	database.DB.Model(&model.ApiKey{}).Where("project_id = ?", id).Count(&keys)
	database.DB.Model(&model.Version{}).Where("project_id = ?", id).Count(&vers)
	h.recordAudit(c, model.AuditProjectDelete, id, "project:"+id, gin.H{
		"name": project.Name, "cards": cards, "apikeys": keys, "versions": vers,
	})
	util.OK(c, gin.H{"deleted": true, "cards": cards, "apikeys": keys, "versions": vers})
}
