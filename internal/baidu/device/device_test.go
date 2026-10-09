package device

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var cuidRe = regexp.MustCompile(`^[0-9A-F]{32}$`)
var uuidRe = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

func checkDevice(t *testing.T, d *Device, width, height int) {
	t.Helper()
	if d == nil {
		t.Fatal("device 为 nil")
	}
	if !cuidRe.MatchString(d.Cuid) {
		t.Fatalf("cuid 格式非法: %q", d.Cuid)
	}
	if d.Fuid == "" {
		t.Fatal("fuid 为空")
	}
	if !uuidRe.MatchString(d.Gid) {
		t.Fatalf("gid 格式非法: %q", d.Gid)
	}
	if len(d.LogTraceID) != 60 {
		t.Fatalf("log_trace_id 长度非法: %d", len(d.LogTraceID))
	}
	if len(d.PassID) != 4 {
		t.Fatalf("pass_id 长度非法: %q", d.PassID)
	}
	if d.Width != width || d.Height != height {
		t.Fatalf("宽高非法: %dx%d want %dx%d", d.Width, d.Height, width, height)
	}
	// rinfo.fuid = md5(fuid)
	if d.Rinfo["fuid"] == "" {
		t.Fatal("rinfo.fuid 为空")
	}

	// --- sofire zid 链路 ---
	// xyus = MD5(uuid).upper() + "|0"（32 hex + "|0" = 34 字符）
	if len(d.Xyus) != 34 || !strings.HasSuffix(d.Xyus, "|0") {
		t.Fatalf("xyus 格式非法: %q", d.Xyus)
	}
	if _, err := hex.DecodeString(d.Xyus[:32]); err != nil {
		t.Fatalf("xyus 前半不是 hex: %v", err)
	}
	// xyusec = base64
	if _, err := base64.StdEncoding.DecodeString(d.Xyusec); err != nil {
		t.Fatalf("xyusec 不是合法 base64: %v", err)
	}
	// zid30 = 30 字节 hex 大写
	if len(d.Zid30) != 60 {
		t.Fatalf("zid30 长度非法: %d", len(d.Zid30))
	}
	if raw, err := hex.DecodeString(d.Zid30); err != nil || len(raw) != 30 {
		t.Fatalf("zid30 不是 30 字节 hex: len=%d err=%v", len(raw), err)
	}
	// zid65 = base64url（RawURLEncoding），解码后 65 字节
	if raw, err := base64.RawURLEncoding.DecodeString(d.Zid65); err != nil || len(raw) != 65 {
		t.Fatalf("zid65 不是 65 字节 base64url: len=%d err=%v", len(raw), err)
	}
	// zid65 含 "/" 分隔
	if raw, _ := base64.RawURLEncoding.DecodeString(d.Zid65); string(raw[32]) != "/" {
		t.Fatalf("zid65 缺少 seg1/seg2 分隔符: %q", d.Zid65)
	}
}

// profileByUA 按 UA 查找手机型号预设（测试用）。
func profileByUA(ua string) *phoneProfile {
	for i := range phoneProfiles {
		if phoneProfiles[i].ua == ua {
			return &phoneProfiles[i]
		}
	}
	return nil
}

func TestNew(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	// UA 必须是内置手机型号之一，宽高/GPU 与该机型一致
	profile := profileByUA(d.Info.UserAgent)
	if profile == nil {
		t.Fatalf("UA 不属于内置手机型号: %s", d.Info.UserAgent)
	}
	checkDevice(t, d, profile.width, profile.height)
	if d.Info.WebglVendorAndRenderer != profile.gpu {
		t.Fatalf("GPU 与机型不匹配: %s != %s", d.Info.WebglVendorAndRenderer, profile.gpu)
	}
	if d.Info.ScreenResolution != fmt.Sprintf("%d,%d", profile.width, profile.height) {
		t.Fatalf("分辨率与机型不匹配: %s", d.Info.ScreenResolution)
	}
}

func TestNewWin(t *testing.T) {
	d, err := NewWin()
	if err != nil {
		t.Fatalf("NewWin error: %v", err)
	}
	checkDevice(t, d, 1920, 1040)
	if d.Info.Platform != "Win32" {
		t.Fatalf("Windows 平台标识非法: %q", d.Info.Platform)
	}
}

// TestZid30Tail 校验 30B zid 尾部结构: [a][0x97][0xA4][b][0x0F][c][0x6A][0x84]。
func TestZid30Tail(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	raw, err := hex.DecodeString(d.Zid30)
	if err != nil {
		t.Fatalf("decode zid30 error: %v", err)
	}
	tail := raw[22:]
	if tail[1] != 0x97 || tail[2] != 0xA4 || tail[4] != 0x0F || tail[6] != 0x6A || tail[7] != 0x84 {
		t.Fatalf("zid30 尾部结构非法: %x", tail)
	}
}

// TestGenZid65StableTemplate 固定 seg1 模板时 zid65 前缀稳定（seg1 不变）。
func TestGenZid65StableTemplate(t *testing.T) {
	z1, _ := genZid65("CUID1", "")
	z2, _ := genZid65("CUID2", "")
	if z1 == "" || z2 == "" {
		t.Fatal("genZid65 返回空")
	}
	r1, _ := base64.RawURLEncoding.DecodeString(z1)
	r2, _ := base64.RawURLEncoding.DecodeString(z2)
	if string(r1[:32]) != string(r2[:32]) {
		t.Fatal("seg1 模板应稳定不变")
	}
	if string(r1[33:]) == string(r2[33:]) {
		t.Fatal("seg2 应按 cuid/时间变化")
	}
}
