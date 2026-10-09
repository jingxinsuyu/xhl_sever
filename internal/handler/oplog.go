package handler

import (
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/model"

	"github.com/gin-gonic/gin"
)

// 写记录的两条原则（很重要）：
//  1. **只增不改**，失败一律忽略——记流水绝不能把扫码登录这类主流程搞挂；
//  2. 只在流程最后写，不在中途写，避免一次调用写出好几条。

// CallLogEntry 一次调用的记录内容。
type CallLogEntry struct {
	ProjectID  string
	Action     string // model.CallActionXxx
	Source     string // model.CallSourceXxx
	UserID     uint64
	Username   string
	ApiKeyID   uint64
	ApiKeyName string
	Ok         bool
	Errno      string
	Message    string
	CostKind   string // membership / calls / credits
	Cost       int
	Started    time.Time // 起始时间，用于算耗时；零值则耗时记 0
}

// logCall 落一条调用记录。
func (h *Handler) logCall(c *gin.Context, e CallLogEntry) {
	if e.ProjectID == "" && e.UserID == 0 && e.ApiKeyID == 0 {
		return // 什么都没记的必要
	}
	msg := e.Message
	if len(msg) > 255 {
		msg = msg[:255]
	}
	entry := model.CallLog{
		ProjectID:  e.ProjectID,
		Action:     e.Action,
		Source:     e.Source,
		UserID:     e.UserID,
		Username:   e.Username,
		ApiKeyID:   e.ApiKeyID,
		ApiKeyName: e.ApiKeyName,
		Ok:         e.Ok,
		Errno:      e.Errno,
		Message:    msg,
		CostKind:   e.CostKind,
		Cost:       e.Cost,
		IP:         clientIP(c),
	}
	if !e.Started.IsZero() {
		if d := time.Since(e.Started).Milliseconds(); d >= 0 {
			entry.DurationMS = d
		}
	}
	_ = database.DB.Create(&entry).Error
}

// BillingEntry 一次额度变动流水。
type BillingEntry struct {
	ProjectID    string
	UserID       uint64
	Username     string
	ApiKeyID     uint64 // 开放平台按 key 扣费时填这里（user_id 为 0）
	Kind         string // days / calls / credits
	Delta        int    // 正=加，负=扣
	BalanceAfter int
	Reason       string // model.BillReasonXxx
	RefID        uint64
	Remark       string
}

// logBilling 落一条额度变动流水（卡密兑换 / 管理员发放 / 调用扣费都走这里）。
func (h *Handler) logBilling(e BillingEntry) {
	if e.ProjectID == "" || (e.UserID == 0 && e.ApiKeyID == 0) {
		return
	}
	remark := e.Remark
	if len(remark) > 255 {
		remark = remark[:255]
	}
	_ = database.DB.Create(&model.BillingLog{
		ProjectID:    e.ProjectID,
		UserID:       e.UserID,
		Username:     e.Username,
		ApiKeyID:     e.ApiKeyID,
		Kind:         e.Kind,
		Delta:        e.Delta,
		BalanceAfter: e.BalanceAfter,
		Reason:       e.Reason,
		RefID:        e.RefID,
		Remark:       remark,
	}).Error
}

// usernameOf 取用户名（找不到返回空串，用于给流水冗余一份可读名）。
func usernameOf(userID uint64) string {
	if userID == 0 {
		return ""
	}
	var u model.User
	if err := database.DB.Select("id", "username").First(&u, userID).Error; err != nil {
		return ""
	}
	return u.Username
}
