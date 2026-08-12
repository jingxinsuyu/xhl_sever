package crypto

import (
	"crypto/aes"
	"encoding/base64"
	"errors"
)

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
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	bs := block.BlockSize()
	if len(b2) == 0 || len(b2)%bs != 0 {
		return "", errors.New("ciphertext length invalid")
	}
	dec := make([]byte, len(b2))
	for i := 0; i < len(b2); i += bs {
		block.Decrypt(dec[i:], b2[i:])
	}
	padding := int(dec[len(dec)-1])
	if padding < 1 || padding > bs {
		return "", errors.New("invalid padding")
	}
	return string(dec[:len(dec)-padding]), nil
}
