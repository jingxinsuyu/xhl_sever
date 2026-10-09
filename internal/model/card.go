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
	GrantedKind   string    `gorm:"size:16;not null;default:''" json:"granted_kind"` // days | calls | credits（空=老数据，按天数解释）
	GrantedAmount int       `gorm:"not null;default:0" json:"granted_amount"`
	CreatedAt     time.Time `json:"created_at"`
}

func (Card) TableName() string { return "card" }

// IsUsed 是否已被使用
func (c *Card) IsUsed() bool {
	return c.UserID != nil || c.UsedAt != nil
}
