package model

import "time"

// 项目变量类型
const (
	VarTypeString = "string"
	VarTypeInt    = "int"
	VarTypeBool   = "bool"
	VarTypeJSON   = "json"
)

// ProjectVariable 项目变量表（string/int/bool/json 简单变量）
type ProjectVariable struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID uint64    `gorm:"not null;index" json:"project_id"`
	Key       string    `gorm:"size:64;not null" json:"key"`
	Type      string    `gorm:"size:16;not null;default:string" json:"type"` // string / int / bool / json
	Value     string    `gorm:"size:1024;not null;default:''" json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ProjectVariable) TableName() string { return "project_variable" }
