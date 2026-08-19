package handler

import (
	"strings"
	"testing"
)

func TestGenerateApiKey(t *testing.T) {
	key, err := generateApiKey()
	if err != nil {
		t.Fatalf("generateApiKey error: %v", err)
	}
	// 格式：sk- + 32 位 hex
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("key 应以 sk- 开头: %q", key)
	}
	hexPart := strings.TrimPrefix(key, "sk-")
	if len(hexPart) != 32 {
		t.Fatalf("hex 部分长度应为 32，实际 %d: %q", len(hexPart), key)
	}
	for _, r := range hexPart {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			t.Fatalf("key 含非法 hex 字符: %q", key)
		}
	}
}

func TestGenerateApiKeyUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		key, err := generateApiKey()
		if err != nil {
			t.Fatalf("generateApiKey error: %v", err)
		}
		if seen[key] {
			t.Fatalf("key 重复: %q", key)
		}
		seen[key] = true
	}
}

func TestRandomCredential(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := randomCredential()
		if len(v) < 7 || len(v) > 9 {
			t.Fatalf("randomCredential 长度 %d 应在 7-9: %q", len(v), v)
		}
		for _, r := range v {
			if !strings.ContainsRune(credentialChars, r) {
				t.Fatalf("randomCredential 含非法字符 %q: %q", r, v)
			}
		}
	}
}
