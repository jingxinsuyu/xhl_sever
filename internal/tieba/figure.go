// Package tieba 贴吧「虚拟形象」设置（从 re_tieba/figure_set 移植，纯标准库）。
//
// 三步，缺一不可：
//  1. 传图   POST uploadphotos.baidu.com/Pic/upload?pid=tieba&filetype=base64   （file = base64(图片)）
//  2. 传 meta POST tieba.baidu.com/mo/q/customfigure/uploadFigureMeta            （figure_meta = base64(metaJSON)）
//  3. 提交   POST tieba.baidu.com/mo/q/customfigure/submitCustomFigure          （figure_pid = 第 1 步的 pic_id）
//
// 形象本体就是 submitCustomFigure 的 figure_pid；服务端按它渲染，
// 客户端从 http://tiebapic.baidu.com/figure/pic/item/<pic_id_encode>.jpg 拉图。
//
// 认证：整串 Cookie（至少要有 BDUSS 与 SToken，见 HasCreds）。
package tieba

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 默认上游地址（可通过 Client 字段覆盖，便于测试/换域名）
const (
	DefaultUploadURL = "https://uploadphotos.baidu.com/Pic/upload?pid=tieba&filetype=base64"
	DefaultMetaURL   = "https://tieba.baidu.com/mo/q/customfigure/uploadFigureMeta"
	DefaultSubmitURL = "https://tieba.baidu.com/mo/q/customfigure/submitCustomFigure"

	// FigureURLFmt 形象图地址（pic_id_encode → 图片）
	FigureURLFmt = "http://tiebapic.baidu.com/figure/pic/item/%s.jpg"
)

const (
	ua = "Mozilla/5.0 (Linux; Android 12; V2241A Build/V417IR; wv) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Version/4.0 Chrome/110.0.5481.154 Mobile Safari/537.36 tieba/22.11.1.0 skin/default"
	referer = "https://tieba.baidu.com/mo/hybrid-main-service/virtualAvatar/hybrid" +
		"?customfullscreen=1&nonavigationbar=1&skin=default"
)

// DefaultMeta 默认形象搭配（HAR 里那份 styleA/styleB，可直接当模板用）
const DefaultMeta = `{"styleA":{"background":"background_AB9DF5","recommendId":"recommend_person0","items":{` +
	`"head":{"itemId":"head_head3","color":"head_FEE7E0"},"body":{"itemId":"body_body1","color":"body_FEE0D5"},` +
	`"clothes":{"itemId":"clothes_clothes3"},"ear":{"itemId":"ear_ear3","color":"body_FEE0D5"},` +
	`"eyebrow":{"itemId":"eyebrow_eyebrow3"},"eye":{"itemId":"eye_eye3"},` +
	`"hair":{"itemId":"hair_hair3","color":"hair_261A40"},"mouth":{"itemId":"mouth_mouth3","color":"head_FEE7E0"},` +
	`"nose":{"itemId":"nose_nose1","color":"head_FEE7E0"}}},` +
	`"styleB":{"background":"background_FABA9B","style":"styleB","items":{` +
	`"head":{"itemId":"head_styleB_head211","color":"head_FEE7E0"},"body":{"itemId":"body_styleB_body1","color":"body_FEE0D5"},` +
	`"hair":{"itemId":"hair_styleB_hair1214","color":"hair_261A40"},"eye":{"itemId":"eye_styleB_eye113"},` +
	`"eyebrow":{"itemId":"eyebrow_styleB_eyebrow113","color":"eyebrow_2E1F1C"},` +
	`"nose":{"itemId":"nose_styleB_nose113","color":"head_FEE7E0"},"ear":{"itemId":"ear_styleB_ear113","color":"body_FEE0D5"},` +
	`"mouth":{"itemId":"mouth_styleB_mouth1115","color":"head_FFEEEB"},"clothes":{"itemId":"clothes_styleB_clothes1217"}}},` +
	`"currentStyle":"styleB"}`

// 默认背景（background_type=tone 时的纯色）
const DefaultBackgroundValue = "#FABA9B"

// 业务错误：调用方据此给用户可读提示
var (
	ErrCookieMissing = errors.New("cookie 缺少 BDUSS 或 SToken")
)

// Client 一次设置流程的操作者。
type Client struct {
	UploadURL string
	MetaURL   string
	SubmitURL string
	ProxyAddr string // 出口代理（host:port / http://user:pass@host:port），空=直连
	Cookie    string // 完整 cookie 串
	Timeout   time.Duration

	hc *http.Client
}

// Result 设置成功后的结果（仅服务端内部使用，不对外返回；
// 因此不带 json tag，避免以后被顺手 marshal 出去把图片信息泄给调用方）。
type Result struct {
	PicID        string // 形象图 pic_id（= figure_pid）
	PicIDEncode  string // 用于拼形象图地址
	FigureURL    string // 设置后的形象图地址
	ResourceLink string // meta 上传得到的 url
}

// NewClient 构造客户端（addr 为空则直连）。
func NewClient(cookie, addr string) *Client {
	return &Client{
		UploadURL: DefaultUploadURL,
		MetaURL:   DefaultMetaURL,
		SubmitURL: DefaultSubmitURL,
		ProxyAddr: strings.TrimSpace(addr),
		Cookie:    strings.TrimSpace(cookie),
	}
}

// HasCreds 检查 cookie 里有没有 BDUSS 与 SToken（大小写不敏感；贴吧两处叫法都出现过）。
// 返回 (是否齐全, 缺什么)。
func HasCreds(cookie string) (bool, string) {
	hasBDUSS, hasSToken := false, false
	for _, kv := range strings.Split(cookie, ";") {
		k, _, _ := strings.Cut(strings.TrimSpace(kv), "=")
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "bduss":
			hasBDUSS = true
		case "stoken":
			hasSToken = true
		}
	}
	switch {
	case !hasBDUSS && !hasSToken:
		return false, "BDUSS、SToken"
	case !hasBDUSS:
		return false, "BDUSS"
	case !hasSToken:
		return false, "SToken"
	}
	return true, ""
}

