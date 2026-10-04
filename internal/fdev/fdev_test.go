package fdev

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// 9 个已知向量（与 libfire.so 模拟结果逐位一致）——见 INTEGRATION_PROMPT.md 验收 2
var knownVectors = map[string]string{
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

// TestFdevVectors 逐位校验 9 个已知向量
func TestFdevVectors(t *testing.T) {
	for dev, want := range knownVectors {
		got, err := Fdev(dev)
		if err != nil {
			t.Fatalf("Fdev(%s) 出错: %v", dev, err)
		}
		if h := hex.EncodeToString(got); h != want {
			t.Errorf("Fdev(%s) = %s, want %s", dev, h, want)
		}
	}
}

// TestSelfCheck SelfCheck() 必须全过
func TestSelfCheck(t *testing.T) {
	if err := SelfCheck(); err != nil {
		t.Fatalf("SelfCheck 失败: %v", err)
	}
}

// TestFdevInvalid 非法输入必须报错（长度非 32）
func TestFdevInvalid(t *testing.T) {
	for _, bad := range []string{"", "abc", strings.Repeat("a", 31), strings.Repeat("a", 33)} {
		if _, err := Fdev(bad); err == nil {
			t.Errorf("Fdev(%q) 期望报错，实际通过", bad)
		}
	}
}

// TestXYUSAndXDev 设备身份派生
func TestXYUSAndXDev(t *testing.T) {
	d := Device{AndroidID: "0123456789abcdef", UUID: "12345678-0000-4000-8000-000000000000"}
	xyus := d.XYUS()
	if len(xyus) != 34 || !strings.HasSuffix(xyus, "|0") {
		t.Fatalf("xyus 形态不对: %q (len=%d)", xyus, len(xyus))
	}
	sum := md5.Sum([]byte(d.AndroidID + d.UUID))
	if want := strings.ToUpper(hex.EncodeToString(sum[:])) + "|0"; xyus != want {
		t.Errorf("XYUS = %q, want %q", xyus, want)
	}
	xdev := XDev(xyus)
	if len(xdev) != 32 || xdev != strings.ToLower(xdev) {
		t.Errorf("XDev 应为 32 位小写 hex，得到 %q", xdev)
	}
	if want := md5.Sum([]byte(xyus)); hex.EncodeToString(want[:]) != xdev {
		t.Errorf("XDev 与 MD5(xyus) 不一致")
	}
}

// TestBuildRequest 请求包结构与关键不变量
func TestBuildRequest(t *testing.T) {
	d := Device{AndroidID: "0123456789abcdef", UUID: "12345678-0000-4000-8000-000000000000"}
	req, err := BuildRequest(d)
	if err != nil {
		t.Fatalf("BuildRequest 出错: %v", err)
	}

	if !strings.HasPrefix(req.URL, "https://sofire.baidu.com/c/11/z/100/"+AppKey+"/") {
		t.Errorf("URL 前缀不对: %s", req.URL)
	}
	// skey 末尾的 "\n" 必须保留（urlencode 后是 %0A）
	if !strings.Contains(req.URL, "%0A") {
		t.Errorf("URL 里缺少 skey 末尾换行的 urlencode(%%0A): %s", req.URL)
	}
	if len(req.FB) != 16 {
		t.Errorf("FB 应为 16 字节，得到 %d", len(req.FB))
	}
	// FB 必须是 f(xdev)
	if want, _ := Fdev(req.XDev); !bytes.Equal(want, req.FB) {
		t.Errorf("FB 与 f(XDev) 不一致")
	}
	if req.XDev != XDev(d.XYUS()) {
		t.Errorf("XDev 不匹配")
	}
	if len(req.Body) <= 16 {
		t.Errorf("body 太短: %d", len(req.Body))
	}
	// body = 密文 + MD5(gz) 的 16 字节尾部；整体应是 (16 的倍数) + 16
	if len(req.Body)%16 != 0 {
		t.Errorf("body 长度 %d 不是 16 的倍数（密文+16 字节 md5）", len(req.Body))
	}
	raw, err := base64.StdEncoding.DecodeString(req.BodyB64)
	if err != nil || !bytes.Equal(raw, req.Body) {
		t.Errorf("BodyB64 与 Body 不一致: err=%v", err)
	}
	for _, k := range []string{"User-Agent", "Content-Type", "x-device-id", "x-sdk-ver", "x-api-ver", "x-app-ver", "x-plu-ver"} {
		if req.Headers[k] == "" {
			t.Errorf("缺少请求头 %s", k)
		}
	}
	if req.Headers["x-device-id"] != req.XDev {
		t.Errorf("x-device-id 应为 XDev")
	}
	if req.Headers["User-Agent"] != UserAgent || req.Headers["x-api-ver"] != XApiVer {
		t.Errorf("请求头常量与约定不符")
	}
}

// TestBuildRequestXYUSInvalid 非法 xyus
func TestBuildRequestXYUSInvalid(t *testing.T) {
	for _, bad := range []string{"", "abc", strings.Repeat("A", 32), strings.Repeat("A", 32) + "|1"} {
		if _, err := BuildRequestXYUS(bad); err == nil {
			t.Errorf("BuildRequestXYUS(%q) 期望报错", bad)
		}
	}
}

// buildFakeResponse 用与实现相同的原语构造一个"服务端响应"，用于验证解密路径。
// 结构：{"data": base64(AES-CBC(gzip(plain))), "skey": base64(rkey XOR fb)}
func buildFakeResponse(t *testing.T, fb []byte, plain string) []byte {
	t.Helper()
	rkey := make([]byte, 16)
	if _, err := rand.Read(rkey); err != nil {
		t.Fatalf("rand: %v", err)
	}
	gz, err := gzipBytes([]byte(plain))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	ct, err := aesCBCEncrypt(gz, rkey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	xored := make([]byte, 16)
	for i := 0; i < 16; i++ {
		xored[i] = rkey[i] ^ fb[i]
	}
	env := map[string]string{
		"data": base64.StdEncoding.EncodeToString(ct),
		"skey": base64.StdEncoding.EncodeToString(xored),
	}
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// mkToken 造一个 87 字符 base64url（解码 65 字节）的 token
func mkToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 65)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	if len(s) != 87 {
		t.Fatalf("token 长度 %d，期望 87", len(s))
	}
	return s
}

// TestOpenResponseRoundTrip 解密往返：单 JSON、双 JSON 拼接两种情况
func TestOpenResponseRoundTrip(t *testing.T) {
	dev := "580661e9742914a02d2a809e34121c90"
	fb, err := Fdev(dev)
	if err != nil {
		t.Fatalf("Fdev: %v", err)
	}
	tok := mkToken(t)

	cases := []struct {
		name  string
		plain string
	}{
		{"单个 JSON", `{"token":"` + tok + `","st":"56","nt":1800}`},
		{"两个 JSON 拼接（需合并）", `{"a":1}{"token":"` + tok + `","st":"56","nt":1800}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := buildFakeResponse(t, fb, c.plain)
			got, err := OpenResponse(resp, fb)
			if err != nil {
				t.Fatalf("OpenResponse: %v", err)
			}
			if got.Token != tok {
				t.Errorf("token 不匹配")
			}
			if got.ST != "56" {
				t.Errorf("st = %q, want 56", got.ST)
			}
			if got.NT != 1800 {
				t.Errorf("nt = %d, want 1800", got.NT)
			}
			if !got.Valid() {
				t.Errorf("Valid() 应为 true")
			}
			if got.TokenBytes() != 65 {
				t.Errorf("TokenBytes = %d, want 65", got.TokenBytes())
			}
		})
	}
}

// TestOpenResponseBadInput 错误输入
func TestOpenResponseBadInput(t *testing.T) {
	if _, err := OpenResponse([]byte(`{}`), make([]byte, 15)); err == nil {
		t.Error("fb 长度非 16 应报错")
	}
	if _, err := OpenResponse([]byte("not-json"), make([]byte, 16)); err == nil {
		t.Error("非 JSON 响应应报错")
	}
}

// TestLiveIssue 联网验收：真去 sofire 签一次，要求 st=56 且 token 解码 65 字节。
// 默认跳过：-short 跳过；或未显式设置 FDEV_LIVE=1 也跳过（避免 CI/部署误打外部接口）。
// 需要真打时：FDEV_LIVE=1 go test ./internal/fdev -run TestLiveIssue -v
// 走代理：额外设置 FDEV_PROXY=http://user:pass@host:port
func TestLiveIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过联网验收")
	}
	if os.Getenv("FDEV_LIVE") != "1" {
		t.Skip("未设置 FDEV_LIVE=1，跳过联网验收")
	}

	d := Device{
		AndroidID: "0123456789abcdef",
		UUID:      "12345678-0000-4000-8000-000000000000",
	}
	req, err := BuildRequest(d)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	tr := &http.Transport{}
	if p := os.Getenv("FDEV_PROXY"); p != "" {
		pu, err := url.Parse(p)
		if err != nil {
			t.Fatalf("代理地址无效: %v", err)
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: tr}

	httpReq, err := http.NewRequest(http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("请求 sofire 失败: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, head(body, 160))
	}

	tok, err := OpenResponse(body, req.FB)
	if err != nil {
		t.Fatalf("OpenResponse: %v", err)
	}
	t.Logf("st=%q nt=%d token=%d 字符 / %d 字节", tok.ST, tok.NT, len(tok.Token), tok.TokenBytes())
	if !tok.Valid() {
		t.Fatalf("验收失败: st=%q tokenBytes=%d（期望 st=56 且 65 字节）", tok.ST, tok.TokenBytes())
	}
}
