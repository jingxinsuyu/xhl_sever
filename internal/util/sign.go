package util

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strings"
)

// SignParams 构造客户端 sign 的规范字符串：
// 参数按键名 ASCII 升序排序，拼接为 key1=value1&key2=value2...，末尾追加 &key=<salt>。
// 客户端必须用相同的参数与盐构造出完全一致的字符串。
func SignParams(params map[string]string, salt string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	b.WriteString("&key=")
	b.WriteString(salt)
	return b.String()
}

// VerifyClientSign 校验客户端 sign：md5(SignParams(params, salt)) 与 sign 一致。
// sign 为 32 位小写十六进制。
func VerifyClientSign(params map[string]string, salt, sign string) bool {
	if sign == "" {
		return false
	}
	sum := md5.Sum([]byte(SignParams(params, salt)))
	return strings.EqualFold(fmt.Sprintf("%x", sum), strings.TrimSpace(sign))
}
