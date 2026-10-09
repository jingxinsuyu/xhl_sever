package model

import "time"

// 管理员角色
const (
	RoleSuper = "super" // 超级管理员（yaml 配置，不落库）
	RoleAdmin = "admin" // 普通管理员
)

// Admin 管理员表
type Admin struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username  string    `gorm:"size:64;not null;uniqueIndex" json:"username"`
	Password  string    `gorm:"size:255;not null" json:"-"`
	Nickname  string    `gorm:"size:64;not null;default:''" json:"nickname"`
	Remark    string    `gorm:"size:255;not null;default:''" json:"remark"`
	Status    int8      `gorm:"not null;default:1" json:"status"` // 1 启用 / 0 禁用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Admin) TableName() string { return "admin" }
