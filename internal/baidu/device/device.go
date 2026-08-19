package device

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	mrand "math/rand"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/crypto"
)

// ================================================================
// DeviceInfo 浏览器/设备指纹信息
// ================================================================

const defaultUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 15_4 like Mac OS X) " +
	"AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/145.0.7632.55 Mobile/15E148 Safari/604.1"

// const defaultUA = "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Mobile Safari/537.36"

// DeviceInfo 设备指纹信息。
type DeviceInfo struct {
	UserAgent                 string `json:"userAgent"`
	Canvas                    string `json:"canvas"`
	Language                  string `json:"language"`
	ColorDepth                string `json:"colorDepth"`
	DeviceMemory              string `json:"deviceMemory"`
	HardwareConcurrency       string `json:"hardwareConcurrency"`
	ScreenResolution          string `json:"screenResolution"`
	AvailableScreenResolution string `json:"availableScreenResolution"`
	TimezoneOffset            string `json:"timezoneOffset"`
	Timezone                  string `json:"timezone"`
	SessionStorage            string `json:"sessionStorage"`
	LocalStorage              string `json:"localStorage"`
	IndexedDb                 string `json:"indexedDb"`
	AddBehavior               string `json:"addBehavior"`
	OpenDatabase              string `json:"openDatabase"`
	CpuClass                  string `json:"cpuClass"`
	Platform                  string `json:"platform"`
	Plugins                   string `json:"plugins"`
	Webgl                     string `json:"webgl"`
	WebglVendorAndRenderer    string `json:"webglVendorAndRenderer"`
	AdBlock                   string `json:"adBlock"`
	HasLiedLanguages          string `json:"hasLiedLanguages"`
	HasLiedResolution         string `json:"hasLiedResolution"`
	HasLiedOs                 string `json:"hasLiedOs"`
	HasLiedBrowser            string `json:"hasLiedBrowser"`
	TouchSupport              string `json:"touchSupport"`
	Fonts                     string `json:"fonts"`
	Audio                     string `json:"audio"`
}

// DefaultDeviceInfo 返回默认设备指纹。
func DefaultDeviceInfo() DeviceInfo {
	return DeviceInfo{
		UserAgent:                 defaultUA,
		Language:                  "zh-CN",
		ColorDepth:                "24",
		HardwareConcurrency:       "16",
		ScreenResolution:          "390,844",
		AvailableScreenResolution: "844,390",
		TimezoneOffset:            "240",
		SessionStorage:            "true",
		LocalStorage:              "true",
		IndexedDb:                 "true",
		AddBehavior:               "false",
		OpenDatabase:              "false",
		Platform:                  "android",
		Plugins:                   "undefined",
		WebglVendorAndRenderer:    "Qualcomm~Adreno (TM) 530", //Apple Inc.~Apple GPU
		AdBlock:                   "false",
		HasLiedLanguages:          "false",
		HasLiedResolution:         "false",
		HasLiedOs:                 "true",
		HasLiedBrowser:            "false",
		TouchSupport:              "5,true,true",
		Fonts:                     "33",
		Audio:                     "undefined",
	}
}

// ================================================================
// Device 设备
// ================================================================

