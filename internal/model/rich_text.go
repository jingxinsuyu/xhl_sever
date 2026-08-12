package model

import "time"

// RichTextAd 项目富文本广告表（每个项目初始为空，编辑保存）
type RichTextAd struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID uint64    `gorm:"not null;uniqueIndex" json:"project_id"`
	Content   string    `gorm:"type:text;not null" json:"content"` // 富文本 HTML
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (RichTextAd) TableName() string { return "rich_text_ad" }
