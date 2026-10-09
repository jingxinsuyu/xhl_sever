package tieba

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeBaidu 起一个假的百度：三步都指到它，可按需让某一步失败。
func fakeBaidu(t *testing.T, failStep string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, "upload:"+string(body))
		if r.Header.Get("Cookie") == "" {
			t.Error("上传图片没带 Cookie")
		}
		if failStep == "upload" {
			_ = json.NewEncoder(w).Encode(map[string]any{"err_no": 1, "err_msg": "图片不合法"})
			return
		}
		// 校验 file 是 base64 且能还原
		v, _ := url.ParseQuery(string(body))
		raw, err := base64.StdEncoding.DecodeString(v.Get("file"))
		if err != nil || len(raw) == 0 {
			t.Errorf("file 不是合法 base64：%v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"err_no": 0,
			"info":   map[string]any{"pic_id": "PID123", "pic_id_encode": "ENC123", "pic_url_no_auth": "http://img/x.jpg"},
		})
	})
	mux.HandleFunc("/meta", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, "meta:"+string(body))
		if failStep == "meta" {
			_ = json.NewEncoder(w).Encode(map[string]any{"no": 1, "error": "meta 不合法"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"no": 0, "data": map[string]any{"resource_link": "https://res/link1"}})
	})
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, "submit:"+string(body))
		if failStep == "submit" {
			_ = json.NewEncoder(w).Encode(map[string]any{"no": 4, "error": "登录失效"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"no": 0})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seen
}

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient("BDUSS=abc; SToken=def; BAIDUID=xyz", "")
	c.UploadURL = srv.URL + "/upload"
	c.MetaURL = srv.URL + "/meta"
	c.SubmitURL = srv.URL + "/submit"
	return c
}

func TestSetFigureSuccess(t *testing.T) {
	srv, seen := fakeBaidu(t, "")
	c := newTestClient(srv)
	res, err := c.SetFigure(context.Background(), []byte("fakepng-bytes"), DefaultMeta)
	if err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	if res.PicID != "PID123" || res.PicIDEncode != "ENC123" {
		t.Fatalf("返回的 pic_id 不对: %+v", res)
	}
	if res.FigureURL != "http://tiebapic.baidu.com/figure/pic/item/ENC123.jpg" {
		t.Fatalf("形象图地址不对: %s", res.FigureURL)
	}
	if res.ResourceLink != "https://res/link1" {
		t.Fatalf("resource_link 不对: %s", res.ResourceLink)
	}
	if len(*seen) != 3 {
		t.Fatalf("应该正好调用三次，实际 %d 次", len(*seen))
	}
	// 第三步必须把第二步拿到的 link 和第一步的 pic_id 带上
	if !strings.Contains((*seen)[2], "meta_value=https%3A%2F%2Fres%2Flink1") ||
		!strings.Contains((*seen)[2], "figure_pid=PID123") {
		t.Fatalf("提交参数不对: %s", (*seen)[2])
	}
}

func TestSetFigureStepFailures(t *testing.T) {
	for _, step := range []string{"upload", "meta", "submit"} {
		srv, _ := fakeBaidu(t, step)
		c := newTestClient(srv)
		if _, err := c.SetFigure(context.Background(), []byte("x"), DefaultMeta); err == nil {
			t.Fatalf("%s 步骤失败时应该返回错误", step)
		}
	}
}

func TestHasCreds(t *testing.T) {
	cases := []struct {
		cookie string
		ok     bool
		miss   string
	}{
		{"BDUSS=a; SToken=b", true, ""},
		{"BDUSS=a; STOKEN=b", true, ""}, // 大写也认
		{"bduss=a; stoken=b", true, ""}, // 小写也认
		{"SToken=b; BAIDUID=x", false, "BDUSS"},
		{"BDUSS=a; BAIDUID=x", false, "SToken"},
		{"BAIDUID=x", false, "BDUSS、SToken"},
	}
	for _, c := range cases {
		ok, miss := HasCreds(c.cookie)
		if ok != c.ok || miss != c.miss {
			t.Errorf("HasCreds(%q) = (%v,%q)，期望 (%v,%q)", c.cookie, ok, miss, c.ok, c.miss)
		}
	}
}

func TestNormalizeProxy(t *testing.T) {
	for _, in := range []string{"1.2.3.4:8080", "http://1.2.3.4:8080", "1.2.3.4:8080:user:pass"} {
		if _, err := normalizeProxyURL(in); err != nil {
			t.Errorf("解析代理 %q 失败: %v", in, err)
		}
	}
	u, _ := normalizeProxyURL("1.2.3.4:8080:user:pass")
	if u.User == nil || u.User.Username() != "user" || u.Host != "1.2.3.4:8080" {
		t.Errorf("host:port:user:pass 解析结果不对: %v", u)
	}
}
