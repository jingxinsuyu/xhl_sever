package qrlogin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"xhl-server/internal/baidu/device"
)

// authURL bduss 换取各平台 STOKEN（passport v3 auth，见 百度auth接口-获取各平台STOKEN.md）。
var authURL = "https://passport.baidu.com/v3/login/api/auth/"

// authUA auth 接口要求的 UA（tpl 内嵌 bduss）。
const authUA = "tpl:bs_andr;android_sapi_v9.15.316"

// bsAndrSignKey tpl=bs_andr 配套签名 key（tb 的是 6e93e...，勿混用）。
const bsAndrSignKey = "e7a13d9747624dedba676a666ba743ae"

// defaultTplList 一次请求的平台（netdisk=网盘 是本接口目标；附 pp/tb/bs 备用）。
const defaultTplList = "pp|tb|bs|netdisk"

// StokenResult bduss → STOKEN 换取结果。
type StokenResult struct {
	OK      bool              `json:"ok"`
	Errno   string            `json:"errno"`
	Errmsg  string            `json:"errmsg"`
	Stokens map[string]string `json:"stoken_list"` // 平台 → STOKEN（netdisk/pp/tb/bs）
}

// authResp auth 接口原始响应。
type authResp struct {
	Code       int               `json:"code"`
	Errno      int               `json:"errno"`
	Errmsg     string            `json:"errmsg"`
	StokenList map[string]string `json:"stoken_list"`
}

// ErrStokenNoBDUSS bduss 为空。
var ErrStokenNoBDUSS = errors.New("BDUSS 不能为空")

// CookieValues 解析 `k=v; k2=v2; ...` cookie 串为 map（与扫码解析同一套规则，
// 供开放接口从传入 ck 提取 BDUSS / PTOKEN）。
func CookieValues(s string) map[string]string {
	return parseCookies(s)
}

// FetchStokens 用 BDUSS（+可选 PTOKEN）换取各平台 STOKEN，现生成设备指纹无需缓存。
// 响应格式与实测一致；失败时返回非 nil error（网络/解析），errno!=0 时返回 StokenResult{OK:false}。
// proxyAddr 可空（空 = 直连）。
func FetchStokens(bduss, ptoken, proxyAddr string) (StokenResult, error) {
	bduss = strings.TrimSpace(bduss)
	if bduss == "" {
		return StokenResult{}, ErrStokenNoBDUSS
	}

	dev, err := device.New()
	if err != nil {
		return StokenResult{}, errors.New("设备指纹生成失败")
	}

	params := map[string]string{
		"return_type": "1",
		"cuid":        dev.Cuid,
		"clientid":    dev.Cuid,
		"app_version": "15.71.0.10",
		"bduss":       bduss,
		"clientfrom":  "native",
		"tpl":         "bs_andr",
		"sdkversion":  "9.15.316",
		"zid":         dev.Zid65,
		"tpl_list":    defaultTplList,
		"appid":       "1",
		"sdk_version": "9.15.316",
		"client":      "android",
		// ptoken 有就带真实值，没有传空值；空值也必须参与签名（sign_key 在签名末尾）
		"ptoken": strings.TrimSpace(ptoken),
	}
	params["sig"] = calculateSig(params, bsAndrSignKey)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	client, err := newClient(proxyAddr)
	if err != nil {
		return StokenResult{}, errors.New("网络配置错误")
	}
	req, err := http.NewRequest(http.MethodPost, authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return StokenResult{}, errors.New("构造请求失败")
	}
	req.Header.Set("User-Agent", authUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://passport.baidu.com/")
	req.Header.Set("Host", "passport.baidu.com")

	resp, err := client.Do(req)
	if err != nil {
		return StokenResult{}, errors.New("连接百度服务器失败，请稍后尝试")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return StokenResult{}, errors.New("读取百度响应失败")
	}

	var j authResp
	if err := json.Unmarshal(body, &j); err != nil {
		return StokenResult{}, errors.New("百度响应格式异常")
	}
	ok := j.Code == 0 || j.Errno == 0
	return StokenResult{
		OK:      ok,
		Errno:   fmt.Sprintf("%d", j.Errno),
		Errmsg:  j.Errmsg,
		Stokens: j.StokenList,
	}, nil
}
