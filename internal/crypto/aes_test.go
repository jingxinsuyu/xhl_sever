package crypto

import (
	"encoding/base64"
	"testing"
)

func TestAesRoundtrip(t *testing.T) {
	key := "xhl-client-key16"
	cases := []string{"pass123", "123456", "a", "", "hello world with spaces and 中文"}
	for _, plain := range cases {
		enc, err := AesEncryptDouble64(plain, key)
		if err != nil {
			t.Fatalf("encrypt %q error: %v", plain, err)
		}
		dec, err := AesDecryptDouble64(enc, key)
		if err != nil {
			t.Fatalf("decrypt %q error: %v", plain, err)
		}
		if dec != plain {
			t.Fatalf("roundtrip mismatch: got %q want %q", dec, plain)
		}
	}
}

func TestAesDecryptWrongKey(t *testing.T) {
	enc, err := AesEncryptDouble64("pass123", "xhl-client-key16")
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	if _, err := AesDecryptDouble64(enc, "1111111111111111"); err == nil {
		t.Fatal("用错误密钥解密应失败")
	}
}

// TestAesECBRoundtrip AES-ECB 单层 base64 加解密 round-trip（device generateFuid / qrlogin data 用）。
func TestAesECBRoundtrip(t *testing.T) {
	key := "FfdsnvsootJmvNfl" // 与 baidu device generateFuid 相同
	cases := []string{"hello", "", "zhong文与特殊字符 !@#$", `{"a":1,"b":"中文"}`}
	for _, plain := range cases {
		enc, err := AesECBEncryptPKCS7(plain, key)
		if err != nil {
			t.Fatalf("encrypt %q error: %v", plain, err)
		}
		dec, err := AesECBDecryptPKCS7Base64(enc, key)
		if err != nil {
			t.Fatalf("decrypt %q error: %v", plain, err)
		}
		if dec != plain {
			t.Fatalf("roundtrip mismatch: got %q want %q", dec, plain)
		}
	}
}

// TestAesECBOutputBase64 AES-ECB 输出必须是单层 base64（能直接解码为密文字节）。
func TestAesECBOutputBase64(t *testing.T) {
	enc, err := AesECBEncryptPKCS7("data-to-encrypt-12345", "FfdsnvsootJmvNfl")
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	// 单层 base64：解码后应为 16 的倍数（AES 块大小）
	ct, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("输出不是合法 base64: %v", err)
	}
	if len(ct)%16 != 0 {
		t.Fatalf("密文长度 %d 不是 16 的倍数", len(ct))
	}
}

// TestAesECBCompatDouble64 qrlogin data 兼容旧双重 base64：单层解密失败后可回退双重。
func TestAesECBCompatDouble64(t *testing.T) {
	key := "czgrFN1A5f6Vfina"
	plain := "user----pass----cookie"
	encDouble, err := AesEncryptDouble64(plain, key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	// 单层解密应失败（内容是双重 base64 外壳）
	if _, err := AesECBDecryptPKCS7Base64(encDouble, key); err == nil {
		t.Fatal("双重 base64 应无法被单层解密，当前返回成功")
	}
	// 双重 base64 回退应还原明文
	dec, err := AesDecryptDouble64(encDouble, key)
	if err != nil {
		t.Fatalf("double64 decrypt error: %v", err)
	}
	if dec != plain {
		t.Fatalf("mismatch: got %q want %q", dec, plain)
	}
}
