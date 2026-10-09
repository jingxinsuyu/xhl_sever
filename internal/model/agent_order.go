package model

import "time"

// AgentOrder 代理订单：每次「批量生成代理卡」自动落一张订单。
// 单价是**下单时的快照**（分），以后改代理价不影响老订单对账。
type AgentOrder struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID   string    `gorm:"not null;size:6;index" json:"project_id"`
	AgentID     uint64    `gorm:"not null;index" json:"agent_id"`
	AgentName   string    `gorm:"size:64;not null;default:''" json:"agent_name"`
	CardTypeID  uint64    `gorm:"not null;default:0" json:"card_type_id"`
	TypeName    string    `gorm:"size:64;not null;default:''" json:"type_name"`
	Count       int       `gorm:"not null;default:0" json:"count"`        // 本单开卡张数
	UnitCents   int64     `gorm:"not null;default:0" json:"unit_cents"`   // 下单时的代理单价（分）
	AmountCents int64     `gorm:"not null;default:0" json:"amount_cents"` // 订单金额 = Count × UnitCents
	Remark      string    `gorm:"size:255;not null;default:''" json:"remark"`
	Operator    string    `gorm:"size:64;not null;default:''" json:"operator"`
	CreatedAt   time.Time `json:"created_at"`
}

func (AgentOrder) TableName() string { return "agent_order" }
