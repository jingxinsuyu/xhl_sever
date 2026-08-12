package model

import "time"

// Project 项目表（软删除，前端不显示已删除项目）
type Project struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string     `gorm:"size:64;not null" json:"name"`
	Remark      string     `gorm:"size:255;not null;default:''" json:"remark"`
	LoginLimit  int        `gorm:"not null;default:0" json:"login_limit"`  // 0 不限制；N 该用户今日只能登录 N 次
	UnbindLimit int        `gorm:"not null;default:0" json:"unbind_limit"` // 0 不限制；N 该用户今日只能自助解绑 N 次
	DeletedAt  *time.Time `gorm:"index" json:"deleted_at"`               // 软删除时间，NULL 未删除
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (Project) TableName() string { return "project" }
