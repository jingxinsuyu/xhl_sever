package handler

import (
	"errors"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
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
}

// UpdateProjectRequest 编辑项目请求
type UpdateProjectRequest struct {
	Name        string `json:"name"`
	Remark      string `json:"remark"`
	LoginLimit  int    `json:"login_limit"`
	UnbindLimit int    `json:"unbind_limit"`
}

// ProjectResponse 项目列表项
type ProjectResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Remark      string `json:"remark"`
	LoginLimit  int    `json:"login_limit"`
	UnbindLimit int    `json:"unbind_limit"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListProjects 查询项目【参数】keyword 模糊搜索，page、page_size 分页（不含已删除）
func (h *Handler) ListProjects(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := database.DB.Model(&model.Project{}).Where("deleted_at IS NULL")
	if keyword != "" {
		query = query.Where("(name LIKE ? OR remark LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
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
		list = append(list, ProjectResponse{
			ID:          p.ID,
			Name:        p.Name,
			Remark:      p.Remark,
			LoginLimit:  p.LoginLimit,
			UnbindLimit: p.UnbindLimit,
			CreatedAt:   p.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:   p.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
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
	if req.LoginLimit < 0 || req.UnbindLimit < 0 {
		util.Fail(c, util.CodeParamError, "login_limit / unbind_limit 不能为负数")
		return
	}

	var count int64
	database.DB.Model(&model.Project{}).Where("(name = ? OR id = ?) AND deleted_at IS NULL", req.Name, req.ID).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "项目名或项目 id 已存在")
		return
	}

	p := model.Project{ID: req.ID, Name: req.Name, Remark: req.Remark, LoginLimit: req.LoginLimit, UnbindLimit: req.UnbindLimit}
	if err := database.DB.Create(&p).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
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
	if req.LoginLimit < 0 || req.UnbindLimit < 0 {
		util.Fail(c, util.CodeParamError, "login_limit / unbind_limit 不能为负数")
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
	}).Error; err != nil {
		util.Fail(c, util.CodeDBError, "修改失败")
		return
	}
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
	util.OK(c, nil)
}
