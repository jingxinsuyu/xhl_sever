package crypto

import (
	"crypto/aes"
	"encoding/base64"
	"errors"
)

// ================================================================
// 登录密码加解密（服务自身）：
// AES-ECB PKCS7 + 双重 base64，客户端用 AesEncryptDouble64 加密。
// ================================================================

// AesEncryptDouble64 AES-ECB PKCS7 加密 + 双重 base64。
// 客户端登录密码加密使用，与服务端 AesDecryptDouble64 对应。
func AesEncryptDouble64(data, key string) (string, error) {
	return aesEncryptDouble64(data, key)
}

// AesDecryptDouble64 逆 AesEncryptDouble64：双重 base64 解码 + AES-ECB PKCS7 解密。
func AesDecryptDouble64(data, key string) (string, error) {
	b1, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	b2, err := base64.StdEncoding.DecodeString(string(b1))
	if err != nil {
		return "", err
	}
	return aesECBDecryptPKCS7(b2, key)
}

// aesEncryptDouble64 AES-ECB PKCS#7 加密，双重 base64（原本在 moonshad.go，随登录解密保留迁移至此）。
func aesEncryptDouble64(data, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	src := []byte(data)
	bs := block.BlockSize()
	padding := bs - (len(src) % bs)
	padded := make([]byte, len(src)+padding)
	copy(padded, src)
	for i := len(src); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(encrypted[i:], padded[i:])
	}

	b64 := base64.StdEncoding.EncodeToString(encrypted)
	return base64.StdEncoding.EncodeToString([]byte(b64)), nil
}

// ================================================================
// AES-ECB 原语（服务自身）：
// 单层 base64 编码，供 baidu/device 的 generateFuid 与 qrlogin data 解密使用。
// ================================================================

// AesECBEncryptPKCS7 AES-ECB PKCS#7 加密，输出 base64（单层）。
// 语义与客户端 baidu/crypto 的 AesECBEncryptPKCS7 完全一致。
func AesECBEncryptPKCS7(plaintext, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	data := []byte(plaintext)
	bs := block.BlockSize()
	padding := bs - (len(data) % bs)
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(encrypted[i:], padded[i:])
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// AesECBDecryptPKCS7Base64 base64 解码 → AES-ECB PKCS7 解密（单层 base64 版本）。
// 用于 qrlogin 的 data 解密（前端「AES 加密后 base64 一次」）。
func AesECBDecryptPKCS7Base64(data, key string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	return aesECBDecryptPKCS7(ct, key)
}

// aesECBDecryptPKCS7 AES-ECB 解密 + 去除 PKCS7 填充。
func aesECBDecryptPKCS7(ct []byte, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	bs := block.BlockSize()
	if len(ct) == 0 || len(ct)%bs != 0 {
		return "", errors.New("ciphertext length invalid")
	}
	dec := make([]byte, len(ct))
	for i := 0; i < len(ct); i += bs {
		block.Decrypt(dec[i:], ct[i:])
	}
	padding := int(dec[len(dec)-1])
	if padding < 1 || padding > bs {
		return "", errors.New("invalid padding")
	}
	// 校验填充字节一致
	padBytes := dec[len(dec)-padding:]
	for _, b := range padBytes {
		if int(b) != padding {
			return "", errors.New("invalid padding")
		}
	}
	return string(dec[:len(dec)-padding]), nil
}
