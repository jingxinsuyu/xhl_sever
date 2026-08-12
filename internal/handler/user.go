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

// UpdateUserPasswordRequest 修改用户密码请求
// password=新登录密码，ex_password=新超级密码；哪个不为空改哪个（至少一个）
type UpdateUserPasswordRequest struct {
	Password   string `json:"password"`
	ExPassword string `json:"ex_password"`
}

// UpdateUserStatusRequest 冻结/解冻用户请求
type UpdateUserStatusRequest struct {
	Status int8 `json:"status" binding:"oneof=0 1"` // 1 启用 / 0 冻结
}

// UnbindUserRequest 管理员解绑用户请求（project_id 为空则解绑全部项目）
type UnbindUserRequest struct {
	ProjectID uint64 `json:"project_id"`
}

// UserListResponse 用户列表项
type UserListResponse struct {
	ID        uint64 `json:"id"`
	Username  string `json:"username"`
	Status    int8   `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListUsers 查询用户【参数】keyword 模糊搜索，page、page_size 分页
func (h *Handler) ListUsers(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))

	query := database.DB.Model(&model.User{})
	if keyword != "" {
		query = query.Where("username LIKE ?", "%"+keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var users []model.User
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]UserListResponse, 0, len(users))
	for _, u := range users {
		list = append(list, UserListResponse{
			ID:        u.ID,
			Username:  u.Username,
			Status:    u.Status,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: u.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// UpdateUserPassword 修改用户密码/超级密码【参数】password  ex_password 不为空就修改
func (h *Handler) UpdateUserPassword(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	req.Password = strings.TrimSpace(req.Password)
	req.ExPassword = strings.TrimSpace(req.ExPassword)
	if req.Password == "" && req.ExPassword == "" {
		util.Fail(c, util.CodeParamError, "password（登录密码）和 ex_password（超级密码）至少填一个")
		return
	}

	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	updates := map[string]any{}
	if req.Password != "" {
		if len(req.Password) < 6 || len(req.Password) > 12 {
			util.Fail(c, util.CodeParamError, "登录密码长度需在 6-12 之间")
			return
		}
		hash, err := util.HashPassword(req.Password)
		if err != nil {
			util.Fail(c, util.CodeDBError, "系统错误")
			return
		}
		updates["password"] = hash
	}
	if req.ExPassword != "" {
		if len(req.ExPassword) < 6 || len(req.ExPassword) > 12 {
			util.Fail(c, util.CodeParamError, "超级密码长度需在 6-12 之间")
			return
		}
		hash, err := util.HashPassword(req.ExPassword)
		if err != nil {
			util.Fail(c, util.CodeDBError, "系统错误")
			return
		}
		updates["super_password"] = hash
	}

	if err := database.DB.Model(&user).Updates(updates).Error; err != nil {
		util.Fail(c, util.CodeDBError, "修改失败")
		return
	}
	util.OK(c, nil)
}

// UpdateUserStatus 冻结/解冻用户【status: 1 启用 / 0 冻结】
func (h *Handler) UpdateUserStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：status 不能为空")
		return
	}
	if req.Status != model.UserStatusNormal && req.Status != model.UserStatusFrozen {
		util.Fail(c, util.CodeParamError, "status 只能为 0（冻结）或 1（启用）")
		return
	}

	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Model(&user).Update("status", req.Status).Error; err != nil {
		util.Fail(c, util.CodeDBError, "操作失败")
		return
	}
	util.OK(c, nil)
}

// UserBindingInfo 用户在某项目的绑定信息
type UserBindingInfo struct {
	MachineCode string `json:"machine_code"`
	BoundAt     string `json:"bound_at"`
}

// UserMembershipProject 用户在某项目的会员情况
type UserMembershipProject struct {
	ProjectID       uint64            `json:"project_id"`
	ProjectName     string            `json:"project_name"`
	ExpiresAt       *string           `json:"expires_at"`        // 会员到期时间，无记录为 null
	HasTime         bool              `json:"has_time"`
	Bindings        []UserBindingInfo `json:"bindings"`          // 绑定情况
	TodayLoginCount int64             `json:"today_login_count"` // 今日已登录次数
}

// GetUserMembership 用户会员情况：该用户在所有项目的会员到期时间 + 绑定情况。
// 解绑操作由 POST /admin/users/:id/unbind（按 project_id）完成。
func (h *Handler) GetUserMembership(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	// 所有未删除项目
	var projects []model.Project
	if err := database.DB.Where("deleted_at IS NULL").Order("id ASC").Find(&projects).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	// 用户在所有项目的会员到期时间
	var ents []model.UserEntitlement
	database.DB.Where("user_id = ?", user.ID).Find(&ents)
	entMap := make(map[uint64]model.UserEntitlement, len(ents))
	for _, e := range ents {
		entMap[e.ProjectID] = e
	}

	// 用户在所有项目的绑定情况
	var bindings []model.UserBinding
	database.DB.Where("user_id = ?", user.ID).Find(&bindings)
	bindingMap := make(map[uint64][]model.UserBinding)
	for _, b := range bindings {
		bindingMap[b.ProjectID] = append(bindingMap[b.ProjectID], b)
	}

	now := timeNow()
	projectResp := make([]UserMembershipProject, 0, len(projects))
	for _, p := range projects {
		item := UserMembershipProject{
			ProjectID:       p.ID,
			ProjectName:     p.Name,
			Bindings:        make([]UserBindingInfo, 0),
			TodayLoginCount: h.todayLoginCount(p.ID, user.ID),
		}
		if e, ok := entMap[p.ID]; ok {
			s := e.ExpiresAt.Format("2006-01-02 15:04:05")
			item.ExpiresAt = &s
			item.HasTime = e.ExpiresAt.After(now)
		}
		for _, b := range bindingMap[p.ID] {
			item.Bindings = append(item.Bindings, UserBindingInfo{
				MachineCode: b.MachineCode,
				BoundAt:     b.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
		projectResp = append(projectResp, item)
	}

	util.OK(c, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"projects": projectResp,
	})
}

// ClearUserLoginCountRequest 清零登录次数请求
type ClearUserLoginCountRequest struct {
	ProjectID uint64 `json:"project_id"` // 0 表示清零该用户全部项目
}

// ClearUserLoginCount 清零用户今日登录次数【管理员】。
func (h *Handler) ClearUserLoginCount(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req ClearUserLoginCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := h.clearDailyLogin(req.ProjectID, user.ID); err != nil {
		util.Fail(c, util.CodeDBError, "清零失败")
		return
	}
	util.OK(c, nil)
}

// UnbindUser 解绑用户【管理员可无限制帮助用户解绑】。
// 因为登录会绑定机器码，解绑后用户可在新设备登录并重新绑定。
func (h *Handler) UnbindUser(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req UnbindUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}

	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "用户不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	q := database.DB.Where("user_id = ?", user.ID)
	if req.ProjectID > 0 {
		q = q.Where("project_id = ?", req.ProjectID)
	}
	if err := q.Delete(&model.UserBinding{}).Error; err != nil {
		util.Fail(c, util.CodeDBError, "解绑失败")
		return
	}
	util.OK(c, gin.H{"unbound": true})
}
