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
	ProjectID uint64     `gorm:"not null;index" json:"project_id"`
	TypeID    uint64     `gorm:"not null;index" json:"type_id"` // 所属卡密类型 id
	CDKey     string     `gorm:"column:cdkey;size:64;not null;uniqueIndex" json:"cdkey"`
	UserID    *uint64    `gorm:"index" json:"user_id"` // 使用人，NULL 未使用
	UsedAt    *time.Time `json:"used_at"`              // 使用时间
	CreatedAt time.Time  `json:"created_at"`
}

func (Card) TableName() string { return "card" }

// IsUsed 是否已被使用
func (c *Card) IsUsed() bool {
	return c.UserID != nil || c.UsedAt != nil
}
