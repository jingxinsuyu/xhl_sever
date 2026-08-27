package handler

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

// ExportCkData 后台 cookie 库导出（SSE 流式）：
// 分批查询 + 写 uploads/export/{task_id}.txt，边写边推进度（按文件大小），
// 写完后推 download_url，前端自动下载。
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
		// 筛选导出：按来源/状态筛选 + 数量（最新到旧）
		if req.Count > 1000000 {
			req.Count = 1000000 // 上限保护（100W）
		}
		switch req.SourceType {
		case "open":
			query = query.Where("source LIKE ?", "开放平台:%")
			if req.SourceValue != "" {
				query = query.Where("source = ?", req.SourceValue)
			}
		case "user":
			query = query.Where("source LIKE ?", "用户:%")
			if req.SourceValue != "" {
				query = query.Where("source = ?", req.SourceValue)
			}
		}
		if req.Exported != nil {
			query = query.Where("exported = ?", *req.Exported)
		}
	}
	query = query.Order("id DESC")

	// COUNT 总数
	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		util.Fail(c, util.CodeDBError, "导出失败")
		return
	}
	if totalCount == 0 {
		util.OK(c, gin.H{"count": 0, "lines": []string{}, "done": true})
		return
	}

	// 抽样估每行字节（前 500 行平均长度）
	avgLineBytes := h.estimateAvgLineBytes(query, totalCount)
	totalBytes := totalCount * avgLineBytes
	if totalBytes <= 0 {
		totalBytes = 1
	}

	// 开 SSE 流
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	writeSSE(c, map[string]interface{}{
		"status": "running", "progress": 0,
		"written_bytes": 0, "total_bytes": totalBytes,
	})

	// 生成任务 id + 文件
	taskID, err := randHex(12)
	if err != nil {
		writeSSE(c, map[string]interface{}{"status": "error", "message": "导出失败"})
		return
	}
	exportDir := filepath.Join(h.uploadRoot(), "export")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		writeSSE(c, map[string]interface{}{"status": "error", "message": "导出失败"})
		return
	}
	filePath := filepath.Join(exportDir, taskID+".txt")
	dlURL := "/uploads/export/" + taskID + ".txt"

	// 分批写文件 + 推进度（SSE 流式，handler 同步执行保持连接）
	file, err := os.Create(filePath)
	if err != nil {
		writeSSE(c, map[string]interface{}{"status": "error", "message": "导出失败"})
		return
	}
	defer file.Close()
	w := bufio.NewWriter(file)

		var written int64
		exportedIDs := []uint64{}
		const batch = 5000
		var lastID uint64 = math.MaxUint64 // 游标（id DESC 最新到旧）
		for {
			var rows []model.CkData
			q := query
			if lastID != math.MaxUint64 {
				q = q.Where("id < ?", lastID)
			}
			if err := q.Limit(batch).Find(&rows).Error; err != nil {
				writeSSE(c, map[string]interface{}{"status": "error", "message": "导出失败"})
				return
			}
			if len(rows) == 0 {
				break
			}
			for _, r := range rows {
				line := fmt.Sprintf("%s----%s----%s\n", r.Username, r.Password, r.Cookie)
				if _, err := w.WriteString(line); err != nil {
					writeSSE(c, map[string]interface{}{"status": "error", "message": "导出失败"})
					return
				}
				written += int64(len(line))
				exportedIDs = append(exportedIDs, r.ID)
			}
			w.Flush()
			lastID = rows[len(rows)-1].ID
			// 推进度
			progress := int(float64(written) / float64(totalBytes) * 100)
			if progress > 100 {
				progress = 100
			}
			writeSSE(c, map[string]interface{}{
				"status": "running", "progress": progress,
				"written_bytes": written, "total_bytes": totalBytes,
			})
		}
		w.Flush()

		// 标记本次导出的行为已导出
		if len(exportedIDs) > 0 {
			database.DB.Model(&model.CkData{}).
				Where("id IN ?", exportedIDs).Update("exported", true)
		}

	// 推 done + 下载链接
	writeSSE(c, map[string]interface{}{
		"status": "done", "progress": 100,
		"written_bytes": written, "total_bytes": written,
		"download_url": dlURL, "count": totalCount,
	})
}

// estimateAvgLineBytes 抽样前 500 行估算每行字节数（含分隔符/换行）。
func (h *Handler) estimateAvgLineBytes(query *gorm.DB, total int64) int64 {
	const sample = 500
	var rows []model.CkData
	if err := query.Limit(sample).Find(&rows).Error; err != nil || len(rows) == 0 {
		return 300 // 兜底
	}
	var totalLen int64
	for _, r := range rows {
		totalLen += int64(len(r.Username) + len(r.Password) + len(r.Cookie) + 7) // + "----" + "\n"
	}
	avg := totalLen / int64(len(rows))
	if avg < 50 {
		avg = 50
	}
	return avg
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
