// Package fdev —— 百度 sofire c/11/z 的设备凭证（z_id）签名算法，纯标准库实现
//
// 逆向来源：libfire.so 的 `ar(key, dev)`（原实现靠 Unicorn 模拟 ARM 代码，
// 这里已还原为等价算法）。**本文件不联网、不依赖任何第三方库**，直接拷进你的项目即可。
//
// 一句话算法：f(dev) = RC4(key = dev 的 32 个 ASCII 字节).keystream(前 16 字节) ⊕ 0x2A
//
// 典型用法（服务端）：
//
//	req, err := fdev.BuildRequest(fdev.Device{AndroidID: "…", UUID: "…"})
//	// req.URL / req.Body / req.Headers 交给**客户端**去发（这样签发出口是客户端的代理）
//	// req.FB 只留在服务端，用来解密响应
//	...
//	tok, err := fdev.OpenResponse(respBody, req.FB)   // 拿 z_id
//
// 安全提醒：`FB`（= f(dev)）是设备凭证派生结果，别下发到客户端；
// 客户端只需要 URL + Body + Headers 就能完成签发，响应解密也交回服务端即可。
package fdev

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"strings"
	"time"
)

// ------------------------------------------------------------------ 常量

const (
	AppKey  = "200012" // 可信签发路径的 app_key（200033 那条路的 token 图片会被过滤）
	Str4    = "116592c67c79b83d38f2d4f263c86fc2"
	URLTmpl = "https://sofire.baidu.com/c/11/z/100/%s/%s/%s"

	UserAgent = "x6/200012/15.71.0.10/5.0.9.5"
	XSdkVer   = "sofire/3.7.1.7"
	XPluVer   = "x6/5.0.9.5"
	XAppVer   = "com.baidu.searchbox/15.71.0.10"
	XApiVer   = "32"

	Pkg    = "com.baidu.searchbox"
	AppVer = "15.71.0.10"
	X6Ver  = "5.0.9.5"

	XorMask = 0x2A // PRGA 输出再异或的固定值
)

const randAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const randAlnum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// ------------------------------------------------------------------ f(dev)

