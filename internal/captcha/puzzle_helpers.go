// 拼图生成辅助函数：抠缺口遮罩、切拼块（低对比度）、形状判断。
package captcha

import (
	"image"
	"image/color"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

// drawGapMask 在背景缺口处画半透明暗色遮罩（非亮白，机器难检测）
func drawGapMask(canvas *image.RGBA, cx, cy, dir int) {
	pad := 7
	mask := color.RGBA{0, 0, 0, 90}  // 半透明黑
	edge := color.RGBA{0, 0, 0, 160} // 边缘略深（轮廓线）
	for y := cy - PieceSize - pad; y <= cy+PieceSize+pad; y++ {
		for x := cx - PieceSize - pad; x <= cx+PieceSize+pad; x++ {
			if x < 0 || y < 0 || x >= W || y >= H {
				continue
			}
			if inPuzzleShape(float64(x), float64(y), float64(cx), float64(cy), PieceSize, dir) {
				if shapeNeighbor(canvas, x, y, cx, cy, dir) {
					canvas.SetRGBA(x, y, blend(canvas.RGBAAt(x, y), mask))
				} else {
					canvas.SetRGBA(x, y, blend(canvas.RGBAAt(x, y), edge))
				}
			}
		}
	}
}

// blend 按 alpha 混合前景到背景
func blend(bg, fg color.RGBA) color.RGBA {
	a := float64(fg.A) / 255
	r := float64(bg.R)*(1-a) + float64(fg.R)*a
	g := float64(bg.G)*(1-a) + float64(fg.G)*a
	b := float64(bg.B)*(1-a) + float64(fg.B)*a
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// cutPieceLowContrast 从背景切出拼图形状区域，做低对比度处理 + 浅描边。
func cutPieceLowContrast(src image.Image, cx, cy, dir int, rng *rand.Rand) *image.NRGBA {
	pad := 7
	size := 2*PieceSize + 2*pad
	piece := imaging.New(size, size, color.Transparent)

	// 先切出形状区域（原图内容）
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := cx - PieceSize - pad + x
			sy := cy - PieceSize - pad + y
			if sx < 0 || sy < 0 || sx >= W || sy >= H {
				continue
			}
			if inPuzzleShape(float64(sx), float64(sy), float64(cx), float64(cy), PieceSize, dir) {
				piece.Set(x, y, src.At(sx, sy))
			}
		}
	}

	// 低对比度 + 轻微亮度调整，让拼块与背景融合（随机强度）
	ct := float64(rng.Intn(26) - 40) // -40%..-15%
	if ct != 0 {
		piece = imaging.AdjustContrast(piece, ct)
	}
	b := float64(rng.Intn(21) - 10) // -10..+10
	if b != 0 {
		piece = imaging.AdjustBrightness(piece, b)
	}

	// 浅描边：形状边缘用半透明白色细线（不突兀）
	stroke := color.NRGBA{255, 255, 255, 70}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if piece.NRGBAAt(x, y).A == 0 {
				continue
			}
			neighbors := [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}}
			for _, n := range neighbors {
				nx, ny := n[0], n[1]
				if nx < 0 || ny < 0 || nx >= size || ny >= size || piece.NRGBAAt(nx, ny).A == 0 {
					piece.SetNRGBA(x, y, blendNRGBA(piece.NRGBAAt(x, y), stroke))
					break
				}
			}
		}
	}
	return piece
}

// blendNRGBA 混合（用于描边）
func blendNRGBA(bg, fg color.NRGBA) color.NRGBA {
	a := float64(fg.A) / 255
	return color.NRGBA{
		uint8(float64(bg.R)*(1-a) + float64(fg.R)*a),
		uint8(float64(bg.G)*(1-a) + float64(fg.G)*a),
		uint8(float64(bg.B)*(1-a) + float64(fg.B)*a),
		uint8(float64(bg.A)*(1-a) + float64(fg.A)*a),
	}
}

// shapeNeighbor 检查 (x,y) 的 4 邻域是否都在形状内（用于边缘）
func shapeNeighbor(canvas *image.RGBA, x, y, cx, cy, dir int) bool {
	dirs := [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, d := range dirs {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= W || ny >= H {
			return false
		}
		if !inPuzzleShape(float64(nx), float64(ny), float64(cx), float64(cy), PieceSize, dir) {
			return false
		}
	}
	return true
}

// inPuzzleShape 拼图形状（方向随机）：
// 形状为矩形 + 一边凸半圆 + 对边凹半圆。dir 为凸起方向：0=上 1=右 2=下 3=左。
// 通过把点按 dir 旋转，使凸起恒朝上（上凸下凹），再统一判断。
func inPuzzleShape(x, y, cx, cy, r float64, dir int) bool {
	dx, dy := x-cx, y-cy
	switch dir % 4 {
	case 1: // 右凸 → 旋转 90°（顺时针）
		dx, dy = -dy, dx
	case 2: // 下凸 → 旋转 180°
		dx, dy = -dx, -dy
	case 3: // 左凸 → 旋转 270°
		dx, dy = dy, -dx
	}
	px, py := cx+dx, cy+dy

	if px >= cx-r && px <= cx+r && py >= cy-r && py <= cy+r {
		if px >= cx-r*0.45 && px <= cx+r*0.45 && py > cy+r-r*0.45 && py <= cy+r {
			return false
		}
		return true
	}
	if py < cy-r {
		dx2 := px - cx
		dy2 := py - (cy - r)
		if dx2 >= -r*0.45 && dx2 <= r*0.45 && dx2*dx2+dy2*dy2 <= (r*0.45)*(r*0.45) {
			return true
		}
	}
	return false
}

// readPNGs 列出目录下所有 PNG 文件绝对路径
func readPNGs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".PNG") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out, nil
}
