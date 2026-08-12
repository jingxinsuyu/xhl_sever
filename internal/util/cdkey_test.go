package util

import "testing"

func TestGenerateCDKey(t *testing.T) {
	key, err := GenerateCDKey()
	if err != nil {
		t.Fatalf("GenerateCDKey error: %v", err)
	}
	if len(key) != 16 {
		t.Fatalf("cdkey 长度应为 16，得到 %d: %s", len(key), key)
	}
	for _, c := range key {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("cdkey 应只含小写 hex 字符: %s", key)
		}
	}
}

func TestGenerateCDKeysUnique(t *testing.T) {
	keys, err := GenerateCDKeys(1000)
	if err != nil {
		t.Fatalf("GenerateCDKeys error: %v", err)
	}
	if len(keys) != 1000 {
		t.Fatalf("应生成 1000 张，得到 %d", len(keys))
	}
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			t.Fatalf("存在重复卡密: %s", k)
		}
		seen[k] = struct{}{}
	}
}
