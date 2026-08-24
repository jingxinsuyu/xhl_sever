// Package qrlogin 百度 sapi 扫码确认：输入二维码链接 + 账号 cookie，
// 动态生成设备指纹（cuid/zid），组 sapi 参数并签名，POST passport.baidu.com 确认登录。
// 复刻 app_re/qr_scan_confirm.py。
package qrlogin

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"xhl-server/internal/baidu/device"
)

// appSignKey sapi 签名密钥（passport 版本相关，勿随意改）。
const appSignKey = "6e93e7659ae637845c7f83abee68a740"

// sapiURL 确认接口，lp 跟随二维码（测试可替换）。
var sapiURL = "https://passport.baidu.com/v2/sapi/qrlogin?lp="

// requestTimeout 单次确认请求超时。
const requestTimeout = 15 * time.Second

// Result 一次扫码确认的结果。
type Result struct {
	OK      bool   // 是否确认成功
	Errno   string // 服务器 errno（失败时展示）
	Code    string // 服务器 code
	Message string // 服务器 message / error_msg
}

// ErrNoSign 二维码内容中缺少 sign。
var ErrNoSign = errors.New("二维码内容中没有 sign")

// javaURLEncode 复刻 Python `quote_plus(s, safe="*-._").replace("~","%7E")`。
// 恒安全：字母/数字/`*-._`；空格→`+`；`~`→`%7E`；其余字节→%XX 大写。
func javaURLEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '*', c == '-', c == '.', c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		case c == '~':
			b.WriteString("%7E")
		default:
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}

// calculateSig 计算 sapi 签名：参数按键字典序排序，每项拼 `k=<URL编码(v)>&`，
// 末尾追加 `sign_key=<key>`，整体 MD5 小写 hex。
//
// ⚠️★ 2026-08-21 修复（逆向自 passport 抓包 + 苏芸朵朵8 号实测）★⚠️
//
//	必须【连空值一起参与签名】——不能跳过空值！
//	背景：纯 BDUSS 扫码时 stoken/ptoken 传空字符串（见 buildParams），
//	但服务器验签时是把 stoken=/ptoken= 空值字段一并算进 sig 的；
//	旧实现 `if v != ""` 跳过了空值 → 签名与服务器计算不一致 → 恒返回 errno:-2。
//	修复后实测：bduss + stoken="" + ptoken="" + 本签名 → errno:0 确认成功。
//	注意：request 中 stoken=/ptoken= 空值字段也必须存在（Go url.Values.Encode 会输出 key=）。
func calculateSig(params map[string]string, signKey string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(javaURLEncode(params[k])) // 空值拼出 "k="（不跳过！）
		sb.WriteByte('&')
	}
	sb.WriteString("sign_key=")
	sb.WriteString(signKey)
	return md5Sum(sb.String())
}

func md5Sum(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

// parseCookies 解析 `k=v; k2=v2; ...` cookie 串为 map。
func parseCookies(s string) map[string]string {
	c := make(map[string]string)
	for _, item := range strings.Split(s, ";") {
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			k := strings.TrimSpace(item[:idx])
			v := strings.TrimSpace(item[idx+1:])
			if k != "" {
				c[k] = v
			}
		}
	}
	return c
}

// ParseQRLink 解析二维码链接，返回 sign 与 lp（缺省 app）。
// 逻辑对齐 Python：含 `?` 按 URL 解析 query，否则把整串当 query。
func ParseQRLink(qr string) (sign, lp string, err error) {
	lp = "app"
	var rawQuery string
	if strings.Contains(qr, "?") {
		u, e := url.Parse(qr)
		if e != nil || u.Scheme == "" {
			u, e = url.Parse("http://x/" + qr)
			if e != nil {
				return "", "", errors.New("二维码内容解析失败")
			}
		}
		rawQuery = u.RawQuery
	} else {
		rawQuery = qr
	}
	vals, e := url.ParseQuery(rawQuery)
	if e != nil {
		return "", "", errors.New("二维码内容解析失败")
	}
	if s := vals.Get("sign"); s != "" {
		sign = s
	}
	if l := vals.Get("lp"); l != "" {
		lp = l
	}
	if sign == "" {
		return "", "", ErrNoSign
	}
	return sign, lp, nil
}

