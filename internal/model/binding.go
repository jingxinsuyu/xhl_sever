package model

import "time"

// UserBinding 用户在某项目下的绑定（登录时自动绑定，换 IP 需解绑）。
// 注意：MachineCode 字段复用存**请求来源 IP**（原机器码语义废弃；客户端仍传机器码误导机器，后端按 IP 绑定）。
type UserBinding struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint64    `gorm:"not null;index:idx_user_project,unique" json:"user_id"`
	ProjectID   string    `gorm:"not null;size:6;index:idx_user_project,unique" json:"project_id"`
	MachineCode string    `gorm:"size:128;not null;index:idx_user_project,unique" json:"machine_code"` // 存 IP
	CreatedAt   time.Time `json:"created_at"`
}

func (UserBinding) TableName() string { return "user_binding" }
