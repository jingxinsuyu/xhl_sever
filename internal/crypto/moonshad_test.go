package crypto

import "testing"

func TestMoonshad(t *testing.T) {
	v := Moonshad("testuser")
	if v == "" {
		t.Fatal("Moonshad 返回空串")
	}
	// 结果应为 md5 十六进制（32 位）经替换得到
	if len(v) < 32 {
		t.Fatalf("Moonshad 结果过短: %s", v)
	}
	// 同输入应稳定
	if Moonshad("testuser") != v {
		t.Fatal("Moonshad 结果不稳定")
	}
}

func TestMoonshadV3Structure(t *testing.T) {
	data := map[string]interface{}{
		"kw":     "测试",
		"page":   1,
		"active": true,
	}
	res, err := NewMoonshadV3(data, 750, 1334).Get()
	if err != nil {
		t.Fatalf("MoonshadV3.Get error: %v", err)
	}
	if res["alg"] != "v3" {
		t.Errorf("alg 应为 v3: %v", res["alg"])
	}
	if _, ok := res["time"].(int); !ok {
		t.Errorf("time 应为 int: %v", res["time"])
	}
	if s, _ := res["sig"].(string); s == "" {
		t.Error("sig 不应为空")
	}
	if s, _ := res["shaOne"].(string); len(s) != 40 {
		t.Errorf("shaOne 应为 40 位: %q", s)
	}
}

func TestMoonshadV4Structure(t *testing.T) {
	data := map[string]interface{}{
		"kw": "测试",
	}
	res, err := NewMoonshadV4(data, 750, 1334, "Mozilla/5.0").Get()
	if err != nil {
		t.Fatalf("MoonshadV4.Get error: %v", err)
	}
	if res["alg"] != "v4" {
		t.Errorf("alg 应为 v4: %v", res["alg"])
	}
	if _, ok := res["time"].(int); !ok {
		t.Errorf("time 应为 int: %v", res["time"])
	}
	if s, _ := res["sig"].(string); s == "" {
		t.Error("sig 不应为空")
	}
	if s, _ := res["bdkm"].(string); len(s) != 32 {
		t.Errorf("bdkm 应为 32 位: %q", s)
	}
}
