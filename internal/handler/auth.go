package handler

import (
	"errors"
	"strconv"
	"strings"

	"xhl-server/internal/crypto"
	"xhl-server/internal/database"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// absInt64 绝对值
func absInt64(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}

// LoginRequest 后台管理员登录请求
type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token    string `json:"token"`
	Role     string `json:"role"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// AdminLogin 管理员登录（超级管理员由 yaml 配置，普通管理员查表）
func (h *Handler) AdminLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：username、password、captcha_id、captcha_code 不能为空")
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	// 防撞库：先校验图形验证码，再按账号限流
	if !h.verifyCaptcha(req.CaptchaID, req.CaptchaCode) {
		util.Fail(c, util.CodeCaptchaError, "验证码错误或已过期")
		return
	}
	if h.loginLimited(req.Username) {
		util.Fail(c, util.CodeRateLimited, "登录过于频繁，请 1 分钟后再试")
		return
	}

	// 超级管理员：校验 yaml 配置
	sa := h.Config.SuperAdmin
	if req.Username == sa.Username {
		if req.Password != sa.Password {
			util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
			return
		}
		token, err := util.GenerateToken(h.Config.JWT.Secret, h.Config.JWT.ExpireHours,
			util.TokenTypeAdmin, 0, sa.Username, model.RoleSuper, 0)
		if err != nil {
			util.Fail(c, util.CodeDBError, "生成 token 失败")
			return
		}
		util.OK(c, LoginResponse{Token: token, Role: model.RoleSuper, Username: sa.Username, Nickname: sa.Nickname})
		return
	}

	// 普通管理员：查表
	var admin model.Admin
	if err := database.DB.Where("username = ?", req.Username).First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if admin.Status == 0 {
		util.Fail(c, util.CodeAccountLocked, "该管理员已被禁用")
		return
	}
	if !util.CheckPassword(admin.Password, req.Password) {
		util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
		return
	}
	token, err := util.GenerateToken(h.Config.JWT.Secret, h.Config.JWT.ExpireHours,
		util.TokenTypeAdmin, admin.ID, admin.Username, model.RoleAdmin, 0)
	if err != nil {
		util.Fail(c, util.CodeDBError, "生成 token 失败")
		return
	}
	util.OK(c, LoginResponse{Token: token, Role: model.RoleAdmin, Username: admin.Username, Nickname: admin.Nickname})
}

// RegisterRequest 用户注册请求
type RegisterRequest struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
	SuperPassword string `json:"super_password" binding:"required"` // 超级密码（解绑等敏感操作校验用）
	CDKey         string `json:"cdkey" binding:"required"`          // 注册激活卡密（必填）
	CaptchaID     string `json:"captcha_id" binding:"required"`
	CaptchaCode   string `json:"captcha_code" binding:"required"`
}

// Register 用户注册：用户名(6-12)、密码(6-12)、超级密码(6-12)、卡密、图形验证码。
// 事务内：校验卡密 → 建用户 → 按卡密所属项目发放对应天数权限。
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：username、password、super_password、cdkey 不能为空")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.CDKey = strings.TrimSpace(req.CDKey)
	if req.Username == "" || req.CDKey == "" || req.SuperPassword == "" {
		util.Fail(c, util.CodeParamError, "参数错误：username、password、super_password、cdkey 不能为空")
		return
	}
	if len(req.Username) < 6 || len(req.Username) > 12 {
		util.Fail(c, util.CodeParamError, "用户名长度需在 6-12 之间")
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 12 {
		util.Fail(c, util.CodeParamError, "密码长度需在 6-12 之间")
		return
	}
	if len(req.SuperPassword) < 6 || len(req.SuperPassword) > 12 {
		util.Fail(c, util.CodeParamError, "超级密码长度需在 6-12 之间")
		return
	}
	if !h.verifyCaptcha(req.CaptchaID, req.CaptchaCode) {
		util.Fail(c, util.CodeCaptchaError, "验证码错误或已过期")
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	superHash, err := util.HashPassword(req.SuperPassword)
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	user := model.User{
		Username:      req.Username,
		Password:      hash,
		SuperPassword: superHash,
		Status:        model.UserStatusNormal,
	}
	var ct *model.CardType

	err = database.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Where("username = ?", user.Username).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrUsernameExists
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		ct, err = redeemCard(tx, &user, req.CDKey, "", timeNow())
		return err
	})
	if err != nil {
		code, msg := mapRedeemError(err)
		util.Fail(c, code, msg)
		return
	}

	// 注册完成立即发放登录 token，无需再次登录（用户 token 1 年有效）
	token, err := util.GenerateToken(h.Config.JWT.Secret, h.Config.JWT.UserExpireHours,
		util.TokenTypeUser, user.ID, user.Username, model.RoleUser, user.TokenVersion)
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	util.OK(c, gin.H{
		"id":             user.ID,
		"username":       user.Username,
		"role":           model.RoleUser,
		"token":          token,
		"project_id":     ct.ProjectID,
		"type_name":      ct.Name,
		"days":           ct.Days,
		"expires_at":     formatTimePtr(entitlementExpiresAt(user.ID, ct.ProjectID)),
	})
}

// UserLoginRequest 用户登录请求
// 密码以 password_enc（AES 加密）传输，请求带 ts（时间戳）与 sign（参数签名），防抓包/防篡改/防重放。
type UserLoginRequest struct {
	ProjectID   string `json:"project_id" binding:"required"`   // 项目 id（6 位数字，按项目绑定）
	MachineCode string `json:"machine_code" binding:"required"` // 机器码 / 安卓 id
	Username    string `json:"username" binding:"required"`
	PasswordEnc string `json:"password_enc" binding:"required"` // AES 加密后的登录密码
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
	Ts          int64  `json:"ts" binding:"required"`   // 客户端时间戳（Unix 秒）
	Sign        string `json:"sign" binding:"required"` // 参数签名（见用户api.md）
}

// UserLogin 用户登录。
// 流程：时间戳防重放 → 签名校验 → 解密密码 → 验证码 → 限流 → 校验账号密码 → 校验项目存在 → 机器码绑定 → 每日登录次数 → 发 token。
// 返回是否有剩余时间（按权限）。
func (h *Handler) UserLogin(c *gin.Context) {
	var req UserLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：登录参数不完整")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.MachineCode = strings.TrimSpace(req.MachineCode)
	if req.Username == "" || req.MachineCode == "" {
		util.Fail(c, util.CodeParamError, "参数错误：username、machine_code 不能为空")
		return
	}

	// 1. 时间戳防重放（5 分钟内）
	if req.Ts <= 0 || absInt64(timeNow().Unix()-req.Ts) > 300 {
		util.Fail(c, util.CodeParamError, "请求已过期，请重试")
		return
	}

	// 2. 签名校验
	params := map[string]string{
		"captcha_code": req.CaptchaCode,
		"captcha_id":   req.CaptchaID,
		"machine_code": req.MachineCode,
		"password_enc": req.PasswordEnc,
		"project_id":   req.ProjectID,
		"ts":           strconv.FormatInt(req.Ts, 10),
		"username":     req.Username,
	}
	if !util.VerifyClientSign(params, h.Config.Security.ClientSignSalt, req.Sign) {
		util.Fail(c, util.CodeParamError, "签名校验失败")
		return
	}

	// 3. 解密密码
	password, err := crypto.AesDecryptDouble64(req.PasswordEnc, h.Config.Security.ClientAESKey)
	if err != nil || password == "" {
		util.Fail(c, util.CodeParamError, "参数错误：密码无法解密")
		return
	}

	if !h.verifyCaptcha(req.CaptchaID, req.CaptchaCode) {
		util.Fail(c, util.CodeCaptchaError, "验证码错误或已过期")
		return
	}
	if h.loginLimited(req.Username) {
		util.Fail(c, util.CodeRateLimited, "登录过于频繁，请 1 分钟后再试")
		return
	}

	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
			return
		}
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if user.Status == model.UserStatusFrozen {
		util.Fail(c, util.CodeAccountLocked, "账号已被冻结")
		return
	}
	if !util.CheckPassword(user.Password, password) {
		util.Fail(c, util.CodeInvalidCred, "用户名或密码错误")
		return
	}

	// 项目必须存在且未删除
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", req.ProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}

	// 会员过期校验：该项目无剩余时间不允许登录（不消耗绑定与登录次数）
	ent := getEntitlement(user.ID, req.ProjectID)
	if ent == nil || !ent.IsValid(timeNow()) {
		util.Fail(c, util.CodeMembershipExpired, "会员已过期，无法登录")
		return
	}

	// 机器码绑定：首次自动绑定；已有其他设备绑定则拒绝，需先解绑
	bound, err := h.checkOrBindMachine(&user, req.ProjectID, req.MachineCode)
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if !bound {
		util.Fail(c, util.CodeDeviceNotBound, "该账号已被别的机器绑定，请先解绑后重试")
		return
	}

	// 记录今日登录次数（管理员会员情况页可见）；超限则拒绝
	loginCount := h.recordDailyLogin(req.ProjectID, user.ID)
	if project.LoginLimit > 0 && loginCount > int64(project.LoginLimit) {
		util.Fail(c, util.CodeLoginLimitExceed, "今日登录次数已达上限")
		return
	}

	// 登录使之前的 token 全部失效：token 版本 +1
	user.TokenVersion++
	if err := database.DB.Model(&user).Update("token_version", user.TokenVersion).Error; err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	// 登录成功返回：会员过期时间 / 今日登录次数 / 登录次数上限
	token, err := util.GenerateToken(h.Config.JWT.Secret, h.Config.JWT.UserExpireHours,
		util.TokenTypeUser, user.ID, user.Username, model.RoleUser, user.TokenVersion)
	if err != nil {
		util.Fail(c, util.CodeDBError, "生成 token 失败")
		return
	}
	util.OK(c, gin.H{
		"token":             token,
		"role":              model.RoleUser,
		"username":          user.Username,
		"project_id":        req.ProjectID,
		"has_time":          true,
		"expires_at":        formatTimePtr(&ent.ExpiresAt), // 会员过期时间
		"today_login_count": loginCount,                    // 今日登录次数
		"login_limit":       project.LoginLimit,            // 登录次数上限
	})
}

// checkOrBindMachine 校验并绑定机器码。
// 返回 bound=false 表示该用户在此项目已绑定其他设备，需先解绑。
func (h *Handler) checkOrBindMachine(user *model.User, projectID string, machineCode string) (bool, error) {
	var b model.UserBinding
	err := database.DB.Where("user_id = ? AND project_id = ? AND machine_code = ?", user.ID, projectID, machineCode).First(&b).Error
	if err == nil {
		return true, nil // 本设备已绑定
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	// 本设备未绑定：若该项目下已有其他设备绑定 → 拒绝
	var count int64
	if err := database.DB.Model(&model.UserBinding{}).
		Where("user_id = ? AND project_id = ?", user.ID, projectID).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	// 首次绑定该设备
	if err := database.DB.Create(&model.UserBinding{
		UserID:      user.ID,
		ProjectID:   projectID,
		MachineCode: machineCode,
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}
