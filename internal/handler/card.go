package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GenerateCardsRequest 生成卡密请求
type GenerateCardsRequest struct {
	TypeID uint64 `json:"type_id" binding:"required"` // 卡密类型 id（按类型生成）
	Count  int    `json:"count" binding:"required"`   // 生成数量
}

// GenerateCards 批量生成卡密（16 位 hash，同 ddcx-admin-vue 一致）
func (h *Handler) GenerateCards(c *gin.Context) {
	var req GenerateCardsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：type_id、count 不能为空")
		return
	}
	if req.Count < 1 || req.Count > 10000 {
		util.Fail(c, util.CodeParamError, "count 需在 1-10000 之间")
		return
	}

	// 校验类型存在且未删除，取所属项目
	var ct model.CardType
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.TypeID).First(&ct).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "卡密类型不存在或已删除")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	keys, err := util.GenerateCDKeys(req.Count)
	if err != nil {
		util.Fail(c, util.CodeDBError, "生成卡密失败")
		return
	}

	cards := make([]model.Card, 0, len(keys))
	for _, k := range keys {
		cards = append(cards, model.Card{ProjectID: ct.ProjectID, TypeID: req.TypeID, CDKey: k})
	}
	if err := database.DB.CreateInBatches(cards, 500).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存卡密失败")
		return
	}

	util.OK(c, gin.H{"count": len(keys), "project_id": ct.ProjectID, "type_id": req.TypeID, "cdkeys": keys})
}

// cardRow 卡密查询行（联表 card + card_type + user）
type cardRow struct {
	ID        uint64
	CDKey     string `gorm:"column:cdkey"`
	TypeID    uint64
	UserID    *uint64
	Username  string // 使用人用户名（反查 user 表）
	UsedAt    *time.Time
	CreatedAt time.Time
	TypeName  string
	Days      int
}

// CardListItem 卡密列表项
type CardListItem struct {
	ID        uint64  `json:"id"`
	CDKey     string  `json:"cdkey"`
	TypeID    uint64  `json:"type_id"`
	TypeName  string  `json:"type_name"`
	Days      int     `json:"days"`
	Status    int     `json:"status"` // 0 未使用 / 1 已使用
	UserID    *uint64 `json:"user_id"`
	Username  string  `json:"username"` // 使用人用户名（反查）
	UsedAt    *string `json:"used_at"`
	CreatedAt string  `json:"created_at"`
}

// ListCards 查询卡密【参数】project_id(必填) type_id keyword(卡密/使用人模糊) status(0/1) start_time end_time
func (h *Handler) ListCards(c *gin.Context) {
	page, pageSize := parsePage(c)
	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		util.Fail(c, util.CodeParamError, "参数错误：project_id 不能为空")
		return
	}
	typeID, _ := strconv.ParseUint(c.Query("type_id"), 10, 64)
	keyword := strings.TrimSpace(c.Query("keyword"))
	statusStr := strings.TrimSpace(c.Query("status"))

	query := database.DB.Table("card AS cd").
		Select("cd.id, cd.cdkey, cd.type_id, cd.user_id, cd.used_at, cd.created_at, ct.name AS type_name, ct.days, u.username AS username").
		Joins("LEFT JOIN card_type AS ct ON ct.id = cd.type_id").
		Joins("LEFT JOIN user AS u ON u.id = cd.user_id").
		Where("cd.project_id = ?", projectID)

	if typeID > 0 {
		query = query.Where("cd.type_id = ?", typeID)
	}
	// keyword 同时匹配卡密 cdkey 和使用人用户名
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(cd.cdkey LIKE ? OR u.username LIKE ?)", like, like)
	}
	switch statusStr {
	case "0":
		query = query.Where("cd.user_id IS NULL")
	case "1":
		query = query.Where("cd.user_id IS NOT NULL")
	}
	// 时间段：按生成时间过滤
	if s := strings.TrimSpace(c.Query("start_time")); s != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
			query = query.Where("cd.created_at >= ?", t)
		}
	}
	if e := strings.TrimSpace(c.Query("end_time")); e != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", e, time.Local); err == nil {
			query = query.Where("cd.created_at <= ?", t)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	var rows []cardRow
	if err := query.Order("cd.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]CardListItem, 0, len(rows))
	for _, r := range rows {
		status := model.CardUnused
		if r.UserID != nil {
			status = model.CardUsed
		}
		item := CardListItem{
			ID:        r.ID,
			CDKey:     r.CDKey,
			TypeID:    r.TypeID,
			TypeName:  r.TypeName,
			Days:      r.Days,
			Status:    status,
			UserID:    r.UserID,
			Username:  r.Username,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if r.UsedAt != nil {
			s := r.UsedAt.Format("2006-01-02 15:04:05")
			item.UsedAt = &s
		}
		list = append(list, item)
	}
	util.OK(c, util.NewPage(list, total, page, pageSize))
}

// DeleteCard 删除卡密（仅限未使用卡密）
func (h *Handler) DeleteCard(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		util.Fail(c, util.CodeParamError, "参数错误：id 不合法")
		return
	}
	var card model.Card
	if err := database.DB.First(&card, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeNotFound, "卡密不存在")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if card.IsUsed() {
		util.Fail(c, util.CodeConflict, "该卡密已使用，不能删除")
		return
	}
	if err := database.DB.Delete(&card).Error; err != nil {
		util.Fail(c, util.CodeDBError, "删除失败")
		return
	}
	util.OK(c, nil)
}
