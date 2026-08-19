package model

import "time"

// ApiKey 第三方开放接口 API Key（按项目发放）。
// key 为 sk- 前缀 + 32 位 hex（DeepSeek 风格），唯一；status=1 启用 / 0 禁用。
type ApiKey struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string    `gorm:"size:6;not null;index" json:"project_id"` // 所属项目 id（6 位数字）
	Name      string    `gorm:"size:64;not null" json:"name"`            // 名称（如「某某平台」）
	Key       string    `gorm:"size:64;not null;uniqueIndex" json:"key"` // sk-xxx
	Remark    string    `gorm:"size:255;not null;default:''" json:"remark"`
	Status    int8      `gorm:"not null;default:1" json:"status"` // 1 启用 / 0 禁用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ApiKey) TableName() string { return "apikey" }
