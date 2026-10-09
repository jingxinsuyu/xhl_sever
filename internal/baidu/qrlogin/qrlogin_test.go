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
		"a b~c*.-_中文":                      "a+b%7Ec*.-_%E4%B8%AD%E6%96%87",
		"c35e28161058c16648886166225c33ad": "c35e28161058c16648886166225c33ad",
		"":                                 "",
		"~":                                "%7E",
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
	// 新算法（空值也参与签名）期望值
	want := "1488a55f726e509f84f0e9e1db19b571"
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
	for _, k := range []string{"client", "cuid", "clientid", "clientfrom", "zid", "appid", "tpl", "app_version", "sdk_version", "sdkversion", "sign", "cmd", "bduss", "sig"} {
		if gotForm.Get(k) == "" {
			t.Fatalf("缺少参数 %s", k)
		}
	}
	if gotForm.Get("sign") != "abc123" || gotForm.Get("bduss") != "bduss123" {
		t.Fatalf("业务参数错误: %v", gotForm)
	}
	// cookie 有真实 STOKEN/PTOKEN → 请求带真实值；没有则空值（纯 BDUSS 也能扫）
	if gotForm.Get("stoken") != "stok456" {
		t.Fatalf("stoken 应传真实值 stok456: %v", gotForm)
	}
	if gotForm.Get("ptoken") != "ptok789" {
		t.Fatalf("ptoken 应传真实值 ptok789: %v", gotForm)
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

// TestConfirmNoToken cookie 只有 BDUSS（无 STOKEN/PTOKEN）→ stoken/ptoken 传空值（纯 BDUSS 扫码）。
func TestConfirmNoToken(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errno":0,"code":"0","message":"ok"}`))
	}))
	defer server.Close()
	sapiURL = server.URL + "/v2/sapi/qrlogin?lp="

	cookie := "BDUSS=bduss123" // 只有 BDUSS
	res, err := Confirm("https://wappass.baidu.com/wp/?qrlogin&sign=abc123&lp=pc", cookie, "")
	if err != nil {
		t.Fatalf("Confirm error: %v", err)
	}
	if !res.OK {
		t.Fatalf("纯 BDUSS 应成功: %+v", res)
	}
	// 无 STOKEN/PTOKEN → 字段存在但为空
	if gotForm.Get("stoken") != "" || gotForm.Get("ptoken") != "" {
		t.Fatalf("无 token 时 stoken/ptoken 应为空: %v", gotForm)
	}
	// sig 自校验（空值参与签名）
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

// TestScanProtectBlocked loginprotect 响应判定（对齐 AlongTyRant账号检测报告.md）：
// 没有 qr 字段 → 保护开启；qr != "0" → 保护开启；qr=="0" → 可扫；code!=110000 → 不拦截。
func TestScanProtectBlocked(t *testing.T) {
	cases := map[string]bool{
		// qr == "0" → 正常可扫
		`{"code":110000,"data":{"protect":{"location":"0","web":"0","username":"0","qr":"0"}}}`: false,
		// 数字形态的 0 同样视为可扫
		`{"code":110000,"data":{"protect":{"qr":0}}}`: false,
		// 没有 qr 字段 → 保护开启
		`{"code":110000,"data":{"protect":{"location":"0","web":"0","username":"0"}}}`: true,
		// qr 非 0 → 保护开启
		`{"code":110000,"data":{"protect":{"qr":"1"}}}`: true,
		`{"code":110000,"data":{"protect":{"qr":1}}}`:   true,
		// code != 110000（凭证无效等）→ 不拦截
		`{"code":-6,"data":{}}`:     false,
		`{"code":110000,"data":{}}`: false, // 无 protect 字段 → 不拦截（无法判定）
		`{"code":110000}`:           false,
		`not json`:                  false,
	}
	for in, want := range cases {
		if got := scanProtectBlocked([]byte(in)); got != want {
			t.Errorf("scanProtectBlocked(%s) = %v, want %v", in, got, want)
		}
	}
}

// TestDetectScanProtect 全流程：httptest 捕获请求参数/头 → 返回保护开启 → 判定 true，
// 并校验请求带 cookie/Referer/UA/gid大写。
func TestDetectScanProtect(t *testing.T) {
	var gotCookie, gotReferer, gotUA, gotGID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotReferer = r.Header.Get("Referer")
		gotUA = r.Header.Get("User-Agent")
		gotGID = r.URL.Query().Get("gid")
		if r.URL.Query().Get("tpl") != "pp" || r.URL.Query().Get("client") != "pc" {
			t.Fatalf("参数不符: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":110000,"data":{"protect":{"location":"0","web":"0","username":"0"}}}`)) // 无 qr → 保护开启
	}))
	defer server.Close()
	loginProtectURL = server.URL

	if !DetectScanProtect("BDUSS=bduss123", "") {
		t.Fatal("无 qr 字段应判定为保护开启")
	}
	if gotCookie != "BDUSS=bduss123" || gotReferer != "https://passport.baidu.com/v3/securitycenter" {
		t.Fatalf("请求头不符: cookie=%q referer=%q", gotCookie, gotReferer)
	}
	if !strings.Contains(gotUA, "Chrome/") {
		t.Fatalf("UA 应为 PC Chrome: %q", gotUA)
	}
	if gotGID != strings.ToUpper(gotGID) || len(gotGID) != 36 {
		t.Fatalf("gid 应为大写 UUID: %q", gotGID)
	}
}

// TestDetectScanProtectOK qr=="0" → 不拦截；cookie 为空 → 不拦截。
func TestDetectScanProtectOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":110000,"data":{"protect":{"qr":"0"}}}`))
	}))
	defer server.Close()
	loginProtectURL = server.URL

	if DetectScanProtect("BDUSS=bduss123", "") {
		t.Fatal("qr==0 不应拦截")
	}
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":110000,"data":{"protect":{"qr":"0"}}}`))
	}))
	defer server2.Close()
	loginProtectURL = server2.URL
	if DetectScanProtect("", "") {
		t.Fatal("空 cookie 不应拦截")
	}
}

// TestDetectScanProtectNetworkError 接口异常（连接失败/非 110000）→ fail-open 不拦截。
func TestDetectScanProtectNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // 连接被拒
	loginProtectURL = url
	if DetectScanProtect("BDUSS=bduss123", "") {
		t.Fatal("连接失败应 fail-open 不拦截")
	}
}

// TestParseSapiRespErrnoMessage errno 已知错误码应映射中文友好提示。
func TestParseSapiRespErrnoMessage(t *testing.T) {
	cases := map[string]string{
		`{"errno":1}`:      "二维码已过期",
		`{"errno":2}`:      "BDUSS 过期",
		`{"errno":3}`:      "用户尚未正常化",
		`{"errno":160102}`: "BDUSS 为空",
		`{"errno":99999}`:  "", // 未知 errno 无映射
		`{"errno":2,"message":"百度自定义"}`: "百度自定义", // 有 message 优先
	}
	for in, want := range cases {
		res, err := parseSapiResp([]byte(in))
		if err != nil {
			t.Fatalf("parseSapiResp(%s) error: %v", in, err)
		}
		if res.OK {
			t.Fatalf("errno!=0 不应成功: %s", in)
		}
		if !strings.Contains(res.Message, want) {
			t.Errorf("parseSapiResp(%s) message = %q, want 含 %q", in, res.Message, want)
		}
	}
}