// buildParams 组 sapi 公共参数 + 业务参数（sig 由调用方计算）。
// ★ 纯 BDUSS 扫码（2026-08-21 逆向确认）：stoken/ptoken 传空值即可成功确认，
//
//	不需要真实的 STOKEN/PTOKEN（服务器只验 sig 签名一致性，不校验 token 值）。
//	关键：① 空值字段必须存在（Go 编码后输出 stoken=&ptoken=）；
//	      ② calculateSig 必须连空值一起算（见 calculateSig 注释，否则 errno:-2）。
func buildParams(sign, lp string, cookies map[string]string, dev *device.Device) map[string]string {
	return map[string]string{
		"client":      "android",
		"cuid":        dev.Cuid,
		"clientid":    dev.Cuid,
		"clientfrom":  "native",
		"zid":         dev.Zid65,
		"appid":       "1",
		"tpl":         "tb",
		"app_version": "22.9.1.0",
		"sdk_version": "9.15.0",
		"sdkversion":  "9.15.0",
		"sign":        sign,
		"cmd":         "login",
		"bduss":       cookies["BDUSS"],
		// 纯 BDUSS 扫码：stoken/ptoken 传空值（签名算法连空值一起算，服务器验签才匹配）
		"stoken": "",
		"ptoken": "",
	}
}

// newClient 构造带代理（可空）与超时的 HTTP 客户端。
func newClient(proxyAddr string) (*http.Client, error) {
	client := &http.Client{Timeout: requestTimeout}
	if proxyAddr != "" {
		target := proxyAddr
		if !strings.Contains(proxyAddr, "://") {
			target = "http://" + proxyAddr
		}
		pu, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		client.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	return client, nil
}

// Confirm 完成一次扫码确认：解析链接 → 动态生成设备 → 组参数签名 → POST sapi。
// cookie 中缺 BDUSS 返回 Result{OK:false}（不报错，视为凭证无效）。
func Confirm(qrContent, cookie, proxyAddr string) (Result, error) {
	sign, lp, err := ParseQRLink(qrContent)
	if err != nil {
		return Result{}, err
	}
	dev, err := device.New()
	if err != nil {
		return Result{}, errors.New("设备指纹生成失败") // 不暴露内部错误
	}
	cookies := parseCookies(cookie)
	if cookies["BDUSS"] == "" {
		return Result{OK: false, Message: "cookie 中缺少 BDUSS"}, nil
	}

	params := buildParams(sign, lp, cookies, dev)
	// ★ 2026-08-21: sig 必须连空值一起算（calculateSig 已修复），
	//   且算 sig 时 params 里还没有 sig 字段（先算后加，sig 不参与自身签名）。
	params["sig"] = calculateSig(params, appSignKey)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	client, err := newClient(proxyAddr)
	if err != nil {
		return Result{}, errors.New("网络配置错误1000") // 不暴露内部错误
	}
	req, err := http.NewRequest(http.MethodPost, sapiURL+url.QueryEscape(lp), strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, errors.New("构造请求失败") // 不暴露内部错误
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", dev.Info.UserAgent) // 与设备指纹一致（随机手机型号）
	req.Header.Set("Host", "passport.baidu.com")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, errors.New("连接百度服务器失败，请稍后尝试") // 不暴露内部错误
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	fmt.Printf("[confirm]:%s \n", string(body))
	if err != nil {
		return Result{}, errors.New("读取百度响应失败") // 不暴露内部错误
	}

	return parseSapiResp(body)
}

// errnoMessages 扫码确认 errno → 中文友好提示（对齐 QrAppLoginResult.java）。
// errno=1 二维码已过期 / 2 确认方 BDUSS 已过期 / 3 用户尚未正常化 / 160102 BDUSS 为空。
var errnoMessages = map[int]string{
	1:      "二维码已过期，请刷新二维码后重试",
	2:      "登录状态已失效（BDUSS 过期），请重新登录",
	3:      "用户尚未正常化",
	160102: "BDUSS 为空，cookie 无效",
}

// parseSapiResp 解析 sapi 响应。成功判定与 Python 一致：code in ("0","110000") 或 errno==0。
// 失败时优先用百度 message；为空则按 errno 映射为中文友好提示。
func parseSapiResp(body []byte) (Result, error) {
	var j map[string]interface{}
	if err := json.Unmarshal(body, &j); err != nil {
		return Result{}, errors.New("百度响应格式异常") // 不暴露内部错误
	}
	code := fmt.Sprintf("%v", j["code"])
	errno := -1
	if v, ok := j["errno"].(float64); ok {
		errno = int(v)
	}
	msg := ""
	if m, ok := j["message"].(string); ok && m != "" {
		msg = m
	} else if m, ok := j["error_msg"].(string); ok && m != "" {
		msg = m
	}
	if msg == "" {
		if m, ok := errnoMessages[errno]; ok {
			msg = m
		}
	}

	ok := code == "0" || code == "110000" || errno == 0
	return Result{OK: ok, Errno: fmt.Sprintf("%d", errno), Code: code, Message: msg}, nil
}
