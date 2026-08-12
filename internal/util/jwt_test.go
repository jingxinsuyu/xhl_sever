package util

import "testing"

func TestGenerateAndParseToken(t *testing.T) {
	secret := "test-secret"
	token, err := GenerateToken(secret, 1, TokenTypeUser, 42, "hello", "user", 7)
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	claims, err := ParseToken(secret, token)
	if err != nil {
		t.Fatalf("ParseToken error: %v", err)
	}
	if claims.Type != TokenTypeUser {
		t.Errorf("type 不匹配: %s", claims.Type)
	}
	if claims.UserID != 42 {
		t.Errorf("user_id 不匹配: %d", claims.UserID)
	}
	if claims.Username != "hello" {
		t.Errorf("username 不匹配: %s", claims.Username)
	}
	if claims.Ver != 7 {
		t.Errorf("ver 不匹配: %d", claims.Ver)
	}
}

func TestParseTokenWrongSecret(t *testing.T) {
	token, err := GenerateToken("secret-a", 1, TokenTypeAdmin, 0, "root", "super", 0)
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	if _, err := ParseToken("secret-b", token); err == nil {
		t.Fatal("用错误密钥解析应失败")
	}
}
