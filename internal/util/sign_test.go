package util

import (
	"crypto/md5"
	"fmt"
	"testing"
)

func TestVerifyClientSign(t *testing.T) {
	salt := "test-salt"
	params := map[string]string{
		"username": "user01",
		"project_id": "2",
		"ts":        "1786000000",
	}
	// 正确签名应通过
	sign := fmt.Sprintf("%x", md5.Sum([]byte(SignParams(params, salt))))
	if !VerifyClientSign(params, salt, sign) {
		t.Fatal("正确签名应通过校验")
	}
	// 错误盐应失败
	if VerifyClientSign(params, "other-salt", sign) {
		t.Fatal("错误盐不应通过")
	}
	// 参数被篡改应失败
	tampered := map[string]string{
		"username": "user02",
		"project_id": "2",
		"ts":        "1786000000",
	}
	if VerifyClientSign(tampered, salt, sign) {
		t.Fatal("被篡改的参数不应通过")
	}
	// 空签名应失败
	if VerifyClientSign(params, salt, "") {
		t.Fatal("空签名不应通过")
	}
}
