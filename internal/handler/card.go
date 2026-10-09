package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GenerateCardsRequest 生成卡密请求
type GenerateCardsRequest struct {
	TypeID  uint64 `json:"type_id" binding:"required"` // 卡密类型 id（按类型生成）
	Count   int    `json:"count" binding:"required"`   // 生成数量
	Remark  string `json:"remark"`                     // 开卡备注（可选，写进每张卡，便于对账）
	AgentID uint64 `json:"agent_id"`                   // 代理归属（可选，0=自营；代理不是登录账号）
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

	// 卡密上记录"这张卡会发什么"，便于后台/对账（老数据为空，按天数解释）
	kind, amount := ct.KindAmount()
	// 开卡备注：与备注一起发出去，方便区分批次/渠道/客户
	remark := strings.TrimSpace(req.Remark)
	if len([]rune(remark)) > 100 {
		util.Fail(c, util.CodeParamError, "备注最多 100 字")
		return
	}
	// 代理归属：可选（0=自营）。代理只是业务档案，不是登录账号。
	// 代理卡：先落一张订单（记下单时的代理单价快照），卡再挂到订单上
	var orderID uint64
	if req.AgentID > 0 {
		var ag model.Agent
		unit := int64(0)
		agName := ""
		if err := database.DB.First(&ag, req.AgentID).Error; err == nil {
			agName = ag.Name
			var ap model.AgentPrice
			if e2 := database.DB.Where("agent_id = ? AND card_type_id = ?", req.AgentID, req.TypeID).First(&ap).Error; e2 == nil {
				unit = ap.PriceCents
			}
		}
		op := ""
		if cl := middleware.GetClaims(c); cl != nil {
			op = cl.Username
		}
		od := model.AgentOrder{
			ProjectID: ct.ProjectID, AgentID: req.AgentID, AgentName: agName,
			CardTypeID: req.TypeID, TypeName: ct.Name, Count: len(keys),
			UnitCents: unit, AmountCents: unit * int64(len(keys)),
			Remark: remark, Operator: op,
		}
		if err := database.DB.Create(&od).Error; err == nil {
			orderID = od.ID
		}
	}

	cards := make([]model.Card, 0, len(keys))
	for _, k := range keys {
		cards = append(cards, model.Card{
			ProjectID:     ct.ProjectID,
			TypeID:        req.TypeID,
			CDKey:         k,
			GrantedKind:   kind,
			GrantedAmount: amount,
			Remark:        remark,
			AgentID:       req.AgentID,
			OrderID:       orderID,
		})
	}
	if err := database.DB.CreateInBatches(cards, 500).Error; err != nil {
		util.Fail(c, util.CodeDBError, "保存卡密失败")
		return
	}

	h.recordAudit(c, model.AuditCardGenerate, ct.ProjectID, "card_type:"+strconv.FormatUint(ct.ID, 10),
		gin.H{"type_name": ct.Name, "kind": kind, "amount": amount, "count": len(keys), "remark": remark, "agent_id": req.AgentID, "order_id": orderID})
	util.OK(c, gin.H{"count": len(keys), "project_id": ct.ProjectID, "type_id": req.TypeID,
		"kind": kind, "amount": amount, "remark": remark, "agent_id": req.AgentID, "order_id": orderID, "cdkeys": keys})
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
	// v2：卡上记录的发放内容（老数据为空/0，回退到类型配置）
	GrantedKind   string
	GrantedAmount int
	CTKind        string `gorm:"column:ct_kind"`
	CTAmount      int    `gorm:"column:ct_amount"`
	Remark        string // v2：开卡备注
	AgentID       uint64 // v2：代理归属（0=自营）
	SettleID      uint64 // v2：结算单 id（0=未结算）
	AgentName     string // v2：代理名（联表）
}

