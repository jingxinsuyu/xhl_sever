// 扫码前账号检测：调 passport user/info + loginprotect，判断账号能否扫码。
// 逆向依据（见 AlongTyRant账号检测报告.md）：
//   - user/info code=110000 账号在线；400021 = BDUSS 已失效
//   - loginprotect data.protect 决定能否扫码：
//       • 缺 qr 字段        → 扫码登录保护被开启（不能扫）
//       • qr 存在且 != "0"  → 扫码登录保护被开启（不能扫）
//       • qr 存在且 == "0"  → 正常，可扫码
//   - 注意：tieba sync 的 block_info（贴吧发帖封禁）不影响扫码，不作为判定依据。
package handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"xhl-server/internal/util"
)

// newUUID 生成标准 UUID（大写），loginprotect 的 gid 参数需要。
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// CheckScanRequest 检测请求：传完整 cookie。
type CheckScanRequest struct {
	// Cookie 完整百度 cookie 串（含 BDUSS 即可）。
	Cookie string `json:"cookie" binding:"required"`
}

// CheckScanResponse 检测结果。
type CheckScanResponse struct {
	Online   bool   `json:"online"`    // 账号是否在线（BDUSS 是否有效）
	CanScan  bool   `json:"can_scan"`  // 能否扫码登录
	Reason   string `json:"reason"`    // 不可扫的原因（空=可扫）
	Username string `json:"username"`  // 用户名（在线时返回）
	Uid      string `json:"uid"`       // uid（在线时返回）
}

// checkClient 检测用 HTTP 客户端。
var checkClient = &http.Client{Timeout: 15 * time.Second}

// UA 用 PC Chrome（loginprotect 网页接口）。
const checkUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// UserInfoResp passport user/info 响应（取需要的字段）。
type UserInfoResp struct {
	Code int `json:"code"`
	Data struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayname"`
		Uid         string `json:"uid"`
	} `json:"data"`
}

// LoginProtectResp loginprotect 响应。
type LoginProtectResp struct {
	Code int `json:"code"`
	Data struct {
		Protect map[string]string `json:"protect"`
	} `json:"data"`
}

// TiebaSyncResp tieba sync 响应（拿数字 uid）。
type TiebaSyncResp struct {
	Data struct {
		User struct {
			IsLogin int    `json:"is_login"`
			UserID  uint64 `json:"user_id"`
			NameShow string `json:"name_show"`
		} `json:"user"`
	} `json:"data"`
}

// tiebaUA sync 接口需贴吧 App UA。
const tiebaUA = "Mozilla/5.0 (Linux; Android 12; SM-A5260 Build/V417IR; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/110.0.5481.154 Mobile Safari/537.36 tieba/22.9.1.0"

// fetchUidFromTieba 调 tieba sync 拿数字 uid（user/info 没有 uid 字段）。
func fetchUidFromTieba(cookie string) (uint64, string, error) {
	req, err := http.NewRequest(http.MethodGet,
		"https://tieba.baidu.com/c/s/pc/sync?subapp_type=pc&_client_type=20&sign=e9b101df871c39eedcf9a232c2d26ec8", nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", tiebaUA)
	req.Header.Set("Host", "tieba.baidu.com")
	req.Header.Set("Cookie", cookie)
	resp, err := checkClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	var ts TiebaSyncResp
	if err := json.Unmarshal(body, &ts); err != nil {
		return 0, "", err
	}
	return ts.Data.User.UserID, ts.Data.User.NameShow, nil
}

// httpGetJSON 带 cookie GET 并解析 JSON。
func httpGetJSON(rawURL, cookie string, out any) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", checkUA)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://passport.baidu.com/v3/securitycenter")
	resp, err := checkClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// CheckScan 扫码前账号检测。
func (h *Handler) CheckScan(c *gin.Context) {
	var req CheckScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：cookie 不能为空")
		return
	}
	cookie := strings.TrimSpace(req.Cookie)
	if cookie == "" || !strings.Contains(cookie, "BDUSS") {
		util.Fail(c, util.CodeParamError, "cookie 无效：缺少 BDUSS")
		return
	}
	resp := CheckScanResponse{}

	// 1) user/info 判断在线
	var uinfo UserInfoResp
	if err := httpGetJSON("https://passport.baidu.com/v3/api/user/info", cookie, &uinfo); err != nil {
		util.Fail(c, util.CodeDBError, "调百度接口失败: "+err.Error())
		return
	}
	if uinfo.Code != 110000 {
		resp.Online = false
		resp.CanScan = false
		resp.Reason = "BDUSS 已失效（账号离线）"
		util.OK(c, resp)
		return
	}
	resp.Online = true
	resp.Username = uinfo.Data.Username

	// 1.5) 从 tieba sync 拿数字 uid(user/info 无 uid 字段; sync 稳定返回 user_id)
	if uid, nameShow, err := fetchUidFromTieba(cookie); err == nil && uid != 0 {
		resp.Uid = fmt.Sprintf("%d", uid)
		if resp.Username == "" || strings.Contains(resp.Username, "*") {
			resp.Username = nameShow // user/info 被脱敏时用 sync 的 name_show 兜底
		}
	} else {
		resp.Uid = uinfo.Data.Uid // 兜底:user/info 里的 uid(通常为空)
	}

	// 2) loginprotect 判断能否扫码
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	params := url.Values{}
	params.Set("adapter", "")
	params.Set("client", "pc")
	params.Set("clientfrom", "pc")
	params.Set("gid", newUUID())
	params.Set("lang", "zh-cn")
	params.Set("liveAbility", "")
	params.Set("suppcheck", "")
	params.Set("tpl", "pp")
	params.Set("tt", ts)
	params.Set("ttt", ts)
	apiURL := "https://passport.baidu.com/v3/api/safe/loginprotect?" + params.Encode()

	var lp LoginProtectResp
	if err := httpGetJSON(apiURL, cookie, &lp); err != nil {
		util.Fail(c, util.CodeDBError, "调 loginprotect 失败: "+err.Error())
		return
	}
	protect := lp.Data.Protect
	qrVal, hasQR := protect["qr"]
	switch {
	case !hasQR:
		// 缺 qr 字段 → 扫码登录保护被开启
		resp.CanScan = false
		resp.Reason = "扫码登录保护被开启（无qr字段）"
	case qrVal != "0":
		resp.CanScan = false
		resp.Reason = "扫码登录保护被开启"
	default:
		resp.CanScan = true
		resp.Reason = ""
	}
	util.OK(c, resp)
}
