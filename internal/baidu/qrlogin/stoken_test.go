package qrlogin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestFetchStokens 全流程：httptest 捕获 form → 校验 sig 自洽 + bduss/tpl_list/UA → 返回 stoken_list。
func TestFetchStokens(t *testing.T) {
	var gotForm url.Values
	var gotUA, gotReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		gotUA = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"errno":0,"errmsg":"Auth Login Sucess","stoken_list":{"netdisk":"nd-abcdef","tb":"tb-xyz","pp":"pp-1"}}`))
	}))
	defer server.Close()
	authURL = server.URL

	res, err := FetchStokens("BDUSS-abc123", "PTOK-xyz", "")
	if err != nil {
		t.Fatalf("FetchStokens error: %v", err)
	}
	if !res.OK {
		t.Fatalf("应成功: %+v", res)
	}
	if res.Stokens["netdisk"] != "nd-abcdef" || res.Stokens["tb"] != "tb-xyz" {
		t.Fatalf("stoken_list 解析错误: %v", res.Stokens)
	}
	if gotUA != authUA || gotReferer != "https://passport.baidu.com/" {
		t.Fatalf("请求头不符: UA=%q referer=%q", gotUA, gotReferer)
	}
	if gotForm.Get("bduss") != "BDUSS-abc123" || gotForm.Get("ptoken") != "PTOK-xyz" {
		t.Fatalf("参数错误: %v", gotForm)
	}
	if gotForm.Get("tpl") != "bs_andr" {
		t.Fatalf("tpl 应为 bs_andr: %v", gotForm)
	}
	// sig 自校验：对收到的参数（排除 sig）重算应一致（bs_andr key，空值也参与）
	recalc := make(map[string]string)
	for k := range gotForm {
		if k == "sig" {
			continue
		}
		recalc[k] = gotForm.Get(k)
	}
	if gotForm.Get("sig") != calculateSig(recalc, bsAndrSignKey) {
		t.Fatal("sig 与算法不一致")
	}
	if !strings.Contains(gotForm.Get("tpl_list"), "netdisk") {
		t.Fatalf("tpl_list 应含 netdisk: %q", gotForm.Get("tpl_list"))
	}
}

// TestFetchStokensNoPtoken 无 PTOKEN → ptoken 传空值参与签名。
func TestFetchStokensNoPtoken(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"errno":0,"stoken_list":{"netdisk":"nd"}}`))
	}))
	defer server.Close()
	authURL = server.URL

	res, err := FetchStokens("BDUSS-x", "", "")
	if err != nil || !res.OK {
		t.Fatalf("应成功: %+v err=%v", res, err)
	}
	if gotForm.Get("ptoken") != "" {
		t.Fatalf("无 ptoken 应传空: %v", gotForm)
	}
}

// TestFetchStokensErrno errno!=0 → OK=false 带 errno/errmsg。
func TestFetchStokensErrno(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":400021,"errno":400021,"errmsg":"BDUSS 无效"}`))
	}))
	defer server.Close()
	authURL = server.URL

	res, err := FetchStokens("BDUSS-bad", "", "")
	if err != nil {
		t.Fatalf("errno!=0 不应返回网络错误: %v", err)
	}
	if res.OK {
		t.Fatal("应失败却成功")
	}
	if res.Errno != "400021" {
		t.Fatalf("errno = %q", res.Errno)
	}
}

// TestFetchStokensNoBDUSS bduss 为空 → 报错。
func TestFetchStokensNoBDUSS(t *testing.T) {
	if _, err := FetchStokens(" ", "", ""); err != ErrStokenNoBDUSS {
		t.Fatalf("空 BDUSS 应返回 ErrStokenNoBDUSS, got %v", err)
	}
}

// TestFetchStokensNetworkError 连接失败 → 友好错误。
func TestFetchStokensNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close()
	authURL = url
	if _, err := FetchStokens("BDUSS-x", "", ""); err == nil {
		t.Fatal("应返回错误")
	}
}
