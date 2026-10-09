package model

import "time"

// 审计动作常量（后台写操作留痕用）
const (
	AuditProjectCreate   = "project.create"
	AuditProjectUpdate   = "project.update"
	AuditProjectSetting  = "project.setting"
	AuditProjectDelete   = "project.delete"
	AuditProjectRestore  = "project.restore"
	AuditUserGrant       = "user.grant"
	AuditUserEntitlement = "user.entitlement" // 设置计费覆盖 / 外部用户号 / 备注
	AuditUserStatus      = "user.status"
	AuditUserPassword    = "user.password"
	AuditUserUnbind      = "user.unbind"
	AuditUserClearLogin  = "user.clear_login"
	AuditCardTypeCreate  = "card_type.create"
	AuditCardTypeDelete  = "card_type.delete"
	AuditCardGenerate    = "card.generate"
	AuditCardDelete      = "card.delete"
	AuditProxyConfig     = "proxy.config"
	AuditCkDataExport    = "ckdata.export"
	AuditApiKeyCreate    = "apikey.create"
	AuditApiKeyRecharge  = "apikey.recharge"
	AuditApiKeyAdjust    = "apikey.adjust"
	AuditApiKeyStatus    = "apikey.status"
	AuditApiKeyDelete    = "apikey.delete"
)

// AuditLog 后台操作审计：谁、什么时候、对哪个项目/对象、做了什么。
// 只增不改，用于对账与追责（v1 完全没有留痕）。
type AuditLog struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AdminID   uint64    `gorm:"not null;default:0;index" json:"admin_id"`
	AdminName string    `gorm:"size:64;not null;default:''" json:"admin_name"`
	ProjectID string    `gorm:"size:6;not null;default:'';index" json:"project_id"`
	Action    string    `gorm:"size:64;not null;default:'';index" json:"action"`
	Target    string    `gorm:"size:128;not null;default:''" json:"target"` // 如 user:123 / apikey:11
	Detail    string    `gorm:"type:text" json:"detail"`                    // JSON：变更前后/增减量
	IP        string    `gorm:"size:64;not null;default:''" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_log" }