// Fdev 计算 f(dev)：输入 32 位 hex 设备号，输出 16 字节（原 libfire.so 的 ar(0, dev)）
func Fdev(devHex string) ([]byte, error) {
	if len(devHex) != 32 {
		return nil, fmt.Errorf("fdev: dev 必须是 32 位 hex，收到 %d 位", len(devHex))
	}
	key := []byte(devHex)

	var s [256]byte
	for i := 0; i < 256; i++ {
		s[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ { // RC4 KSA
		j = (j + int(s[i]) + int(key[i%len(key)])) & 0xFF
		s[i], s[j] = s[j], s[i]
	}

	out := make([]byte, 16) // 初值 0（对应 JNI NewByteArray 的零初始化）
	i, k := 0, 0
	for n := 0; n < 16; n++ { // RC4 PRGA，取前 16 字节再异或 0x2A
		i = (i + 1) & 0xFF
		k = (k + int(s[i])) & 0xFF
		s[i], s[k] = s[k], s[i]
		out[n] = s[(int(s[i])+int(s[k]))&0xFF] ^ out[n] ^ XorMask
	}
	return out, nil
}

// ------------------------------------------------------------------ 设备身份

// Device 一台设备的身份（客户端指纹里的这两个字段即可）
type Device struct {
	AndroidID string
	UUID      string
}

// XYUS = MD5(android_id + uuid).upper() + "|0"
func (d Device) XYUS() string {
	sum := md5.Sum([]byte(d.AndroidID + d.UUID))
	return strings.ToUpper(hex.EncodeToString(sum[:])) + "|0"
}

// XDev = MD5(xyus)（小写 hex），既进入 f(dev)，也作为请求头 x-device-id 明文发给服务端
func XDev(xyus string) string {
	sum := md5.Sum([]byte(xyus))
	return hex.EncodeToString(sum[:])
}

// ------------------------------------------------------------------ 请求包

// Request 一次签发的完整请求（客户端拿 URL/Body/Headers 去发；FB 只留服务端）
type Request struct {
	URL     string            `json:"url"`
	Body    []byte            `json:"-"` // 密文 body（POST 的原始字节）
	BodyB64 string            `json:"body_b64"`
	Headers map[string]string `json:"headers"`
	XYUS    string            `json:"-"`
	XDev    string            `json:"x_dev,omitempty"`
	FB      []byte            `json:"-"` // f(dev)：解密响应用，**不要下发给客户端**
	Issued  time.Time         `json:"issued_at"`
}

// BuildRequest 用设备身份生成请求包
func BuildRequest(d Device) (*Request, error) { return BuildRequestXYUS(d.XYUS()) }

// BuildRequestXYUS 用 xyus（已算好的设备串）生成请求包
func BuildRequestXYUS(xyus string) (*Request, error) {
	if !strings.HasSuffix(xyus, "|0") || len(xyus) != 34 {
		return nil, fmt.Errorf("fdev: xyus 形如 32位大写hex + \"|0\"，收到 %q", xyus)
	}
	xdev := XDev(xyus)
	fb, err := Fdev(xdev)
	if err != nil {
		return nil, err
	}

	nowMs := time.Now().UnixMilli()
	devID := strings.TrimSuffix(xyus, "|0")
	rmf := fmt.Sprintf("001%d02%011d", nowMs, rand.Int63n(100_000_000_000))
	cuid := devID + "|" + randFrom(randAlnum, 8)

	plain := plainBody{
		F1: "", F2: Pkg, F3: AppVer,
		F4: xyus, F5: "fEb7KjpY", F6: nowMs, F7: "",
		F8: AppKey, F9: "x6", F10: X6Ver, F11: "", F12: "",
		F13: 1, F14: 1,
		Mod: []moduleSection{{
			Token: devID + "04", UT: devID + "04", Magic: "", TokenRT: "",
			MZ: xyus, DS: "2", Zid: xyus, ActSt: "0", ChnSt: "0", OsVer: "32",
			Reason: reason{A: 1, B: 0, C: nowMs, D: 3},
			TP:     "3", TK: "#", PD: "8649", LRC: "0", CC: "", LRE: "", IPO: "1",
			RMF: rmf, T1: "114", T2: "79", T3: "", T4: "0",
			F15091: "1", F15082: fmt.Sprintf("%s#%d#1#0", rmf, nowMs), F15083: "",
			F15112: rmf + "###0", F15006: "1", D0006: "4", F15085: "{}",
			Isj: "0", Ise: "1", Cuid: cuid,
		}},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(plain); err != nil {
		return nil, err
	}
	raw := bytes.TrimRight(buf.Bytes(), "\n") // Python 是紧凑 JSON；这里少一个换行而已，服务端按 JSON 解析

	gz, err := gzipBytes(raw)
	if err != nil {
		return nil, err
	}
	key := []byte(randFrom(randAlphabet, 16))
	ct, err := aesCBCEncrypt(gz, key)
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(gz)

	body := make([]byte, 0, len(ct)+16)
	body = append(body, ct...)
	body = append(body, sum[:]...)

	xored := make([]byte, 16)
	for i := 0; i < 16; i++ {
		xored[i] = fb[i] ^ key[i]
	}
	skey := base64.StdEncoding.EncodeToString(xored)

	ts := fmt.Sprintf("%d", time.Now().Unix())
	md5s := fmt.Sprintf("%x", md5.Sum([]byte(AppKey+ts+Str4)))
	// ⚠️ skey 后面那个 "\n" 是原实现的，别省（urlencode 后是 %0A）
	fullURL := fmt.Sprintf(URLTmpl, AppKey, ts, md5s) + "?" + url.QueryEscape("skey") + "=" + url.QueryEscape(skey+"\n")

	return &Request{
		URL:  fullURL,
		Body: body,
		Headers: map[string]string{
			"User-Agent":       UserAgent,
			"Pragma":           "no-cache",
			"Accept":           "*/*",
			"Content-Type":     "application/x-www-form-urlencoded; charset=utf-8",
			"Accept-Language":  "zh",
			"x-sdk-ver":        XSdkVer,
			"x-plu-ver":        XPluVer,
			"x-app-ver":        XAppVer,
			"x-device-id":      xdev,
			"x-api-ver":        XApiVer,
			"Accept-Encoding":  "gzip",
		},
		BodyB64: base64.StdEncoding.EncodeToString(body),
		XYUS:    xyus,
		XDev:    xdev,
		FB:      fb,
		Issued:  time.Now(),
	}, nil
}

// ------------------------------------------------------------------ 响应解密

// Token 解密后的签发结果
type Token struct {
	Token      string         `json:"token"`      // 87 字符 base64url（65 字节）
	ST         string         `json:"st"`         // "56" = 审核通过
	NT         int            `json:"nt"`         // 有效期（秒），实测 1800
	Raw        map[string]any `json:"raw,omitempty"`
}

// Valid 是否可用
func (t *Token) Valid() bool { return t.ST == "56" && t.TokenBytes() == 65 }

// TokenBytes token 的原始字节数（应为 65）
func (t *Token) TokenBytes() int {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(t.Token, "="))
	if err != nil {
		return 0
	}
	return len(b)
}

// OpenResponse 解密 sofire 的响应（需要生成请求时的 FB）
//
//	⚠️ 客户端拿不到 FB，所以这一步要么在服务端做，要么由服务端只把 rkey 算出来回给客户端。
func OpenResponse(respBody []byte, fb []byte) (*Token, error) {
	if len(fb) != 16 {
		return nil, fmt.Errorf("fdev: fb 必须是 16 字节")
	}
	if len(respBody) > 2 && respBody[0] == 0x1f && respBody[1] == 0x8b { // 万一响应是 gzip
		if gz, err := gzip.NewReader(bytes.NewReader(respBody)); err == nil {
			if raw, err := io.ReadAll(gz); err == nil {
				respBody = raw
			}
		}
	}

	envelope, err := firstJSON(respBody)
	if err != nil {
		return nil, fmt.Errorf("fdev: 响应不是 JSON：%w（原文 %q）", err, head(respBody, 120))
	}
	dataB64, _ := envelope["data"].(string)
	skeyB64, _ := envelope["skey"].(string)
	if dataB64 == "" || skeyB64 == "" {
		return nil, fmt.Errorf("fdev: 响应缺少 data/skey：%v", keysOf(envelope))
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("fdev: data base64 解码失败：%w", err)
	}
	rskey, err := base64.StdEncoding.DecodeString(skeyB64)
	if err != nil {
		return nil, fmt.Errorf("fdev: skey base64 解码失败：%w", err)
	}
	if len(rskey) < 16 {
		return nil, fmt.Errorf("fdev: 响应 skey 长度异常 %d", len(rskey))
	}
	rkey := make([]byte, 16)
	for i := 0; i < 16; i++ {
		rkey[i] = rskey[i] ^ fb[i]
	}

	plain, err := aesCBCDecrypt(data, rkey)
	if err != nil {
		return nil, err
	}
	if gz, err := gzip.NewReader(bytes.NewReader(plain)); err == nil { // 明文可能被 gzip 过
		if raw, err := io.ReadAll(gz); err == nil {
			plain = raw
		}
	}

	// 明文里偶尔会拼两个 JSON 对象 → 逐个解、合并
	merged := map[string]any{}
	rest := plain
	for {
		i := bytes.IndexByte(rest, '{')
		if i < 0 {
			break
		}
		obj, err := firstJSON(rest[i:])
		if err != nil {
			break
		}
		for k, v := range obj {
			merged[k] = v
		}
		dec := json.NewDecoder(bytes.NewReader(rest[i:]))
		var skip map[string]any
		if err := dec.Decode(&skip); err != nil {
			break
		}
		consumed := int(dec.InputOffset())
		if consumed <= 0 || i+consumed >= len(rest) {
			break
		}
		rest = rest[i+consumed:]
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("fdev: 明文里没有 JSON：%q", head(plain, 120))
	}

	tok := &Token{Raw: merged}
	tok.Token, _ = merged["token"].(string)
	tok.ST, _ = merged["st"].(string)
	switch v := merged["nt"].(type) {
	case float64:
		tok.NT = int(v)
	case string:
		fmt.Sscanf(v, "%d", &tok.NT)
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("fdev: 响应里没有 token（st=%q nt=%d）", tok.ST, tok.NT)
	}
	return tok, nil
}

// ------------------------------------------------------------------ 自检

// vectors 是从原实现里抠出来的已知向量（libfire.so 模拟结果），用于回归
var vectors = map[string]string{
	"580661e9742914a02d2a809e34121c90": "16912530d0d1b71f98b17eb34e0bd7fc",
	"e7f73cfb8a2791ddf0b5a1947262e04a": "4216518e405517828e3ee89b498dcbb5",
	"00000000000000000000000000000000": "a2558caae951fe0d1ed5c85edb4c7490",
	"11111111111111111111111111111111": "4a30e847bd6dc0585e163797604d8ade",
	"ffffffffffffffffffffffffffffffff": "7ae22fdb436f7a10c383d459b3c52548",
	"00000000000000000000000000000001": "113d055129bd2fb640b78d59d5d2a9fb",
	"10000000000000000000000000000000": "1cc218c8a735d0a3ba1964384500cf71",
	"e51df8e594d787656a3bc7bd13396b06": "ef68b005ca787c9a5fa2fe088b9b0338",
	"a9ed27e28f3b117da7b53f6512f080c9": "9f1903fbdb4bd22c8d7433a6b3dbf094",
}

// SelfCheck 跑一遍已知向量，确认算法没被动过；返回错误说明哪个向量不对
func SelfCheck() error {
	bad := 0
	for dev, want := range vectors {
		got, err := Fdev(dev)
		if err != nil {
			return err
		}
		if hex.EncodeToString(got) != want {
			bad++
			fmt.Printf("  [FAIL] f(%s) = %s，期望 %s\n", dev, hex.EncodeToString(got), want)
		}
	}
	if bad > 0 {
		return fmt.Errorf("fdev: %d/%d 个向量不匹配", bad, len(vectors))
	}
	return nil
}

// ------------------------------------------------------------------ 内部工具

type reason struct {
	A int   `json:"1"`
	B int   `json:"2"`
	C int64 `json:"3"`
	D int   `json:"4"`
}

type moduleSection struct {
	Token   string `json:"token"`
	UT      string `json:"ut"`
	Magic   string `json:"magic"`
	TokenRT string `json:"token_rt"`
	MZ      string `json:"mz"`
	DS      string `json:"ds"`
	Zid     string `json:"zid"`
	ActSt   string `json:"act_st"`
	ChnSt   string `json:"chn_st"`
	OsVer   string `json:"os_ver"`
	Reason  reason `json:"reason"`
	TP      string `json:"tp"`
	TK      string `json:"tk"`
	PD      string `json:"pd"`
	LRC     string `json:"lrc"`
	CC      string `json:"cc"`
	LRE     string `json:"lre"`
	IPO     string `json:"ipo"`
	RMF     string `json:"rmf"`
	T1      string `json:"t1"`
	T2      string `json:"t2"`
	T3      string `json:"t3"`
	T4      string `json:"t4"`
	F15091  string `json:"15091"`
	F15082  string `json:"15082"`
	F15083  string `json:"15083"`
	F15112  string `json:"15112"`
	F15006  string `json:"15006"`
	D0006   string `json:"d0006"`
	F15085  string `json:"15085"`
	Isj     string `json:"isj"`
	Ise     string `json:"ise"`
	Cuid    string `json:"cuid"`
}

type plainBody struct {
	F1  string          `json:"1"`
	F2  string          `json:"2"`
	F3  string          `json:"3"`
	F4  string          `json:"4"`
	F5  string          `json:"5"`
	F6  int64           `json:"6"`
	F7  string          `json:"7"`
	F8  string          `json:"8"`
	F9  string          `json:"9"`
	F10 string          `json:"10"`
	F11 string          `json:"11"`
	F12 string          `json:"12"`
	F13 int             `json:"13"`
	F14 int             `json:"14"`
	Mod []moduleSection `json:"module_section"`
}

func randFrom(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

func gzipBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, 6)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pkcs7Pad / unpad —— 与 Python 的 padding.PKCS7(128) 等价
func pkcs7Pad(data []byte) []byte {
	n := 16 - len(data)%16
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	n := int(data[len(data)-1])
	if n < 1 || n > 16 || n > len(data) {
		return data
	}
	return data[:len(data)-n]
}

// aesCBCEncrypt —— AES-128-CBC + 零 IV + PKCS7（与现实现逐字节一致）
func aesCBCEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, padded)
	return out, nil
}

func aesCBCDecrypt(ct, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct)%aes.BlockSize != 0 || len(ct) == 0 {
		return nil, fmt.Errorf("fdev: 密文长度 %d 不是 16 的倍数", len(ct))
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, ct)
	return pkcs7Unpad(out), nil
}

func firstJSON(b []byte) (map[string]any, error) {
	i := bytes.IndexByte(b, '{')
	if i < 0 {
		return nil, fmt.Errorf("没有 {")
	}
	dec := json.NewDecoder(bytes.NewReader(b[i:]))
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func head(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
