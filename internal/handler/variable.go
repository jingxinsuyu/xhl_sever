package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// VariableRequest 项目变量增改请求
type VariableRequest struct {
	Key   string `json:"key" binding:"required"`
	Type  string `json:"type" binding:"required"` // string / int / bool / json
	Value string `json:"value"`
}

// VariableResponse 项目变量列表项
type VariableResponse struct {
	ID        uint64 `json:"id"`
	ProjectID uint64 `json:"project_id"`
	Key       string `json:"key"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// validateVariable 校验类型合法且 value 按类型可解析
func validateVariable(vtype, value string) (bool, string) {
	switch vtype {
	case model.VarTypeString:
		return true, ""
	case model.VarTypeInt:
		if _, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err != nil {
			return false, "int 类型变量的值必须是整数"
		}
		return true, ""
	case model.VarTypeBool:
		if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
			return false, "bool 类型变量的值必须是 true/false"
		}
		return true, ""
	case model.VarTypeJSON:
		if !json.Valid([]byte(value)) {
			return false, "json 类型变量的值必须是合法 JSON"
		}
		return true, ""
	default:
		return false, "type 只能为 string / int / bool / json"
	}
}

// ListVariables 项目变量（按项目）：查询列表
func (h *Handler) ListVariables(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var vars []model.ProjectVariable
	if err := database.DB.Where("project_id = ?", projectID).Order("id DESC").Find(&vars).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	list := make([]VariableResponse, 0, len(vars))
	for _, v := range vars {
		list = append(list, VariableResponse{
			ID:        v.ID,
			ProjectID: v.ProjectID,
			Key:       v.Key,
			Type:      v.Type,
			Value:     v.Value,
			CreatedAt: v.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: v.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, list)
}

// CreateVariable 项目变量（按项目）：新增
func (h *Handler) CreateVariable(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req VariableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：key、type 不能为空")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		util.Fail(c, util.CodeParamError, "参数错误：key 不能为空")
		return
	}
	if ok, msg := validateVariable(req.Type, req.Value); !ok {
		util.Fail(c, util.CodeParamError, msg)
		return
	}

	// 同一项目下 key 唯一
	var count int64
	database.DB.Model(&model.ProjectVariable{}).
		Where("project_id = ? AND `key` = ?", projectID, req.Key).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "变量 key 已存在")
		return
	}

	v := model.ProjectVariable{ProjectID: projectID, Key: req.Key, Type: req.Type, Value: req.Value}
	if err := database.DB.Create(&v).Error; err != nil {
		util.Fail(c, util.CodeDBError, "创建失败")
		return
	}
	util.OK(c, gin.H{"id": v.ID})
}

// UpdateVariable 项目变量：编辑
func (h *Handler) UpdateVariable(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var req VariableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		util.Fail(c, util.CodeParamError, "参数错误：key 不能为空")
		return
	}
	if ok, msg := validateVariable(req.Type, req.Value); !ok {
		util.Fail(c, util.CodeParamError, msg)
		return
	}

	var v model.ProjectVariable
	if err := database.DB.First(&v, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "变量不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	var count int64
	database.DB.Model(&model.ProjectVariable{}).
		Where("project_id = ? AND `key` = ? AND id <> ?", v.ProjectID, req.Key, id).Count(&count)
	if count > 0 {
		util.Fail(c, util.CodeConflict, "变量 key 已存在")
		return
	}

	if err := database.DB.Model(&v).Updates(map[string]any{
		"key":   req.Key,
		"type":  req.Type,
		"value": req.Value,
	}).Error; err != nil {
		util.Fail(c, util.CodeDBError, "修改失败")
		return
	}
	util.OK(c, nil)
}

// DeleteVariable 项目变量：删除
func (h *Handler) DeleteVariable(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var v model.ProjectVariable
	if err := database.DB.First(&v, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "变量不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := database.DB.Delete(&v).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	util.OK(c, nil)
}
