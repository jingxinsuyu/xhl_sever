package model

import "time"

// UserBinding 用户在某项目下的绑定（登录时按设备码自动绑定，换设备码需解绑）。
// MachineCode 存设备码（稳定）；IP 仅记录最近登录来源，不参与拦截。
type UserBinding struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint64    `gorm:"not null;index:idx_user_project,unique" json:"user_id"`
	ProjectID   string    `gorm:"not null;size:6;index:idx_user_project,unique" json:"project_id"`
	MachineCode string    `gorm:"size:128;not null;index:idx_user_project,unique" json:"machine_code"` // 设备码
	IP          string    `gorm:"size:64;not null;default:''" json:"ip"`                              // 最近登录 IP（仅记录）
	CreatedAt   time.Time `json:"created_at"`
}

func (UserBinding) TableName() string { return "user_binding" }
