package handler

import (
	"testing"

	"xhl-server/internal/database"
	"xhl-server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestCheckOrBindIP 验证 IP 绑定逻辑（需本地 MySQL，hlong 库）。
// 覆盖：首次绑 IP / 同 IP 放行 / 不同 IP 拒绝 / 解绑后可重建。
func TestCheckOrBindIP(t *testing.T) {
	db, err := gorm.Open(mysql.Open(
		"root:320326..@tcp(127.0.0.1:3306)/hlong?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{})
	if err != nil {
		t.Skip("本地 MySQL 不可用:", err)
	}
	database.DB = db
	// 清理测试用户 22 的绑定
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")

	h := &Handler{}
	u := &model.User{ID: 22}

	// 1. 首次绑 IP 1.1.1.1
	if ok, err := h.checkOrBindIP(u, "100001", "1.1.1.1"); err != nil || !ok {
		t.Fatalf("首次绑定应成功: ok=%v err=%v", ok, err)
	}
	// 2. 同 IP 放行
	if ok, err := h.checkOrBindIP(u, "100001", "1.1.1.1"); err != nil || !ok {
		t.Fatalf("同 IP 应放行: ok=%v err=%v", ok, err)
	}
	// 3. 不同 IP 拒绝
	if ok, _ := h.checkOrBindIP(u, "100001", "2.2.2.2"); ok {
		t.Fatal("不同 IP 应拒绝")
	}
	// 4. 验证存的是 IP（machine_code 列）
	var b model.UserBinding
	if err := db.Where("user_id = ? AND project_id = ?", 22, "100001").First(&b).Error; err != nil {
		t.Fatalf("查绑定失败: %v", err)
	}
	if b.MachineCode != "1.1.1.1" {
		t.Fatalf("machine_code 应存 IP 1.1.1.1，实际 %q", b.MachineCode)
	}
	// 5. 解绑后可重建
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")
	if ok, err := h.checkOrBindIP(u, "100001", "3.3.3.3"); err != nil || !ok {
		t.Fatalf("解绑后应可重建: ok=%v err=%v", ok, err)
	}
	// 清理
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")
}
