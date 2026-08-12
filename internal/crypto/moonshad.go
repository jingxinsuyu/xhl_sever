package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var dmap = map[byte]string{
	'a': "3", 'b': "4", 'c': "5", 'd': "9", 'e': "8", 'f': "7",
	'g': "1", 'h': "2", 'i': "6", 'j': "0", 'k': "a", 'l': "b",
	'm': "c", 'n': "d", 'o': "e", 'p': "f", 'q': "g", 'r': "z",
	's': "y", 't': "x", 'u': "w", 'v': "v", 'w': "u", 'x': "o",
	'y': "p", 'z': "q",
	'0': "s", '1': "t", '2': "r", '3': "h", '4': "i",
	'5': "j", '6': "k", '7': "l", '8': "m", '9': "n",
}

const (
	v3Key      = "moonshad5moonsh2"
	v4OuterKey = "moonshad5moonsh2"
)

// ================================================================
// 工具函数
// ================================================================

// aesEncryptDouble64 AES-ECB PKCS#7，双重 base64。
func aesEncryptDouble64(data, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	src := []byte(data)
	bs := block.BlockSize()
	padding := bs - (len(src) % bs)
	padded := make([]byte, len(src)+padding)
	copy(padded, src)
	for i := len(src); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(encrypted[i:], padded[i:])
	}

	b64 := base64.StdEncoding.EncodeToString(encrypted)
	return base64.StdEncoding.EncodeToString([]byte(b64)), nil
}

func mixStr(a, b string) string {
	var res strings.Builder
	i := 0
	for i < len(a) && i < len(b) {
		res.WriteByte(a[i])
		res.WriteByte(b[i])
		i++
	}
	res.WriteString(a[i:])
	res.WriteString(b[i:])
	return res.String()
}

func dataSortToStr(data map[string]interface{}) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := data[k]
		switch val := v.(type) {
		case nil:
			parts = append(parts, k+"=null")
		case bool:
			if val {
				parts = append(parts, k+"=true")
			} else {
				parts = append(parts, k+"=false")
			}
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		}
	}
	return strings.Join(parts, "&")
}

