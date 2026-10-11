package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Server 服务配置
type Server struct {
	Port      string `yaml:"port"`
	Mode      string `yaml:"mode"`
	StaticDir string `yaml:"static_dir"` // 前端静态资源目录（相对 config.yaml 所在目录）
	// AdminPath 后台管理入口路径前缀（随机字符串，如 /ef16e15c12bb）。
	// 非空时后台仅在该前缀下提供，其余路径（含根路径 /）一律 404，避免后台被直接扫到；
	// 为空时保持旧行为（后台挂在根路径，便于本地调试）。
	AdminPath string `yaml:"admin_path"`
}

// Database 数据库配置
type Database struct {
	Host         string `yaml:"host"`
	Port         string `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DBName       string `yaml:"dbname"`
	Charset      string `yaml:"charset"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// JWT 配置
type JWT struct {
	Secret          string `yaml:"secret"`
	ExpireHours     int    `yaml:"expire_hours"`      // 后台管理员 token 过期（小时）
	UserExpireHours int    `yaml:"user_expire_hours"` // 用户 token 过期（小时）
}

// SuperAdmin 超级管理员（yaml 配置，不落库）
type SuperAdmin struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Nickname string `yaml:"nickname"`
}

// Redis Redis 配置
type Redis struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// Security 登录安全配置
type Security struct {
	LoginRateLimitPerMin int            `yaml:"login_rate_limit_per_min"` // 每账号每分钟允许登录次数
	CaptchaExpireSeconds int            `yaml:"captcha_expire_seconds"`   // 图形验证码有效期（秒）
	ClientSignSalt       string         `yaml:"client_sign_salt"`         // 客户端登录参数签名盐
	ClientAESKey         string         `yaml:"client_aes_key"`           // 客户端登录密码 AES 密钥（16 字节）
	QrLoginAESKey        string         `yaml:"qrlogin_aes_key"`          // qrlogin data 加密 AES 密钥（16 字节）
	CaptchaStep          map[string]int `yaml:"captcha_step"`             // 各接口每日调用每 N 次触发一次滑动验证码（key=接口名，0=不触发）
}

// Upload 文件上传配置
type Upload struct {
	Dir     string `yaml:"dir"`      // 上传根目录（相对或绝对路径）
	BaseURL string `yaml:"base_url"` // 静态资源对外访问基础地址，如 http://127.0.0.1:8080
}

// Cost 扣费配置
type Cost struct {
	OpenQrLoginCost int `yaml:"open_qrlogin_cost"` // 开放平台每次扫码确认扣费积分（调用即扣，0=不扣）
	FdevIssueCost   int `yaml:"fdev_issue_cost"`   // fdev 出包每次扣费积分（调用即扣，0=不扣；解密 /open 不扣费）
	OpenFigureCost  int `yaml:"open_figure_cost"`  // 贴吧虚拟形象每次设置成功扣费积分（成功才扣；默认 1）
}

// Log 记录相关配置
type Log struct {
	// RetainDays 调用记录（call_log）保留天数：0/缺省 = 90 天。
	// 想完全不清就填一个很大的数（比如 36500）；扣费账本（billing_log）不受这里影响，永不自动清理。
	RetainDays int `yaml:"retain_days"`
}

// Tieba 贴吧接口地址（留空用官方地址）。
// 放出来是为了两件事：Baidu 换域名时不用改代码；本地自测可以指到 mock 服务上。
type Tieba struct {
	UploadURL string `yaml:"upload_url"`
	MetaURL   string `yaml:"meta_url"`
	SubmitURL string `yaml:"submit_url"`
}

// LoginRecords 「登录记录查询」的**协议下发**配置（服务端只发指令，客户端用本机 IP 直连上游）。
//
// 为什么协议放服务端：上游（百度 passport loginrecord）只需要被查询账号自己的 cookie，
// 本身**不需要任何共享密钥**；把它放这里的价值是「协议/域名/参数/解析规则由服务端下发」——
// 上游改了只改配置重启，老客户端不用重新发版。
//
// 注意（诚实边界）：本段里的值会随票据下发到客户端，因此**它不是对客户端保密的密钥**，
// 只能当作「可轮换的协议参数」。真正的凭据始终是调用方自己的 ck（不落库、不写日志）。
type LoginRecords struct {
	// UpstreamURL 上游查询地址（留空用内置官方地址 passport.baidu.com/.../loginrecord）。
	UpstreamURL string `yaml:"upstream_url"`
	// Method 上游 method，留空 GET。
	Method string `yaml:"method"`
	// Query 附加 query 参数；值中 ${TS_MS} 由客户端在发送时替换成毫秒时间戳。
	Query map[string]string `yaml:"query"`
	// Headers 附加请求头；值中 ${CK} 替换成调用方的 ck、${UPSTREAM_KEY} 替换成 upstream_key。
	Headers map[string]string `yaml:"headers"`
	// UserAgent 上游 UA（留空用内置照抓包 UA）。
	UserAgent string `yaml:"user_agent"`
	// TimeoutSeconds 客户端执行上游请求的建议超时（秒，默认 15）。
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// SuccessCodes 视为「查到了登录记录」的上游 code（默认 [110000, 0]）——命中才允许扣费。
	SuccessCodes []int `yaml:"success_codes"`
	// TicketSecret 票据 HMAC 密钥；留空回退 security.client_sign_salt。轮换=改这里并重启。
	TicketSecret string `yaml:"ticket_secret"`
	// TicketTTLSeconds 票据有效期（秒，默认 120；上限 600）。
	TicketTTLSeconds int `yaml:"ticket_ttl_seconds"`
	// UpstreamKey 可选的上游密钥片段：非空时以 ${UPSTREAM_KEY} 注入到下发的 headers/query。
	// 本项目上游不需要它，默认留空；且下发到客户端后即不再是秘密，仅用于「协议可轮换」。
	UpstreamKey string `yaml:"upstream_key"`
}

