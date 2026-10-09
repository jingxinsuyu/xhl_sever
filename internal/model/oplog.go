package model

import "time"

// 调用动作（call_log.action）
const (
	CallActionQrLogin   = "qrlogin"    // 扫码确认
	CallActionNetdisk   = "netdisk"    // BDUSS 转网盘 cookie
	CallActionCheckScan = "check-scan" // 扫码前账号检测
)

// 调用来源（call_log.source）
const (
	CallSourceUser = "user" // 客户端登录用户
	CallSourceOpen = "open" // 开放平台 API Key
)

// 扣费原因（billing_log.reason）
const (
	BillReasonCall  = "call"  // 调用扣费
	BillReasonCard  = "card"  // 卡密兑换发放
	BillReasonGrant = "grant" // 管理员直接发放
)

// CallLog 调用记录：一个项目一份流水，后台可按项目看"谁在什么时候调了什么、成没成、扣了多少"。
// 只用于展示与排查，不参与任何业务判断；写入失败一律忽略，绝不能影响主流程。
type CallLog struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID  string    `gorm:"size:6;not null;default:'';index" json:"project_id"`
	Action     string    `gorm:"size:16;not null;default:'';index" json:"action"`
	Source     string    `gorm:"size:8;not null;default:'';index" json:"source"`
	UserID     uint64    `gorm:"not null;default:0;index" json:"user_id"`
	Username   string    `gorm:"size:64;not null;default:''" json:"username"` // 冗余一份，用户改名/删除后仍可读
	ApiKeyID   uint64    `gorm:"not null;default:0;index" json:"api_key_id"`
	ApiKeyName string    `gorm:"size:64;not null;default:''" json:"api_key_name"`
	Ok         bool      `gorm:"not null;default:false;index" json:"ok"`
	Errno      string    `gorm:"size:32;not null;default:''" json:"errno"`
	Message    string    `gorm:"size:255;not null;default:''" json:"message"`
	CostKind   string    `gorm:"size:16;not null;default:''" json:"cost_kind"` // membership=不扣 / calls / credits
	Cost       int       `gorm:"not null;default:0" json:"cost"`
	IP         string    `gorm:"size:64;not null;default:''" json:"ip"`
	DurationMS int64     `gorm:"not null;default:0" json:"duration_ms"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (CallLog) TableName() string { return "call_log" }

// BillingLog 扣费/发放流水（账本）：任何额度变动都必须在这里留一条，用于对账。
// 只增不改，不参与业务判断。
type BillingLog struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID    string    `gorm:"size:6;not null;default:'';index" json:"project_id"`
	UserID       uint64    `gorm:"not null;default:0;index" json:"user_id"`
	Username     string    `gorm:"size:64;not null;default:''" json:"username"`
	ApiKeyID     uint64    `gorm:"not null;default:0;index" json:"api_key_id"`
	Kind         string    `gorm:"size:16;not null;default:'';index" json:"kind"` // days / calls / credits
	Delta        int       `gorm:"not null;default:0" json:"delta"`               // 正=加，负=扣
	BalanceAfter int       `gorm:"not null;default:0" json:"balance_after"`       // 变动后余额（days 类为 0）
	Reason       string    `gorm:"size:16;not null;default:'';index" json:"reason"`
	RefID        uint64    `gorm:"not null;default:0" json:"ref_id"` // 关联卡密 id 等
	Remark       string    `gorm:"size:255;not null;default:''" json:"remark"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

func (BillingLog) TableName() string { return "billing_log" }
