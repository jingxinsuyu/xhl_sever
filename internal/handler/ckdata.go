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
	Source    string `json:"source"` // 来源：用户:用户名 / 开放平台:key名；历史数据为空
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListCkData 后台 cookie 库分页查询。
// 参数：keyword 模糊匹配用户名或来源（用户:xxx / 开放平台:xxx），exported 筛选导出状态（0 未导出 / 1 已导出，缺省全部），page、page_size 分页。
func (h *Handler) ListCkData(c *gin.Context) {
	page, pageSize := parsePage(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	exported := strings.TrimSpace(c.Query("exported"))

	query := database.DB.Model(&model.CkData{})
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(username LIKE ? OR source LIKE ?)", like, like)
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
			Source:    r.Source,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// CkDataExportRequest 导出请求：
//   - ids 非空 → 导出勾选的这几条（不筛状态/来源）
//   - ids 为空 → 筛选导出：source_type/source_value 来源筛选、exported 状态筛选、count 数量
type CkDataExportRequest struct {
	Count       int      `json:"count"`        // 导出数量（筛选导出时）
	IDs         []uint64 `json:"ids"`          // 勾选导出的记录 id
	SourceType  string   `json:"source_type"`  // 来源类型：all / open / user（筛选导出）
	SourceValue string   `json:"source_value"` // 来源值：开放平台:xxx 或 用户:xxx
	Exported    *bool    `json:"exported"`     // nil=全部；true=已导出；false=未导出
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
		// 筛选导出：按来源/状态筛选 + 数量
		if req.Count > 1000 {
			req.Count = 1000 // 上限保护
		}
		if req.SourceType != "" && req.SourceType != "all" && req.SourceValue != "" {
			query = query.Where("source = ?", req.SourceValue)
		}
		if req.Exported != nil {
			query = query.Where("exported = ?", *req.Exported)
		}
		query = query.Order("id ASC").Limit(req.Count)
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

// ListCkDataOptions 筛选导出弹窗的用户下拉数据源（分页加载）：
// 参数 type=open|user，查 ckdata 去重 source 前缀（开放平台:/用户:）；keyword 可选模糊过滤；page、page_size 分页。
func (h *Handler) ListCkDataOptions(c *gin.Context) {
	page, pageSize := parsePage(c)
	typ := strings.TrimSpace(c.Query("type"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	prefix := "用户:"
	if typ == "open" {
		prefix = "开放平台:"
	}

	query := database.DB.Model(&model.CkData{}).
		Select("DISTINCT source").
		Where("source LIKE ?", prefix+"%")
	if keyword != "" {
		query = query.Where("source LIKE ?", "%"+keyword+"%")
	}

	var sources []string
	if err := query.Order("source ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&sources).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}
	util.OK(c, gin.H{"list": sources, "has_more": len(sources) == pageSize})
}

// buildExportLines 把 ckdata 行拼成 用户名----密码----cookie 列表。
func buildExportLines(rows []model.CkData) []string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("%s----%s----%s", r.Username, r.Password, r.Cookie))
	}
	return lines
}
