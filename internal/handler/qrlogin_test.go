package handler

import (
	"testing"

	"xhl-server/internal/crypto"
)

// TestDecryptAccountData 单层 base64 加密 → 解密按 ---- 分割出用户名/密码/cookie。
func TestDecryptAccountData(t *testing.T) {
	key := "czgrFN1A5f6Vfina"
	plain := "baidu_user----pass123----BDUSS=abc; STOKEN=xyz"
	enc, err := crypto.AesECBEncryptPKCS7(plain, key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	user, pass, cookie, err := decryptAccountData(enc, key, "")
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if user != "baidu_user" || pass != "pass123" || cookie != "BDUSS=abc; STOKEN=xyz" {
		t.Fatalf("分割错误: %q / %q / %q", user, pass, cookie)
	}
}

// TestDecryptAccountDataFallbackDouble64 双重 base64（旧约定）也应能解密。
func TestDecryptAccountDataFallbackDouble64(t *testing.T) {
	key := "czgrFN1A5f6Vfina"
	enc, err := crypto.AesEncryptDouble64("u----p----c", key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	user, pass, cookie, err := decryptAccountData(enc, key, "")
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if user != "u" || pass != "p" || cookie != "c" {
		t.Fatalf("分割错误: %q / %q / %q", user, pass, cookie)
	}
}

// TestDecryptAccountDataBad 无法解密 / 缺字段应报错。
func TestDecryptAccountDataBad(t *testing.T) {
	if _, _, _, err := decryptAccountData("not-valid", "keykeykeykeykey1", ""); err == nil {
		t.Fatal("非法 data 应报错")
	}
	enc, _ := crypto.AesECBEncryptPKCS7("只有两段----没有cookie", "keykeykeykeykey1")
	if _, _, _, err := decryptAccountData(enc, "keykeykeykeykey1", ""); err == nil {
		t.Fatal("缺 cookie 段应报错")
	}
}
