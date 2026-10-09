package model

import "time"

// 用户状态
const (
	UserStatusFrozen = 0 // 冻结
	UserStatusNormal = 1 // 启用
)

// 用户角色（用于 JWT claims）
const RoleUser = "user"

// User 用户表
type User struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username      string    `gorm:"size:64;not null;uniqueIndex" json:"username"`
	Password      string    `gorm:"size:255;not null" json:"-"`            // 登录密码
	SuperPassword string    `gorm:"size:255;not null;default:''" json:"-"` // 超级密码（解绑等敏感操作校验用）
	TokenVersion  int64     `gorm:"not null;default:0" json:"-"`           // token 版本（登录时自增，旧 token 失效）
	Status        int8      `gorm:"not null;default:1" json:"status"`      // 1 启用 / 0 冻结
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (User) TableName() string { return "user" }