// CardListItem 卡密列表项
type CardListItem struct {
	ID        uint64  `json:"id"`
	CDKey     string  `json:"cdkey"`
	TypeID    uint64  `json:"type_id"`
	TypeName  string  `json:"type_name"`
	Days      int     `json:"days"`
	Kind      string  `json:"kind"`   // v2：days | calls | credits
	Amount    int     `json:"amount"` // v2：发放数量
	Status    int     `json:"status"` // 0 未使用 / 1 已使用
	UserID    *uint64 `json:"user_id"`
	Username  string  `json:"username"`   // 使用人用户名（反查）
	Remark    string  `json:"remark"`     // v2：开卡备注
	AgentID   uint64  `json:"agent_id"`   // v2：代理归属（0=自营）
	AgentName string  `json:"agent_name"` // v2：代理名
	Settled   bool    `json:"settled"`    // v2：是否已结算
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
		Select("cd.id, cd.cdkey, cd.type_id, cd.user_id, cd.used_at, cd.created_at, "+
			"cd.granted_kind, cd.granted_amount, cd.remark, ct.name AS type_name, ct.days, ct.kind AS ct_kind, ct.amount AS ct_amount, u.username AS username").
		Joins("LEFT JOIN card_type AS ct ON ct.id = cd.type_id").
		Joins("LEFT JOIN user AS u ON u.id = cd.user_id").
		Where("cd.project_id = ?", projectID)

		// 按代理筛选：agent_id=0 表示自营（无代理）
	if typeID > 0 {
		query = query.Where("cd.type_id = ?", typeID)
	}
	// keyword 同时匹配卡密 cdkey、使用人用户名 和 开卡备注
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(cd.cdkey LIKE ? OR u.username LIKE ? OR cd.remark LIKE ?)", like, like, like)
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

	// 分页器上的统计：当前筛选条件下的「已用 / 未用」张数
	// （复用同一套筛选条件，只把 Select 换成计数，避免和列表口径不一致）
	var used, unused int64
	countQ := database.DB.Table("card AS cd").
		Joins("LEFT JOIN card_type AS ct ON ct.id = cd.type_id").
		Joins("LEFT JOIN user AS u ON u.id = cd.user_id").
		Where("cd.project_id = ?", projectID)
	if typeID > 0 {
		countQ = countQ.Where("cd.type_id = ?", typeID)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		countQ = countQ.Where("(cd.cdkey LIKE ? OR u.username LIKE ? OR cd.remark LIKE ?)", like, like, like)
	}
	switch statusStr {
	case "0":
		countQ = countQ.Where("cd.user_id IS NULL")
	case "1":
		countQ = countQ.Where("cd.user_id IS NOT NULL")
	}
	if s := strings.TrimSpace(c.Query("start_time")); s != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
			countQ = countQ.Where("cd.created_at >= ?", t)
		}
	}
	if e := strings.TrimSpace(c.Query("end_time")); e != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", e, time.Local); err == nil {
			countQ = countQ.Where("cd.created_at <= ?", t)
		}
	}
	if err := countQ.Where("cd.user_id IS NOT NULL").Count(&used).Error; err != nil {
		util.Fail(c, util.CodeDBError, "统计失败")
		return
	}
	unused = total - used

	var rows []cardRow
	// 排序：默认按生成时间倒序；点表头可切（未使用的卡 used_at 为 NULL，统一放最后）
	switch strings.TrimSpace(c.Query("sort")) {
	case "created_asc":
		query = query.Order("cd.id ASC")
	case "used_desc":
		query = query.Order("cd.used_at IS NULL, cd.used_at DESC, cd.id DESC")
	case "used_asc":
		query = query.Order("cd.used_at IS NULL, cd.used_at ASC, cd.id ASC")
	default:
		query = query.Order("cd.id DESC")
	}
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		util.Fail(c, util.CodeDBError, "查询失败")
		return
	}

	list := make([]CardListItem, 0, len(rows))
	for _, r := range rows {
		status := model.CardUnused
		if r.UserID != nil {
			status = model.CardUsed
		}
		// 优先用卡上记录的发放内容；老卡没有记录就按类型配置归一化（只有 days 的类型也算 days）
		kind, amount := r.GrantedKind, r.GrantedAmount
		if kind == "" || amount <= 0 {
			ct := model.CardType{Days: r.Days, Kind: r.CTKind, Amount: r.CTAmount}
			kind, amount = ct.KindAmount()
		}
		item := CardListItem{
			ID:        r.ID,
			CDKey:     r.CDKey,
			TypeID:    r.TypeID,
			TypeName:  r.TypeName,
			Days:      r.Days,
			Kind:      kind,
			Amount:    amount,
			Status:    status,
			UserID:    r.UserID,
			Username:  r.Username,
			Remark:    r.Remark,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if r.UsedAt != nil {
			s := r.UsedAt.Format("2006-01-02 15:04:05")
			item.UsedAt = &s
		}
		list = append(list, item)
	}
	// 分页字段与 util.NewPage 一致，额外带上「已用/未用」统计供分页器展示
	util.OK(c, gin.H{
		"list": list, "total": total, "page": page, "page_size": pageSize,
		"used": used, "unused": unused,
	})
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
	h.recordAudit(c, model.AuditCardDelete, card.ProjectID, "card:"+strconv.FormatUint(card.ID, 10),
		gin.H{"type_id": card.TypeID, "cdkey_tail": keyTail(card.CDKey)})
	util.OK(c, nil)
}
