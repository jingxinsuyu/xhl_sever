package handler

import (
	"errors"

	"xhl-server/internal/crypto"
	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 默认视口宽高（与百度工具箱移动端一致）
const (
	defaultMoonshadWidth  = 750
	defaultMoonshadHeight = 1334
)

// MoonshadRequest moonshad 加密请求
type MoonshadRequest struct {
	ProjectID uint64                 `json:"project_id" binding:"required"` // 项目 id（检测用户是否有权限）
	Data      map[string]interface{} `json:"data" binding:"required"`       // 要加密的 json
	Width     int                    `json:"width"`                         // 可选，默认 750
	Height    int                    `json:"height"`                        // 可选，默认 1334
	Alg       string                 `json:"alg"`                           // v3 / v4，默认 v3
	UA        string                 `json:"ua"`                            // v4 需要 User-Agent
}

// Moonshad moonshad 接口（baidu 包内加密函数）。
// 返回 code, msg, data（data 为 moonshad 签名结果）。
func (h *Handler) Moonshad(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	var req MoonshadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：project_id、data 不能为空")
		return
	}
	if len(req.Data) == 0 {
		util.Fail(c, util.CodeParamError, "参数错误：data 不能为空")
		return
	}

	// 项目必须存在且未删除
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	// 检测用户是否有权限（该用户对项目有未过期权限）
	ent := getEntitlement(claims.UserID, req.ProjectID)
	if ent == nil || !ent.IsValid(timeNow()) {
		util.Fail(c, util.CodeNoPermission, "无该项目权限")
		return
	}

	width, height := req.Width, req.Height
	if width <= 0 {
		width = defaultMoonshadWidth
	}
	if height <= 0 {
		height = defaultMoonshadHeight
	}

	var result map[string]interface{}
	var err error
	switch req.Alg {
	case "v4":
		result, err = crypto.NewMoonshadV4(req.Data, width, height, req.UA).Get()
	default:
		result, err = crypto.NewMoonshadV3(req.Data, width, height).Get()
	}
	if err != nil {
		util.Fail(c, util.CodeDBError, "加密失败")
		return
	}
	util.OK(c, result)
}
