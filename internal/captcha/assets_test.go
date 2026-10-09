package captcha

import (
	"math/rand"
	"path/filepath"
	"testing"
)

// TestRepoBackgroundsAvailable 守住一个真实的部署事故：
// 源码没问题、编译也没问题，但如果部署时只拷了可执行文件、漏掉 captcha_assets/backgrounds，
// 滑动验证码（POST /api/xhl/captcha）会在运行期直接报「验证码素材加载失败」。
// 这个测试按仓库结构定位素材目录，缺文件就让 go test 变红。
func TestRepoBackgroundsAvailable(t *testing.T) {
	dir := filepath.Join("..", "..", "captcha_assets", "backgrounds")
	img, err := RandomBackground(dir, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("加载素材目录 %s 失败：%v（部署/打包时必须一起带上 captcha_assets/backgrounds）", dir, err)
	}
	if img == nil {
		t.Fatal("背景图解析结果为空")
	}
	if b := img.Bounds(); b.Dx() != W || b.Dy() != H {
		t.Fatalf("背景图尺寸 %dx%d，期望 %dx%d（生成器会自动缩放，这里只是兜底检查）", b.Dx(), b.Dy(), W, H)
	}
}

// TestRandomBackgroundEmptyDir 记录失败形态：目录空/不存在时返回 error，
// 调用方 handler 会转成「验证码素材加载失败」，所以缺素材不是静默降级。
func TestRandomBackgroundEmptyDir(t *testing.T) {
	img, err := RandomBackground(filepath.Join(t.TempDir(), "not-exist"), rand.New(rand.NewSource(1)))
	if err == nil && img != nil {
		t.Fatal("目录不存在却成功加载了背景图，与预期不符")
	}
}