func (c *Client) httpClient() (*http.Client, error) {
	if c.hc != nil {
		return c.hc, nil
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := &http.Transport{}
	if c.ProxyAddr != "" {
		u, err := normalizeProxyURL(c.ProxyAddr)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(u)
	}
	c.hc = &http.Client{Timeout: timeout, Transport: tr}
	return c.hc, nil
}

// normalizeProxyURL 把 host:port / host:port:user:pass 也转成可用的代理 URL。
func normalizeProxyURL(addr string) (*url.URL, error) {
	addr = strings.TrimSpace(addr)
	if !strings.Contains(addr, "://") {
		parts := strings.Split(addr, ":")
		switch len(parts) {
		case 2: // host:port
			addr = "http://" + addr
		case 4: // host:port:user:pass
			addr = fmt.Sprintf("http://%s:%s@%s:%s", parts[2], parts[3], parts[0], parts[1])
		default:
			addr = "http://" + addr
		}
	}
	return url.Parse(addr)
}

func (c *Client) postForm(ctx context.Context, tag, rawURL string, fields map[string]string) (map[string]any, error) {
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader([]byte(form.Encode())))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("X-Requested-With", "com.baidu.tieba")
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Origin", "https://tieba.baidu.com")
	req.Header.Set("Referer", referer)

	hc, err := c.httpClient()
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s 请求失败：%w", tag, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s 返回非 JSON（HTTP %d）：%s", tag, resp.StatusCode, head(raw, 160))
	}
	return out, nil
}

// SetFigure 走完整三步；image 是图片原始字节（已在调用方校验过类型与大小）。
func (c *Client) SetFigure(ctx context.Context, image []byte, meta string) (*Result, error) {
	if ok, missing := HasCreds(c.Cookie); !ok {
		return nil, fmt.Errorf("%w（缺 %s）", ErrCookieMissing, missing)
	}
	if len(image) == 0 {
		return nil, errors.New("图片为空")
	}
	if strings.TrimSpace(meta) == "" {
		meta = DefaultMeta
	}

	// 1) 传图
	up, err := c.uploadImage(ctx, image)
	if err != nil {
		return nil, err
	}
	// 2) 传 meta
	link, err := c.uploadMeta(ctx, meta)
	if err != nil {
		return nil, err
	}
	// 3) 提交（背景默认纯色；figure_pid 用刚传上去的图）
	if err := c.submit(ctx, link, up.PicID, up.PicID, "tone", DefaultBackgroundValue); err != nil {
		return nil, err
	}
	if up.PicIDEncode != "" {
		up.FigureURL = fmt.Sprintf(FigureURLFmt, up.PicIDEncode)
	}
	up.ResourceLink = link
	return up, nil
}

func (c *Client) uploadImage(ctx context.Context, image []byte) (*Result, error) {
	data, err := c.postForm(ctx, "上传图片", c.UploadURL, map[string]string{
		"file": base64.StdEncoding.EncodeToString(image),
		"tbs":  "",
	})
	if err != nil {
		return nil, err
	}
	if no, _ := data["err_no"].(float64); no != 0 {
		return nil, fmt.Errorf("上传图片失败：%s", jsonHead(data))
	}
	info, _ := data["info"].(map[string]any)
	out := &Result{}
	out.PicID, _ = info["pic_id"].(string)
	out.PicIDEncode, _ = info["pic_id_encode"].(string)
	if out.PicID == "" {
		return nil, fmt.Errorf("上传图片没有返回 pic_id：%s", jsonHead(data))
	}
	return out, nil
}

func (c *Client) uploadMeta(ctx context.Context, metaJSON string) (string, error) {
	data, err := c.postForm(ctx, "上传形象参数", c.MetaURL, map[string]string{
		"figure_meta": base64.StdEncoding.EncodeToString([]byte(metaJSON)),
		"tbs":         "",
	})
	if err != nil {
		return "", err
	}
	if no, _ := data["no"].(float64); no != 0 {
		return "", fmt.Errorf("上传形象参数失败：%s", jsonHead(data))
	}
	d, _ := data["data"].(map[string]any)
	link, _ := d["resource_link"].(string)
	if link == "" {
		return "", fmt.Errorf("上传形象参数没有返回 resource_link：%s", jsonHead(data))
	}
	return link, nil
}

func (c *Client) submit(ctx context.Context, metaURLStr, figurePID, bgPID, bgType, bgValue string) error {
	data, err := c.postForm(ctx, "提交形象", c.SubmitURL, map[string]string{
		"meta_type":             "url",
		"meta_value":            metaURLStr,
		"figure_pid":            figurePID,
		"background_figure_pid": bgPID,
		"background_type":       bgType,
		"background_value":      bgValue,
		"text":                  "",
		"icon":                  "",
		"tbs":                   "",
	})
	if err != nil {
		return err
	}
	if no, _ := data["no"].(float64); no != 0 {
		return fmt.Errorf("提交形象失败：%s", jsonHead(data))
	}
	// 有的返回带 errno/no_msg，一并当失败处理，避免"看起来成功其实没设置上"
	if msg, _ := data["no_msg"].(string); msg != "" {
		return fmt.Errorf("提交形象失败：%s", msg)
	}
	return nil
}

func head(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}

func jsonHead(v any) string {
	b, _ := json.Marshal(v)
	return head(b, 200)
}
