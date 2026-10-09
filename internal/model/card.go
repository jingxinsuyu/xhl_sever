package model

import "time"

// 卡密使用状态
const (
	CardUnused = 0 // 未使用
	CardUsed   = 1 // 已使用
)

// Card 卡密表
type Card struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string     `gorm:"not null;size:6;index" json:"project_id"`
	TypeID    uint64     `gorm:"not null;index" json:"type_id"` // 所属卡密类型 id
	CDKey     string     `gorm:"column:cdkey;size:64;not null;uniqueIndex" json:"cdkey"`
	UserID    *uint64    `gorm:"index" json:"user_id"` // 使用人，NULL 未使用
	UsedAt    *time.Time `json:"used_at"`              // 使用时间
	// ---- v2 新增：记录这张卡实际发放了什么（天数/次数/积分）----
	GrantedKind   string `gorm:"size:16;not null;default:''" json:"granted_kind"` // days | calls | credits（空=老数据，按天数解释）
	GrantedAmount int    `gorm:"not null;default:0" json:"granted_amount"`
	// ---- v2 新增：开卡备注（发放批次/渠道/客户等，便于对账；只增列，不动老数据）----
	Remark string `gorm:"size:255;not null;default:''" json:"remark"`
	// ---- v2 新增：代理归属与结算（agent_id=0 表示自营；settle_id=0 表示未结算）----
	AgentID   uint64     `gorm:"not null;default:0;index" json:"agent_id"`
	SettleID  uint64     `gorm:"not null;default:0;index" json:"settle_id"`
	SettledAt *time.Time `json:"settled_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func (Card) TableName() string { return "card" }

// IsUsed 是否已被使用
func (c *Card) IsUsed() bool {
	return c.UserID != nil || c.UsedAt != nil
}
