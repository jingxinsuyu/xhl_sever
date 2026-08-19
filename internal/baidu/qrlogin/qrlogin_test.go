package qrlogin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestJavaURLEncode 对齐 Python quote_plus(s, safe="*-._").replace("~","%7E")。
func TestJavaURLEncode(t *testing.T) {
	cases := map[string]string{
		"a b~c*.-_中文":            "a+b%7Ec*.-_%E4%B8%AD%E6%96%87",
		"c35e28161058c16648886166225c33ad": "c35e28161058c16648886166225c33ad",
		"":                        "",
		"~":                       "%7E",
	}
	for in, want := range cases {
		if got := javaURLEncode(in); got != want {
			t.Errorf("javaURLEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCalculateSig 已知向量：与 Python qr_scan_confirm.py 算法一致。
func TestCalculateSig(t *testing.T) {
	params := map[string]string{
		"client":      "android",
		"cuid":        "278F9CB3D1B7FD78E9B3CB278E45DB77",
		"clientid":    "278F9CB3D1B7FD78E9B3CB278E45DB77",
		"clientfrom":  "native",
		"zid":         "H4NIE_lSLE3cmpPLj4w1rt7xjJEQjlevVQXcZbR7nDpII0_HQGanLeKNALBhmz9eI2-gIB_j7Gu3ZdHDHyr_zsQ",
		"appid":       "1",
		"tpl":         "tb",
		"app_version": "22.9.1.0",
		"sdk_version": "9.15.0",
		"sdkversion":  "9.15.0",
		"sign":        "c35e28161058c16648886166225c33ad",
		"cmd":         "login",
		"bduss":       "bduss-abc~xyz",
		"stoken":      "",
		"ptoken":      "ptok-1",
	}
	want := "7f8832a1116c51d192cf5606ff032e06"
	if got := calculateSig(params, appSignKey); got != want {
		t.Fatalf("calculateSig = %q, want %q", got, want)
	}
}

// TestParseCookies 解析 cookie 串。
func TestParseCookies(t *testing.T) {
	c := parseCookies("  BDUSS=abc; STOKEN= xyz ;  PTOKEN =qwe; ")
	if c["BDUSS"] != "abc" || c["STOKEN"] != "xyz" || c["PTOKEN"] != "qwe" {
		t.Fatalf("parseCookies = %v", c)
	}
}

// TestParseQRLink 解析二维码链接。
func TestParseQRLink(t *testing.T) {
	sign, lp, err := ParseQRLink("https://wappass.baidu.com/wp/?qrlogin&sign=c35e&lp=pc&tpl=mn")
	if err != nil || sign != "c35e" || lp != "pc" {
		t.Fatalf("ParseQRLink(url) = %q/%q/%v", sign, lp, err)
	}
	sign, lp, err = ParseQRLink("sign=c35e")
	if err != nil || sign != "c35e" || lp != "app" {
		t.Fatalf("ParseQRLink(q) = %q/%q/%v", sign, lp, err)
	}
	if _, _, err := ParseQRLink("https://wappass.baidu.com/wp/?qrlogin&lp=pc"); err != ErrNoSign {
		t.Fatalf("缺 sign 应返回 ErrNoSign, got %v", err)
	}
}

// TestConfirm 全流程：httptest 捕获请求 → 校验 16 参数 + sig + 设备 UA → 返回 success。
func TestConfirm(t *testing.T) {
	var gotForm url.Values
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errno":0,"code":"0","message":"ok"}`))
	}))
	defer server.Close()
	sapiURL = server.URL + "/v2/sapi/qrlogin?lp="

	cookie := "BDUSS=bduss123; STOKEN=stok456; PTOKEN=ptok789"
	res, err := Confirm("https://wappass.baidu.com/wp/?qrlogin&sign=abc123&lp=pc", cookie, "")
	if err != nil {
		t.Fatalf("Confirm error: %v", err)
	}
	if !res.OK {
		t.Fatalf("Confirm 应成功: %+v", res)
	}
	// UA 必须来自设备指纹（随机手机型号，含 tieba 后缀），而非硬编码
	if !strings.Contains(gotUA, "Android") || !strings.Contains(gotUA, "tieba/") {
		t.Fatalf("UA 不是手机设备指纹: %q", gotUA)
	}
	// 公共参数
	for _, k := range []string{"client", "cuid", "clientid", "clientfrom", "zid", "appid", "tpl", "app_version", "sdk_version", "sdkversion", "sign", "cmd", "bduss", "stoken", "ptoken", "sig"} {
		if gotForm.Get(k) == "" {
			t.Fatalf("缺少参数 %s", k)
		}
	}
	if gotForm.Get("sign") != "abc123" || gotForm.Get("bduss") != "bduss123" || gotForm.Get("stoken") != "stok456" || gotForm.Get("ptoken") != "ptok789" {
		t.Fatalf("业务参数错误: %v", gotForm)
	}
	// sig 自校验：对收到的参数（排除 sig 本身）重算应一致
	recalc := make(map[string]string)
	for k := range gotForm {
		if k == "sig" {
			continue
		}
		recalc[k] = gotForm.Get(k)
	}
	if gotForm.Get("sig") != calculateSig(recalc, appSignKey) {
		t.Fatalf("sig 与算法不一致")
	}
}

// TestConfirmFailure 服务器返回失败 errno → Result.OK=false。
func TestConfirmFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errno":400031,"code":"400031","error_msg":"登录失败"}`))
	}))
	defer server.Close()
	sapiURL = server.URL + "/v2/sapi/qrlogin?lp="

	res, err := Confirm("https://wappass.baidu.com/wp/?qrlogin&sign=abc123&lp=pc", "BDUSS=bduss123; STOKEN=s; PTOKEN=p", "")
	if err != nil {
		t.Fatalf("Confirm error: %v", err)
	}
	if res.OK {
		t.Fatal("应失败却成功")
	}
	if res.Errno != "400031" {
		t.Fatalf("errno = %q", res.Errno)
	}
}

