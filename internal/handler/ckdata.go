package handler

import (
	"fmt"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// CkDataItem 后台 cookie 库列表项（不含密码/cookie 明文，凭据经导出接口获取）
type CkDataItem struct {
	ID        uint64 `json:"id"`
	UserID    uint64 `json:"user_id"`
	Username  string `json:"username"`
	Exported  bool   `json:"exported"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListCkData 后台 cookie 库分页查询。
// 参数：keyword 用户名模糊，exported 筛选导出状态（0 未导出 / 1 已导出，缺省全部），page、page_size 分页。
func (h *Handler) ListCkData(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	exported := strings.TrimSpace(c.Query("exported"))

	query := database.DB.Model(&model.CkData{})
	if keyword != "" {
		query = query.Where("username LIKE ?", "%"+keyword+"%")
	}
	if exported == "0" {
		query = query.Where("exported = ?", false)
	} else if exported == "1" {
		query = query.Where("exported = ?", true)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var rows []model.CkData
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]CkDataItem, 0, len(rows))
	for _, r := range rows {
		list = append(list, CkDataItem{
			ID:        r.ID,
			UserID:    r.UserID,
			Username:  r.Username,
			Exported:  r.Exported,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// CkDataExportRequest 导出请求：
//   - ids 非空 → 导出勾选的这几条（未导出的）
//   - ids 为空 → 导出最早的 count 条未导出记录
type CkDataExportRequest struct {
	Count int      `json:"count"` // 按数量导出（未勾选时生效）
	IDs   []uint64 `json:"ids"`   // 勾选导出的记录 id
}

// ExportCkData 后台 cookie 库导出：返回 用户名----密码----cookie 行列表，并把导出的记录标记为已导出。
func (h *Handler) ExportCkData(c *gin.Context) {
	var req CkDataExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}
	if len(req.IDs) == 0 && req.Count <= 0 {
		util.Fail(c, util.CodeParamError, "请勾选记录或填写导出条数")
		return
	}

	query := database.DB.Model(&model.CkData{})
	if len(req.IDs) > 0 {
		// 勾选导出：按勾选 id 导出，不区分是否已导出
		query = query.Where("id IN ?", req.IDs)
	} else {
		// 按数量导出：只取最早的未导出记录
		if req.Count > 1000 {
			req.Count = 1000 // 上限保护
		}
		query = query.Where("exported = ?", false).Order("id ASC").Limit(req.Count)
	}

	var rows []model.CkData
	if err := query.Find(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "导出失败")
		return
	}
	if len(rows) == 0 {
		util.OK(c, gin.H{"count": 0, "lines": []string{}})
		return
	}

	// 标记已导出（事务）
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if err := database.DB.Model(&model.CkData{}).
		Where("id IN ?", ids).Update("exported", true).Error; err != nil {
		util.Fail(c, util.CodeDBError, "导出失败")
		return
	}

	util.OK(c, gin.H{"count": len(rows), "lines": buildExportLines(rows)})
}

// buildExportLines 把 ckdata 行拼成 用户名----密码----cookie 列表。
func buildExportLines(rows []model.CkData) []string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("%s----%s----%s", r.Username, r.Password, r.Cookie))
	}
	return lines
}
