package model

import "time"

// UserBinding 用户在某项目下的机器码绑定（登录时自动绑定，换设备需解绑）
type UserBinding struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint64    `gorm:"not null;index:idx_user_project,unique" json:"user_id"`
	ProjectID   uint64    `gorm:"not null;index:idx_user_project,unique" json:"project_id"`
	MachineCode string    `gorm:"size:128;not null;index:idx_user_project,unique" json:"machine_code"`
	CreatedAt   time.Time `json:"created_at"`
}

func (UserBinding) TableName() string { return "user_binding" }
