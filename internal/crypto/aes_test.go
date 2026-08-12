package crypto

import "testing"

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
