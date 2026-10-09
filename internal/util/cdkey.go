package util

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
)

// GenerateCDKey 生成单个卡密：16 位小写 MD5（对随机 16 字节做 md5 后取前 16 位 hex）
func GenerateCDKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	sum := md5.Sum(buf)
	return hex.EncodeToString(sum[:])[:16], nil
}

// GenerateCDKeys 批量生成指定数量、互不重复的卡密
func GenerateCDKeys(count int) ([]string, error) {
	keys := make([]string, 0, count)
	seen := make(map[string]struct{}, count)
	for len(keys) < count {
		key, err := GenerateCDKey()
		if err != nil {
			return nil, err
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys, nil
}