// TestConfirmNetworkErrorFriendly 连接失败时返回友好错误，不暴露内部 net/http 细节。
func TestConfirmNetworkErrorFriendly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // 地址已释放 → 连接被拒
	sapiURL = url + "/v2/sapi/qrlogin?lp="

	_, err := Confirm("https://wappass.baidu.com/wp/?qrlogin&sign=abc&lp=pc", "BDUSS=x; STOKEN=s; PTOKEN=p", "")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if err.Error() != "连接百度服务器失败，请稍后尝试" {
		t.Fatalf("应返回友好错误信息, got %q", err.Error())
	}
}

// TestConfirmNoBDUSS cookie 缺 BDUSS → 不报错但失败。
func TestConfirmNoBDUSS(t *testing.T) {
	res, err := Confirm("https://wappass.baidu.com/wp/?qrlogin&sign=abc123", "STOKEN=s", "")
	if err != nil {
		t.Fatalf("Confirm error: %v", err)
	}
	if res.OK {
		t.Fatal("缺 BDUSS 不应成功")
	}
}

// TestParseSapiResp code 字符串/数字两种形态。
func TestParseSapiResp(t *testing.T) {
	res, err := parseSapiResp([]byte(`{"code":"0","errno":0}`))
	if err != nil || !res.OK {
		t.Fatalf("code=0 应成功: %+v err=%v", res, err)
	}
	res, err = parseSapiResp([]byte(`{"errno":0,"message":"ok"}`))
	if err != nil || !res.OK {
		t.Fatalf("errno=0 应成功: %+v err=%v", res, err)
	}
	res, err = parseSapiResp([]byte(`{"errno":400031}`))
	if err != nil || res.OK {
		t.Fatalf("errno!=0 应失败: %+v err=%v", res, err)
	}
}
