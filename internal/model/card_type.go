package model

import "time"

// CardType 卡密类型表（软删除，防止卡密生成使用后被删掉或报错）
type CardType struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID uint64     `gorm:"not null;index" json:"project_id"`
	Name      string     `gorm:"size:64;not null" json:"name"`
	Days      int        `gorm:"not null;default:0" json:"days"` // 充值天数
	DeletedAt *time.Time `gorm:"index" json:"deleted_at"`         // 软删除时间，NULL 未删除
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (CardType) TableName() string { return "card_type" }
