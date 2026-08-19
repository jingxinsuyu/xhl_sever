package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"xhl-server/internal/config"
	"xhl-server/internal/crypto"
	"xhl-server/internal/migration"
	"xhl-server/internal/model"
	"xhl-server/internal/util"
)

func main() {
	cfg, _ := config.Load("config.yaml")
	db, _ := migration.Connect(&cfg.Database)

	// 1. 检查 ckdata 唯一索引
	var idxs []string
	db.Raw("SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='ckdata' AND NON_UNIQUE=0").Scan(&idxs)
	fmt.Println("ckdata 唯一索引:", idxs)

	// 2. 直接验证 insert/update 语义：同用户名更新、新用户名插入
	uid := uint64(99999)
	db.Exec("DELETE FROM ckdata WHERE user_id = ?", uid)
	// 同用户名 A 存两次 → 应 1 行(更新)
	for i := 0; i < 2; i++ {
		var ck model.CkData
		err := db.Where("user_id = ? AND username = ?", uid, "accountA").First(&ck).Error
		if err == nil {
			db.Model(&ck).Updates(map[string]any{"cookie": fmt.Sprintf("ckA-%d", i)})
		} else {
			db.Create(&model.CkData{UserID: uid, Username: "accountA", Cookie: fmt.Sprintf("ckA-%d", i)})
		}
	}
	// 新用户名 B → 应新增一行
	db.Create(&model.CkData{UserID: uid, Username: "accountB", Cookie: "ckB"})
	var cnt int64
	db.Model(&model.CkData{}).Where("user_id = ?", uid).Count(&cnt)
	fmt.Printf("直接写入: user=%d 应有 2 行(accountA+accountB), 实际 %d 行\n", uid, cnt)

	// 3. 建会员用户 → 调 qrlogin(假 sign,必失败) → 校验 ckdata 不落库
	var u model.User
	db.Where("username = ?", "cktest_user").First(&u)
	if u.ID == 0 {
		hash, _ := util.HashPassword("123456")
		u = model.User{Username: "cktest_user", Password: hash, SuperPassword: hash, Status: 1}
		db.Create(&u)
	}
	db.Where("user_id = ? AND project_id = '100001'", u.ID).First(&model.UserEntitlement{})
	db.Exec("INSERT INTO user_entitlement (user_id, project_id, expires_at, created_at, updated_at) SELECT ?, '100001', ?, NOW(), NOW() ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at)",
		u.ID, time.Now().AddDate(0, 0, 30))
	token, _ := util.GenerateToken(cfg.JWT.Secret, cfg.JWT.UserExpireHours, util.TokenTypeUser, u.ID, u.Username, model.RoleUser, u.TokenVersion)

	dataEnc, _ := crypto.AesECBEncryptPKCS7("cktest----pass----BDUSS=test; STOKEN=s", cfg.Security.QrLoginAESKey)
	body, _ := json.Marshal(map[string]interface{}{
		"loginUrl": "https://wappass.baidu.com/wp/?qrlogin&sign=fake&lp=pc",
		"data":     dataEnc,
	})
	req, _ := http.NewRequest("POST", "http://127.0.0.1:8080/api/xhl/qrlogin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	raw, _ := io.ReadAll(resp.Body)
	hasOK := bytes.Contains(raw, []byte(`"ok":true`))
	fmt.Printf("qrlogin 假 sign 结果 ok=%v(应为 false)\n", hasOK)

	var ckCnt int64
	db.Model(&model.CkData{}).Where("user_id = ?", u.ID).Count(&ckCnt)
	fmt.Printf("失败登录后 ckdata 行数: %d (应为 0,登录成功才入表)\n", ckCnt)

	// 清理
	db.Exec("DELETE FROM ckdata WHERE user_id = ?", uid)
	db.Exec("DELETE FROM ckdata WHERE user_id = ?", u.ID)
	db.Exec("DELETE FROM user_entitlement WHERE user_id = ?", u.ID)
	db.Exec("DELETE FROM user_binding WHERE user_id = ?", u.ID)
	db.Exec("DELETE FROM user WHERE id = ?", u.ID)
}