func dmapStr(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if repl, ok := dmap[s[i]]; ok {
			b.WriteString(repl)
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func generateCanvasKey() string {
	// 模拟浏览器 canvas 指纹，每次调用生成不同的值
	img := image.NewRGBA(image.Rect(0, 0, 280, 60))

	// 白色背景
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// 文字
	text1 := "百度一下,你就知道 !@#$%^&*()_+{}:?><~`,./;'[]"
	drawText(img, text1, 2, 14, color.Black)

	// 橙色矩形
	orange := color.RGBA{255, 102, 0, 255}
	for y := range 11 {
		for x := range 21 {
			img.Set(100+x, 3+y, orange)
		}
	}

	// 文字
	drawText(img, "201407154183", 5, 42, color.Black)

	// 随机噪点
	for range rand.Intn(8) + 3 {
		px := rand.Intn(280)
		py := rand.Intn(60)
		img.Set(px, py, color.RGBA{
			uint8(rand.Intn(256)),
			uint8(rand.Intn(256)),
			uint8(rand.Intn(256)),
			255,
		})
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func drawText(img *image.RGBA, text string, x, y int, c color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

// getShaOne 与参考实现一致:sha1(md5(当前毫秒时间戳))。
// 百度校验 shaOne 与返回的 time/elapsed 对应,不能用与时间无关的值。
func getShaOne() (string, int64) {
	now := time.Now().UnixMilli()
	md5Hex := fmt.Sprintf("%x", md5.Sum([]byte(strconv.FormatInt(now, 10))))
	return fmt.Sprintf("%x", sha1.Sum([]byte(md5Hex))), now
}

// ================================================================
// Moonshad
// ================================================================

// Moonshad 计算 moonshad 值。
func Moonshad(username string) string {
	h := fmt.Sprintf("%x", md5.Sum([]byte(username+"Moonshadow")))
	h = strings.Replace(h, "o", "ow", 1)
	h = strings.Replace(h, "d", "do", 1)
	h = strings.Replace(h, "a", "ad", 1)
	h = strings.Replace(h, "h", "ha", 1)
	h = strings.Replace(h, "s", "sh", 1)
	h = strings.Replace(h, "n", "ns", 1)
	h = strings.Replace(h, "m", "mo", 1)
	return h
}

// ================================================================
// MoonshadV3
// ================================================================

// MoonshadV3 v3 加密。
type MoonshadV3 struct {
	data       map[string]interface{}
	wh         string
	shaOne     string
	shaOneTime int64
	key        string // 签名使用的 AES key，默认 v3Key，可通过 WithKey 覆盖
}

// NewMoonshadV3 创建 v3 实例，默认使用 v3Key 签名。
func NewMoonshadV3(data map[string]interface{}, width, height int) *MoonshadV3 {
	d := make(map[string]interface{})
	for k, v := range data {
		d[k] = v
	}
	shaOneVal, shaOneTime := getShaOne()

	m := &MoonshadV3{
		data:       d,
		wh:         fmt.Sprintf("%d%d", width, height),
		shaOne:     shaOneVal,
		shaOneTime: shaOneTime,
		key:        v3Key,
	}

	m.data["alg"] = "v3"
	m.data["time"] = int(time.Now().Unix())
	delete(m.data, "sig")
	delete(m.data, "traceid")
	delete(m.data, "callback")
	delete(m.data, "elapsed")
	delete(m.data, "shaOne")

	return m
}

// WithKey 覆盖 v3 签名的 AES key。
func (m *MoonshadV3) WithKey(key string) *MoonshadV3 {
	m.key = key
	return m
}

func (m *MoonshadV3) getSig() (string, error) {
	sortedStr := dataSortToStr(m.data)
	md5Hex := fmt.Sprintf("%x", md5.Sum([]byte(sortedStr)))
	h := dmapStr(m.wh)
	mixed := mixStr(md5Hex, h)
	return aesEncryptDouble64(mixed, m.key)
}

// Get 返回 v3 签名结果。
func (m *MoonshadV3) Get() (map[string]interface{}, error) {
	sig, err := m.getSig()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"time":    m.data["time"],
		"alg":     "v3",
		"sig":     sig,
		"elapsed": time.Now().UnixMilli() - m.shaOneTime,
		"shaOne":  m.shaOne,
	}, nil
}

// ================================================================
// MoonshadV4
// ================================================================

// MoonshadV4 v4 加密。
type MoonshadV4 struct {
	data       map[string]interface{}
	wh         string
	ua         string
	bdkm       string
	shaOne     string
	shaOneTime int64
}

// NewMoonshadV4 创建 v4 实例。
func NewMoonshadV4(data map[string]interface{}, width, height int, ua string) *MoonshadV4 {
	d := make(map[string]interface{})
	for k, v := range data {
		d[k] = v
	}
	shaOneVal, shaOneTime := getShaOne()

	m := &MoonshadV4{
		data:       d,
		wh:         fmt.Sprintf("%d%d", width, height),
		ua:         ua,
		shaOne:     shaOneVal,
		shaOneTime: shaOneTime,
	}

	m.data["alg"] = "v4"
	m.data["time"] = int(time.Now().Unix())
	delete(m.data, "sig")
	delete(m.data, "traceid")
	delete(m.data, "callback")

	// bdkm = MD5(JSON.stringify(data))
	jsonBytes, _ := json.Marshal(m.data)
	m.bdkm = fmt.Sprintf("%x", md5.Sum(jsonBytes))
	m.data["bdkm"] = m.bdkm

	return m
}

func (m *MoonshadV4) getSig() string {
	sortedStr := dataSortToStr(m.data)
	md5Hex := fmt.Sprintf("%x", md5.Sum([]byte(sortedStr)))
	h := dmapStr(m.wh)
	return mixStr(md5Hex, h)
}

// Get 返回 v4 签名结果。
func (m *MoonshadV4) Get() (map[string]interface{}, error) {
	sig := m.getSig()
	payload := map[string]interface{}{
		"sig": sig,
		"addData": map[string]interface{}{
			"elapsed":   time.Now().UnixMilli() - m.shaOneTime,
			"shaOne":    m.shaOne,
			"ua":        m.ua,
			"canvasKey": generateCanvasKey(),
		},
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	encrypted, err := aesEncryptDouble64(string(jsonBytes), v4OuterKey)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"time": m.data["time"],
		"alg":  "v4",
		"sig":  encrypted,
		"bdkm": m.bdkm,
	}, nil
}