// TiebaStatus 「贴吧状态查询器」的**协议下发**配置（服务端只发指令，客户端用本机 IP 直连上游）。
//
// 与 LoginRecords 同一套架构与理由（见 LOGIN_RECORDS_API.md 第 0 节）：
// 上游是贴吧的公开主页接口 `home/get/panel`，**不需要任何共享密钥**，
// 所以本段唯一的价值是「上游域名/参数/字段路径/单位换算/错误码释义可热换」——
// 上游改了只改配置重启，老客户端不用重新发版。
//
// 诚实边界：本段的值会随票据下发到客户端，**它不是对客户端保密的密钥**；
// 唯一凭据是调用方自己的 ck（只以 sha256 指纹进票据，不落库、不写日志）。
type TiebaStatus struct {
	// UpstreamURL 上游查询地址（留空用内置 https://tieba.baidu.com/home/get/panel）。
	UpstreamURL string `yaml:"upstream_url"`
	// Method 上游 method，留空 GET。
	Method string `yaml:"method"`
	// Query 附加 query 参数；值里 ${UN} = 用户名、${RED_TAG} = 10 位随机数、${TS_MS} = 毫秒时间戳。
	Query map[string]string `yaml:"query"`
	// Headers 附加请求头；值里 ${CK} 替换成调用方 ck、${UN} 替换成用户名、${UPSTREAM_KEY} 替换成 upstream_key。
	Headers map[string]string `yaml:"headers"`
	// UserAgent 上游 UA（留空用内置照抓包 WebUA）。
	UserAgent string `yaml:"user_agent"`
	// TimeoutSeconds 客户端执行上游请求的建议超时（秒，默认 15）。
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// SuccessCodes 视为「查到贴吧状态」的上游 code（默认 [0]）——命中才允许扣费。
	SuccessCodes []int `yaml:"success_codes"`
	// TicketSecret 票据 HMAC 密钥；留空回退 security.client_sign_salt。轮换=改这里并重启。
	TicketSecret string `yaml:"ticket_secret"`
	// TicketTTLSeconds 票据有效期（秒，默认 120；上限 600）。
	TicketTTLSeconds int `yaml:"ticket_ttl_seconds"`
	// UpstreamKey 可选的上游密钥片段：非空时以 ${UPSTREAM_KEY} 注入到下发的 headers/query。
	UpstreamKey string `yaml:"upstream_key"`

	// AgeField 吧龄字段路径（点分，默认 data.tb_age）。
	AgeField string `yaml:"age_field"`
	// PostField 发帖数字段路径（点分，默认 data.post_num）。
	PostField string `yaml:"post_field"`
	// CommentField 发言数字段路径（点分，默认 data.followed_count）。
	CommentField string `yaml:"comment_field"`
	// AgeUnit 吧龄原始单位：year（年，默认）/ day（天）。下发时算好 factor 供客户端换算。
	AgeUnit string `yaml:"age_unit"`
	// DaysPerYear 天→年换算基准（默认 365），仅 AgeUnit=day 时用到。
	DaysPerYear int `yaml:"days_per_year"`
	// ErrorCodes 上游 no/errno → 中文状态释义（留空用内置表）。
	ErrorCodes map[string]string `yaml:"error_codes"`
}

// Config 全局配置
type Config struct {
	Server       Server       `yaml:"server"`
	Database     Database     `yaml:"database"`
	Redis        Redis        `yaml:"redis"`
	Security     Security     `yaml:"security"`
	Cost         Cost         `yaml:"cost"`
	JWT          JWT          `yaml:"jwt"`
	SuperAdmin   SuperAdmin   `yaml:"super_admin"`
	Upload       Upload       `yaml:"upload"`
	Log          Log          `yaml:"log"`
	Tieba        Tieba        `yaml:"tieba"`
	LoginRecords LoginRecords `yaml:"login_records"`
	TiebaStatus  TiebaStatus  `yaml:"tieba_status"`

	// BaseDir 配置文件所在目录（软件根目录），供相对路径（如 upload.dir）解析基准，不来自 yaml。
	BaseDir string `yaml:"-"`
}

// Load 从指定路径加载 yaml 配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	// 记录配置文件所在目录，作为相对路径的基准（避免受进程启动目录影响）
	if abs, err := filepath.Abs(path); err == nil {
		cfg.BaseDir = filepath.Dir(abs)
	}
	return cfg, nil
}

// UploadDir 解析上传根目录（绝对路径）：
// upload.dir 为相对路径时，基于软件根目录（config.yaml 所在目录）解析，保证从任意目录启动都写到同一位置。
func (c *Config) UploadDir() string {
	dir := strings.TrimSpace(c.Upload.Dir)
	if dir == "" {
		dir = "uploads"
	}
	if !filepath.IsAbs(dir) && c.BaseDir != "" {
		dir = filepath.Join(c.BaseDir, dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// StaticDir 解析前端静态资源目录（绝对路径），规则同 UploadDir：
// static_dir 为相对路径时，基于软件根目录（config.yaml 所在目录）解析。
func (c *Config) StaticDir() string {
	dir := strings.TrimSpace(c.Server.StaticDir)
	if dir == "" {
		dir = "web"
	}
	if !filepath.IsAbs(dir) && c.BaseDir != "" {
		dir = filepath.Join(c.BaseDir, dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// AdminPath 返回归一化的后台入口前缀：保证以 "/" 开头、结尾无 "/"。
// 返回空串表示未配置（后台挂在根路径，兼容旧部署）。
func (c *Config) AdminPath() string {
	p := strings.TrimSpace(c.Server.AdminPath)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}
