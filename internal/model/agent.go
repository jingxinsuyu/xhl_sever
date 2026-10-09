package model

import "time"

// Agent 代理商信息（每个项目一套）。
// 注意：代理**不是登录账号**，不能登录后台；它只是「开卡归属 + 结算」用的业务档案。
type Agent struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string    `gorm:"not null;size:6;index" json:"project_id"`
	Name      string    `gorm:"size:64;not null" json:"name"`               // 代理名称/编号
	Contact   string    `gorm:"size:64;not null;default:''" json:"contact"` // 联系人
	Phone     string    `gorm:"size:64;not null;default:''" json:"phone"`   // 联系方式（QQ/微信/手机）
	Remark    string    `gorm:"size:255;not null;default:''" json:"remark"`
	Status    int       `gorm:"not null;default:1" json:"status"` // 1 启用 / 0 停用
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Agent) TableName() string { return "agent" }

// AgentPrice 代理在某个卡密类型上的结算单价（单位：分，整数避免浮点误差）
type AgentPrice struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID    uint64    `gorm:"not null;uniqueIndex:idx_agent_type" json:"agent_id"`
	CardTypeID uint64    `gorm:"not null;uniqueIndex:idx_agent_type" json:"card_type_id"`
	PriceCents int64     `gorm:"not null;default:0" json:"price_cents"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (AgentPrice) TableName() string { return "agent_price" }

// CardSettlement 一次结算记录：把勾选的卡按「代理 × 卡密类型」单价汇总成一张结算单。
type CardSettlement struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID   string    `gorm:"not null;size:6;index" json:"project_id"`
	AgentID     uint64    `gorm:"not null;index" json:"agent_id"`
	AgentName   string    `gorm:"size:64;not null;default:''" json:"agent_name"`
	CardCount   int       `gorm:"not null;default:0" json:"card_count"` // 本次结算张数
	UsedCount   int       `gorm:"not null;default:0" json:"used_count"` // 其中已使用的张数
	AmountCents int64     `gorm:"not null;default:0" json:"amount_cents"`
	Remark      string    `gorm:"size:255;not null;default:''" json:"remark"`
	Operator    string    `gorm:"size:64;not null;default:''" json:"operator"` // 操作管理员
	CreatedAt   time.Time `json:"created_at"`
}

func (CardSettlement) TableName() string { return "card_settlement" }
