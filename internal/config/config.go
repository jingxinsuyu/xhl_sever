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
	LoginRateLimitPerMin int    `yaml:"login_rate_limit_per_min"` // 每账号每分钟允许登录次数
	CaptchaExpireSeconds int    `yaml:"captcha_expire_seconds"`   // 图形验证码有效期（秒）
	ClientSignSalt       string `yaml:"client_sign_salt"`         // 客户端登录参数签名盐
	ClientAESKey         string `yaml:"client_aes_key"`           // 客户端登录密码 AES 密钥（16 字节）
	QrLoginAESKey        string `yaml:"qrlogin_aes_key"`          // qrlogin data 加密 AES 密钥（16 字节）
}

// Upload 文件上传配置
type Upload struct {
	Dir     string `yaml:"dir"`      // 上传根目录（相对或绝对路径）
	BaseURL string `yaml:"base_url"` // 静态资源对外访问基础地址，如 http://127.0.0.1:8080
}

// Config 全局配置
type Config struct {
	Server     Server     `yaml:"server"`
	Database   Database   `yaml:"database"`
	Redis      Redis      `yaml:"redis"`
	Security   Security   `yaml:"security"`
	JWT        JWT        `yaml:"jwt"`
	SuperAdmin SuperAdmin `yaml:"super_admin"`
	Upload     Upload     `yaml:"upload"`

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
