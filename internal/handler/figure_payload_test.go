package handler

import "testing"

// TestFigureUserPayloadNoImageInfo 用户端成功返回:只有成败/计费字段,不能含图片信息。
// 需求:客户端日志「设置成功就行,不要返回图片信息」。真机联调脚本(_verify_figure.py)也断言了这点。
func TestFigureUserPayloadNoImageInfo(t *testing.T) {
	p := figureUserPayload("per_call", 1, 4)
	want := map[string]bool{"ok": true, "billing_mode": true, "cost": true, "remaining": true, "hint": true}
	for k := range p {
		if !want[k] {
			t.Errorf("返回里多了不该有的字段:%q", k)
		}
	}
	for k := range want {
		if _, ok := p[k]; !ok {
			t.Errorf("返回里缺字段:%q", k)
		}
	}
	for _, banned := range []string{"pic_id", "figure_url", "pic_url", "image_url", "url"} {
		if _, ok := p[banned]; ok {
			t.Errorf("不能返回图片信息,却出现了 %q", banned)
		}
	}
	if p["ok"] != true {
		t.Errorf("ok 应为 true,实际 %v", p["ok"])
	}
	if p["remaining"] != 4 || p["cost"] != 1 || p["billing_mode"] != "per_call" {
		t.Errorf("字段值不对:%v", p)
	}
}

// TestFigureOpenPayloadNoImageInfo 开放平台成功返回:同样不含图片信息。
func TestFigureOpenPayloadNoImageInfo(t *testing.T) {
	p := figureOpenPayload(1, 99)
	want := map[string]bool{"ok": true, "cost": true, "balance": true, "hint": true}
	for k := range p {
		if !want[k] {
			t.Errorf("返回里多了不该有的字段:%q", k)
		}
	}
	for _, banned := range []string{"pic_id", "figure_url", "pic_url", "image_url"} {
		if _, ok := p[banned]; ok {
			t.Errorf("不能返回图片信息,却出现了 %q", banned)
		}
	}
}
