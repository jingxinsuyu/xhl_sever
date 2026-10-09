package util

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("123456")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if !CheckPassword(hash, "123456") {
		t.Fatal("正确密码应校验通过")
	}
	if CheckPassword(hash, "654321") {
		t.Fatal("错误密码不应校验通过")
	}
}
