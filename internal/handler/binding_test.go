package handler

import (
	"testing"

	"xhl-server/internal/database"
	"xhl-server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestCheckOrBindDevice 验证设备码绑定逻辑（需本地 MySQL，hlong 库）。
// 覆盖：首次绑设备码 / 同设备码换 IP 放行并记录 / 其他设备码拒绝 / 解绑后可重建。
func TestCheckOrBindDevice(t *testing.T) {
	db, err := gorm.Open(mysql.Open(
		"root:320326..@tcp(127.0.0.1:3306)/hlong?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{})
	if err != nil {
		t.Skip("本地 MySQL 不可用:", err)
	}
	database.DB = db
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")

	h := &Handler{}
	u := &model.User{ID: 22}

	// 1. 首次绑设备码 device-A（IP 1.1.1.1）
	if ok, err := h.checkOrBindDevice(u, "100001", "device-A", "1.1.1.1"); err != nil || !ok {
		t.Fatalf("首次绑定应成功: ok=%v err=%v", ok, err)
	}
	// 2. 同设备码换 IP → 放行（IP 仅记录不拦截）
	if ok, err := h.checkOrBindDevice(u, "100001", "device-A", "2.2.2.2"); err != nil || !ok {
		t.Fatalf("同设备码换 IP 应放行: ok=%v err=%v", ok, err)
	}
	// 3. 验证绑定记录：device 存设备码，ip 已更新为最新
	var b model.UserBinding
	if err := db.Where("user_id = ? AND project_id = ?", 22, "100001").First(&b).Error; err != nil {
		t.Fatalf("查绑定失败: %v", err)
	}
	if b.MachineCode != "device-A" {
		t.Fatalf("machine_code 应存设备码 device-A，实际 %q", b.MachineCode)
	}
	if b.IP != "2.2.2.2" {
		t.Fatalf("ip 应更新为 2.2.2.2，实际 %q", b.IP)
	}
	// 4. 其他设备码拒绝（防共用号）
	if ok, _ := h.checkOrBindDevice(u, "100001", "device-B", "3.3.3.3"); ok {
		t.Fatal("其他设备码应拒绝")
	}
	// 5. 解绑后可重建
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")
	if ok, err := h.checkOrBindDevice(u, "100001", "device-C", "4.4.4.4"); err != nil || !ok {
		t.Fatalf("解绑后应可重建: ok=%v err=%v", ok, err)
	}
	// 清理
	db.Exec("DELETE FROM user_binding WHERE user_id = 22")
}
