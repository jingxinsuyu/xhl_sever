// Package captcha 滑动拼图验证码生成（防机器识别）：
// 在背景图上抠一个拼图形状缺口（半透明暗色遮罩），拼图块从背景对应位置切出
// （低对比度 + 浅描边 + 随机凸起方向），供扫码接口每 25 次触发一次验证。
package captcha

import (
	"image"
	"image/draw"
	"math/rand"

	"github.com/disintegration/imaging"
)

// 画布尺寸与拼图块尺寸
const (
	W = 250
	H = 150
	// PieceSize 拼图块半宽
	PieceSize = 20
)

// Result 一次生成的验证码
type Result struct {
	BG     *image.RGBA  // 背景图（带真缺口 + 干扰缺口遮罩）
	Piece  *image.NRGBA // 拼图块（透明，低对比度 + 描边，来自真缺口位置）
	GapX   int          // 真缺口中心 X（答案）
	GapY   int          // 真缺口中心 Y
	Dir    int          // 真缺口凸起方向：0=上 1=右 2=下 3=左
	DecoyX int          // 干扰缺口中心 X
	DecoyY int          // 干扰缺口中心 Y
}

// Generate 生成滑动拼图验证码：1 个真缺口（切拼图块）+ 1 个干扰缺口（仅遮罩，位置不重叠）。
// bg 为已缩放到 250x150 的背景图；rng 为随机源。
func Generate(bg image.Image, rng *rand.Rand) *Result {
	canvas := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(canvas, canvas.Bounds(), bg, image.Point{}, draw.Src)

	// 真缺口：随机位置 + 方向
	gapX := PieceSize + 12 + rng.Intn(W-2*(PieceSize+12))
	gapY := PieceSize + 12 + rng.Intn(H-2*(PieceSize+12))
	gapDir := rng.Intn(4)
	// 干扰缺口：随机位置（与真缺口保持间距），独立方向
	decoyX, decoyY, decoyDir := randomDecoy(rng, gapX, gapY)

	// 画真缺口遮罩 + 切拼图块
	drawGapMask(canvas, gapX, gapY, gapDir)
	piece := cutPieceLowContrast(bg, gapX, gapY, gapDir, rng)
	// 画干扰缺口遮罩（不切拼块）
	drawGapMask(canvas, decoyX, decoyY, decoyDir)

	return &Result{
		BG: canvas, Piece: piece,
		GapX: gapX, GapY: gapY, Dir: gapDir,
		DecoyX: decoyX, DecoyY: decoyY,
	}
}

// randomDecoy 生成与真缺口间距 ≥ minGap 的随机干扰缺口位置。
func randomDecoy(rng *rand.Rand, gapX, gapY int) (x, y, dir int) {
	const minGap = 70
	for i := 0; i < 100; i++ {
		x = PieceSize + 12 + rng.Intn(W-2*(PieceSize+12))
		y = PieceSize + 12 + rng.Intn(H-2*(PieceSize+12))
		dx, dy := x-gapX, y-gapY
		if dx*dx+dy*dy >= minGap*minGap {
			return x, y, rng.Intn(4)
		}
	}
	// 保底：放在对角位置
	return W - gapX, H - gapY, rng.Intn(4)
}

// RandomBackground 从背景素材目录随机加载一张并缩放到 250x150。
func RandomBackground(dir string, rng *rand.Rand) (image.Image, error) {
	entries, err := readPNGs(dir)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	img, err := imaging.Open(entries[rng.Intn(len(entries))])
	if err != nil {
		return nil, err
	}
	return imaging.Resize(img, W, H, imaging.Lanczos), nil
}
