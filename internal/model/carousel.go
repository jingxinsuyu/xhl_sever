package model

import "time"

// Carousel 项目轮播图表（380×65 jpg + 跳转链接，链接为空不跳）
type Carousel struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string    `gorm:"not null;size:6;index" json:"project_id"`
	ImagePath string    `gorm:"size:255;not null;default:''" json:"image_path"` // 存储相对路径
	Link      string    `gorm:"size:512;not null;default:''" json:"link"`       // 跳转连接，为空不跳
	CreatedAt time.Time `json:"created_at"`
}

func (Carousel) TableName() string { return "carousel" }