// Device 完整设备信息。
type Device struct {
	Cuid       string            `json:"cuid"`
	Fuid       string            `json:"fuid"`
	Gid        string            `json:"gid"`
	LogTraceID string            `json:"log_trace_id"`
	PassID     string            `json:"pass_id"`
	Rinfo      map[string]string `json:"rinfo"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Info       DeviceInfo        `json:"info"`
	Xyus       string            `json:"xyus"`    // sofire xyus: MD5(UUID).upper() + "|0"
	Xyusec     string            `json:"xyusec"`  // Base64(ac(xyus)) — sofire 加密后的设备串
	Zid30      string            `json:"zid_30b"` // 30B zid (xytk_m): 22B 指纹 + 8B 尾
	Zid65      string            `json:"zid_65b"` // 65B zid (s_to_re_d.token): base64url(32B/32B)
}

// ================================================================
// sofire zid 生成 (逆向复刻, 见 zid_破解进度.md)
//   核心: ac/dc = AES-128-CBC(key=30212102dicudiab, IV=0), PKCS7
//         xyus   = MD5(UUID).upper() + "|0"
//         xyusec = Base64(ac(xyus))
//         itb 输出 30B = [22B 稳定设备指纹] + [8B 透传尾]
//         65B zid     = base64url( seg1(32B) + "/" + seg2(32B) )
//   服务器对 zid 校验宽松 → 模板化生成即可 (实测篡改末字节仍登录成功)
// ================================================================

// sofire 常量 (逆向确认)
const (
	sofireAESKey     = "30212102dicudiab"                                                 // m.g.a(16)
	zidFingerprint22 = "2D581DB41C75FC33764CCCBBE868499485496B2C1145"                     // 当前设备 22B 指纹模板
	zidSeg1Current   = "e701dc3e5709838baae86d153b0a75daf07a4b29e2170005c0b32cfc9d635dd1" // 32B seg1 模板
	zidHistorical65B = "04VCC0ogo2WhreKTkn-1IbswOJ4jjVgu6ajgMIytWASTF3PVO-_Iy3vJ3Av1xdmpFfqxZUF_CUj5fAf5eNpaFZw"
)

// ================================================================
// 生成函数
// ================================================================

func generateCuid() string {
	r := mrand.Float64()
	return strings.ToUpper(fmt.Sprintf("%x", md5.Sum(fmt.Appendf(nil, "%f", r))))
}

func generateFuid(info DeviceInfo) (string, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	return crypto.AesECBEncryptPKCS7(string(data), "FfdsnvsootJmvNfl")
}

func generateRinfo(fuid string) map[string]string {
	return map[string]string{"fuid": fmt.Sprintf("%x", md5.Sum([]byte(fuid)))}
}

func generateGid() string {
	uuid := make([]byte, 16)
	rand.Read(uuid)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]))
}

func generatePassID() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 4)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

func generateLogTraceID() string {
	b := make([]byte, 30)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func randomMD5() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", md5.Sum(b))
}

// ================================================================
// sofire zid 生成 (复刻 zid_gen.py 逻辑)
// ================================================================

// genRandomUUID 生成 v4 风格 UUID 字符串 (小写, 与 Python uuid.uuid4() 一致)。
func genRandomUUID() string {
	u := make([]byte, 16)
	rand.Read(u)
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// genXyus 生成 sofire xyus: MD5(uuid).upper() + "|0"。
func genXyus() string {
	sum := md5.Sum([]byte(genRandomUUID()))
	return strings.ToUpper(fmt.Sprintf("%x", sum)) + "|0"
}

// aesCBCEncryptPKCS7 AES-128-CBC(IV=0) + PKCS7 加密 (sofire ac)。
func aesCBCEncryptPKCS7(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	pad := bs - len(data)%bs
	padded := append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, make([]byte, bs)).CryptBlocks(out, padded)
	return out, nil
}

// genXyusec xyusec = Base64(ac(xyus))。
func genXyusec(xyus string) (string, error) {
	ct, err := aesCBCEncryptPKCS7([]byte(xyus), []byte(sofireAESKey))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// genItbTail 生成 itb 输出尾 8B (透传): 输入 rand6 hex → [a][0x97][mid][b][0x00][c][6A][84]。
// 验证样本: rand=BC9218 → BC A6 C9 92 0F 18 6A 84 (mid=0xA4)
func genItbTail(rand6 string, midByte byte) ([]byte, error) {
	a, err := strconv.ParseUint(rand6[0:2], 16, 8)
	if err != nil {
		return nil, err
	}
	b, err := strconv.ParseUint(rand6[2:4], 16, 8)
	if err != nil {
		return nil, err
	}
	c, err := strconv.ParseUint(rand6[4:6], 16, 8)
	if err != nil {
		return nil, err
	}
	return []byte{byte(a), 0x97, midByte, byte(b), 0x0F, byte(c), 0x6A, 0x84}, nil
}

// genZid30 生成 30B zid (xytk_m 格式): 22B 稳定指纹 + 8B 动态尾。返回 hex 大写。
func genZid30(fingerprint22 []byte) (string, error) {
	if len(fingerprint22) == 0 {
		fp, err := hex.DecodeString(zidFingerprint22)
		if err != nil {
			return "", err
		}
		fingerprint22 = fp
	}
	rand6 := make([]byte, 3)
	rand.Read(rand6)
	rand6hex := strings.ToUpper(fmt.Sprintf("%x", rand6)) // 6 hex 字符
	tail, err := genItbTail(rand6hex, 0xA4)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(append(append([]byte{}, fingerprint22...), tail...))), nil
}

// genZid65 生成 65B zid (s_to_re_d.token 格式): base64url( seg1(32B) + "/" + seg2(32B) )。
// seg2 由 cuid + 时间戳派生 (每设备/每会话不同; 服务器校验宽松, 模板可用)。
func genZid65(cuid, seg1Hex string) (string, error) {
	if seg1Hex == "" {
		seg1Hex = zidSeg1Current
	}
	seg1, err := hex.DecodeString(seg1Hex)
	if err != nil || len(seg1) != 32 {
		seg1 = []byte(zidFingerprint22[:32]) // fallback
	}
	h := sha256.Sum256([]byte(cuid + fmt.Sprint(time.Now().UnixNano())))
	raw := append(append(append([]byte{}, seg1...), '/'), h[:32]...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ================================================================
// 手机型号预设 (UA + 显卡 + 屏幕, New 随机选用)
// ================================================================

// phoneProfile 一台手机型号的指纹组合。
type phoneProfile struct {
	name   string // 型号名(日志/调试用)
	ua     string // 完整 User-Agent(含 tieba 版本后缀, 与 sapi app_version 对应)
	gpu    string // WebglVendorAndRenderer(对应手机 GPU)
	width  int    // 屏幕宽
	height int    // 屏幕高
}

// phoneProfiles 内置手机型号池(UA/GPU/分辨率与机型一一对应)。
var phoneProfiles = []phoneProfile{
	{
		name: "SM-A5260(骁龙778G)",
		ua:   "Mozilla/5.0 (Linux; Android 12; SM-A5260 Build/V417IR; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/110.0.5481.154 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "Qualcomm~Adreno (TM) 642L",
		width: 1080, height: 2400,
	},
	{
		name: "Pixel 7(Google Tensor G2)",
		ua:   "Mozilla/5.0 (Linux; Android 13; Pixel 7 Build/TQ3A.230805.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/116.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "Google~Mali-G710",
		width: 1080, height: 2400,
	},
	{
		name: "小米13(骁龙8Gen2)",
		ua:   "Mozilla/5.0 (Linux; Android 13; 2201123C Build/TKQ1.220829.002; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/116.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "Qualcomm~Adreno (TM) 740",
		width: 1080, height: 2400,
	},
	{
		name: "红米Note12Pro(天玑1080)",
		ua:   "Mozilla/5.0 (Linux; Android 13; 22101316C Build/TKQ1.221013.002; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/114.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "ARM~Mali-G68",
		width: 1080, height: 2400,
	},
	{
		name: "Mate60Pro(麒麟9000S)",
		ua:   "Mozilla/5.0 (Linux; Android 12; ALN-AL00 Build/HUAWEIALN-AL00; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/110.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "Huawei~Mali-G710",
		width: 1260, height: 2720,
	},
	{
		name: "OPPO Find X6(天玑9200)",
		ua:   "Mozilla/5.0 (Linux; Android 13; PGFM10 Build/TP1A.220905.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/115.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "ARM~Mali-G715",
		width: 1260, height: 2772,
	},
	{
		name: "vivo X90(天玑9200)",
		ua:   "Mozilla/5.0 (Linux; Android 13; V2241A Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/115.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "ARM~Mali-G715",
		width: 1260, height: 2800,
	},
	{
		name: "荣耀90(骁龙7Gen1)",
		ua:   "Mozilla/5.0 (Linux; Android 13; REA-NX9 Build/HONORREA-NX9; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/115.0.0.0 Mobile Safari/537.36 tieba/22.9.1.0",
		gpu:  "Qualcomm~Adreno (TM) 644",
		width: 1200, height: 2664,
	},
}

// randomPhoneProfile 随机取一台手机型号。
func randomPhoneProfile() phoneProfile {
	return phoneProfiles[mrand.Intn(len(phoneProfiles))]
}

// ================================================================
// 工厂
// ================================================================

// New 创建随机设备指纹 (随机手机型号: UA/GPU/分辨率与机型一致; 含 sofire zid 全链路)。
func New() (*Device, error) {
	profile := randomPhoneProfile()
	info := DefaultDeviceInfo()
	info.UserAgent = profile.ua
	info.WebglVendorAndRenderer = profile.gpu
	info.ScreenResolution = fmt.Sprintf("%d,%d", profile.width, profile.height)
	info.AvailableScreenResolution = fmt.Sprintf("%d,%d", profile.width, profile.height-138) // 去掉状态栏/导航栏
	info.Canvas = randomMD5()
	info.Webgl = randomMD5()

	fuid, err := generateFuid(info)
	if err != nil {
		return nil, err
	}

	// --- sofire zid 链路 ---
	cuid := generateCuid()
	xyus := genXyus()
	xyusec, err := genXyusec(xyus)
	if err != nil {
		return nil, err
	}
	zid30, err := genZid30(nil)
	if err != nil {
		return nil, err
	}
	zid65, err := genZid65(cuid, "")
	if err != nil {
		return nil, err
	}

	return &Device{
		Cuid:       cuid,
		Fuid:       fuid,
		Gid:        generateGid(),
		LogTraceID: generateLogTraceID(),
		PassID:     generatePassID(),
		Rinfo:      generateRinfo(fuid),
		Width:      profile.width,
		Height:     profile.height,
		Info:       info,
		Xyus:       xyus,
		Xyusec:     xyusec,
		Zid30:      zid30,
		Zid65:      zid65,
	}, nil
}
