package handler

import "testing"

func TestNormalizeProxy(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:8080":          "http://1.2.3.4:8080",
		"http://1.2.3.4:8080":    "http://1.2.3.4:8080",
		"1.2.3.4:8080:u:p":       "http://u:p@1.2.3.4:8080",
		"  ":                     "",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeProxy(in); got != want {
			t.Errorf("normalizeProxy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	// \n 换行
	if got := firstLine("\n\n1.2.3.4:8080\n2.3.4.5:9090\n"); got != "1.2.3.4:8080" {
		t.Fatalf("firstLine(\\n) = %q", got)
	}
	// \r\n 换行(TrimSpace 剥掉 \r)
	if got := firstLine("\r\n\r\n1.2.3.4:8080\r\n2.3.4.5:9090\r\n"); got != "1.2.3.4:8080" {
		t.Fatalf("firstLine(\\r\\n) = %q", got)
	}
	// 混合换行 + 前后空白
	if got := firstLine(" \r\n 6.6.6.6:8080 \r\n 7.7.7.7:9090 \n"); got != "6.6.6.6:8080" {
		t.Fatalf("firstLine(混合) = %q", got)
	}
	if got := firstLine("  \n \r\n"); got != "" {
		t.Fatalf("空内容应返回空: %q", got)
	}
}
