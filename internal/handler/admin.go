package handler

import (
	"errors"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateAdminRequest 创建管理员请求
type CreateAdminRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
}

// UpdateAdminRequest 修改管理员请求（password 可选，非空才改）
type UpdateAdminRequest struct {
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
}

// UpdateAdminStatusRequest 冻结/解冻管理员
type UpdateAdminStatusRequest struct {
	Status int8 `json:"status" binding:"oneof=0 1"` // 1 启用 / 0 禁用
}

// AdminListResponse 管理员列表项
type AdminListResponse struct {
	ID        uint64 `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Remark    string `json:"remark"`
	Status    int8   `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListAdmins 查询管理员【参数】keyword 模糊搜索（用户名/昵称），page、page_size 分页
func (h *Handler) ListAdmins(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := database.DB.Model(&model.Admin{})
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(username LIKE ? OR nickname LIKE ?)", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var admins []model.Admin
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&admins).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]AdminListResponse, 0, len(admins))
	for _, a := range admins {
		list = append(list, AdminListResponse{
			ID:        a.ID,
			Username:  a.Username,
			Nickname:  a.Nickname,
			Remark:    a.Remark,
			Status:    a.Status,
			CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: a.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// CreateAdmin 创建管理员【仅超级管理员】参数 用户名 密码 昵称 备注
func (h *Handler) CreateAdmin(c *gin.Context) {
	var req CreateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：username、password 不能为空")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		util.Fail(c, util.CodeParamError, "参数错误：username、password 不能为空")
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 32 {
		util.Fail(c, util.CodeParamError, "密码长度需在 6-32 之间")
		return
	}
	// 用户名不能与超级管理员冲突
	if req.Username == h.Config.SuperAdmin.Username {
		util.Fail(c, util.CodeConflict, "用户名与超级管理员冲突")
		return
	}

	var count int64
	database.DB.Model(&model.Admin{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "用户名已存在")
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	admin := model.Admin{Username: req.Username, Password: hash, Nickname: req.Nickname, Remark: req.Remark, Status: 1}
	if err := database.DB.Create(&admin).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	util.OK(c, gin.H{"id": admin.ID})
}

// UpdateAdmin 修改管理员【仅超级管理员】参数 密码(可选) 昵称 备注
func (h *Handler) UpdateAdmin(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	req.Password = strings.TrimSpace(req.Password)

	var admin model.Admin
	if err := database.DB.First(&admin, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "管理员不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	// 不允许修改超级管理员
	if admin.Username == h.Config.SuperAdmin.Username {
		util.Fail(c, util.CodeForbidden, "不能修改超级管理员")
		return
	}

	updates := map[string]any{}
	if req.Password != "" {
		if len(req.Password) < 6 || len(req.Password) > 32 {
			util.Fail(c, util.CodeParamError, "密码长度需在 6-32 之间")
			return
		}
		hash, err := util.HashPassword(req.Password)
		if err != nil {
			util.Fail(c, util.CodeDBError, "系统错误")
			return
		}
		updates["password"] = hash
	}
	updates["nickname"] = req.Nickname
	updates["remark"] = req.Remark

	if err := database.DB.Model(&admin).Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "修改失败")
		return
	}
	util.OK(c, nil)
}

// UpdateAdminStatus 冻结/解冻管理员【仅超级管理员】status: 1 启用 / 0 禁用
func (h *Handler) UpdateAdminStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateAdminStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：status 不能为空")
		return
	}

	claims := middleware.GetClaims(c)
	var admin model.Admin
	if err := database.DB.First(&admin, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "管理员不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if admin.Username == h.Config.SuperAdmin.Username {
		util.Fail(c, util.CodeForbidden, "不能冻结超级管理员")
		return
	}
	if claims != nil && claims.UserID == id {
		util.Fail(c, util.CodeForbidden, "不能冻结当前登录的管理员")
		return
	}

	if err := database.DB.Model(&admin).Update("status", req.Status).Error; err != nil {
		util.Fail(c, util.CodeDBError, "操作失败")
		return
	}
	util.OK(c, nil)
}
