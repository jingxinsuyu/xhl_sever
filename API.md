# 小火龙工具箱后端 · 所有接口调用文档

> 本文档覆盖后端全部 HTTP 接口，按「公共接口」与「管理后台接口」分组。
> 接口清单以 `internal/router/router.go` 为准。
> **第三方开放接口（API Key 鉴权）见 [OPEN_API.md](OPEN_API.md)。**

---

## 1. 通用约定

### 1.1 基础地址

| 环境 | Base URL |
|---|---|
| 本地开发 | `http://127.0.0.1:8080` |
| 生产（Docker） | `http://<服务器IP>:8888` |

### 1.2 鉴权方式

需要登录的接口在请求头携带 token：

```
Authorization: Bearer <token>
```

- 管理后台 token 通过 `POST /api/admin/login` 获取（`admin` / `super` 两种角色）。
- 用户端 token 通过注册 / 登录获取（`user` 角色）。
- 用户登录后旧 token 全部失效（token 版本机制）。

### 1.3 响应结构

所有接口统一返回 JSON（HTTP 状态码均为 200，业务结果用 `code` 区分）：

```json
{ "code": 0, "message": "ok", "data": { } }
```

| 字段 | 说明 |
|---|---|
| `code` | 业务码，0 表示成功 |
| `message` | 提示信息 |
| `data` | 业务数据，失败或部分接口不返回（`omitempty`） |

### 1.4 业务错误码

| code | 含义 |
|---|---|
| 0 | 成功 |
| 1001 | 参数错误 |
| 1002 | 未登录 / token 无效 |
| 1003 | 无权限 |
| 1004 | 资源不存在 |
| 1005 | 数据库错误 |
| 1006 | 冲突（用户名已存在等） |
| 1007 | 用户名或密码错误 |
| 1008 | 账号被冻结 / 禁用 |
| 1009 | 卡密已使用 |
| 1010 | 卡密无效 |
| 1011 | 卡密类型已删除 |
| 1012 | 图形验证码错误或已过期 |
| 1013 | 登录过于频繁（限流） |
| 1014 | 设备未绑定（换设备需先解绑） |
| 1015 | 今日登录次数已达上限 |
| 1016 | 无该项目权限 |
| 1017 | 今日自助解绑次数已达上限 |
| 1018 | 会员已过期 |

### 1.5 分页参数（查询列表类接口）

| 参数 | 说明 | 默认 |
|---|---|---|
| `page` | 页码（≥1） | 1 |
| `page_size` | 每页条数（1~100） | 10 |

分页响应 `data` 结构：

```json
{
  "list": [ ... ],
  "total": 0,
  "page": 1,
  "page_size": 10
}
```

### 1.6 图形验证码

注册、用户登录、管理员登录前需先调 `GET /api/captcha` 获取验证码，提交时携带 `captcha_id` + `captcha_code`（校验后即失效，防止重放）。

---

## 2. 用户端登录：签名与加密说明

`POST /api/user/login` 采用「时间戳防重放 + 参数签名 + 密码 AES 加密」三重防护，客户端需按以下算法构造参数。

### 2.1 密码加密（password_enc）

```
明文密码 → AES-ECB PKCS7 加密（密钥 = security.client_aes_key，16 字节）
       → 加密结果 Base64 编码
       → 对 Base64 字符串再 Base64 编码一次（双重 base64）
```

### 2.2 参数签名（sign）

参与签名的参数为（键名 **ASCII 升序** 排序）：

| 参数 | 取值 |
|---|---|
| `captcha_code` | 验证码 |
| `captcha_id` | 验证码 id |
| `machine_code` | 机器码 |
| `password_enc` | AES 加密后的密码 |
| `project_id` | 项目 id（字符串） |
| `ts` | 客户端时间戳（Unix 秒，字符串） |
| `username` | 用户名 |

```
规范串 = 参数按 ASCII 升序拼接为 key1=value1&key2=value2&...&key=签名盐
sign   = md5(规范串) 的 32 位小写十六进制
```

`key` 字段值使用 `security.client_sign_salt`（与客户端保持一致）。服务端用同样的规则重算比对。

### 2.3 时间戳防重放

`ts` 与服务器当前时间的差须在 **300 秒（5 分钟）** 内，否则返回 1001「请求已过期，请重试」。

---

## 3. 公共接口（用户端，无需登录）

### 3.1 健康检查

```
GET /api/health
```

无参数。返回 `{"code":0,"message":"ok"}`。

### 3.2 获取图形验证码

```
GET /api/captcha
```

无参数。响应 `data`：

```json
{
  "captcha_id": "xxxxxxxx",
  "image": "data:image/png;base64,...."
}
```

`image` 为 base64 图片，可直接用于 `<img src>`。有效期见配置 `captcha_expire_seconds`（默认 300 秒）。

### 3.3 用户注册

```
POST /api/user/register
Content-Type: application/json
```

请求体：

```json
{
  "username": "testuser",        // 必填，6-12 位
  "password": "123456",          // 必填，6-12 位（登录密码）
  "super_password": "654321",    // 必填，6-12 位（超级密码，解绑等敏感操作用）
  "cdkey": "XXXXXXXXXXXXXXXX",   // 必填，注册激活卡密
  "captcha_id": "xxxxxxxx",
  "captcha_code": "1234"
}
```

注册成功即发放用户 token，响应 `data`：

```json
{
  "id": 1,
  "username": "testuser",
  "role": "user",
  "token": "eyJ...",
  "project_id": "100001",
  "type_name": "月卡",
  "days": 30,
  "expires_at": "2026-09-11 10:00:00"
}
```

> 注册事务内自动完成卡密兑换，首次注册即绑定该项目权限（到期时间累加）。

### 3.4 用户登录

```
POST /api/user/login
Content-Type: application/json
```

请求体（`sign` / `password_enc` 算法见第 2 节）：

```json
{
  "project_id": "100001",
  "machine_code": "a1b2c3d4",
  "username": "testuser",
  "password_enc": "双重base64后的密文",
  "captcha_id": "xxxxxxxx",
  "captcha_code": "1234",
  "ts": 1723440000,
  "sign": "32位小写md5"
}
```

响应 `data`：

```json
{
  "token": "eyJ...",
  "role": "user",
  "username": "testuser",
  "project_id": "100001",
  "has_time": true,
  "expires_at": "2026-09-11 10:00:00",
  "today_login_count": 1,
  "login_limit": 0
}
```

| 字段 | 说明 |
|---|---|
| `has_time` | 该项目是否有剩余时间 |
| `expires_at` | 会员到期时间 |
| `today_login_count` | 今日已登录次数 |
| `login_limit` | 项目设置的每日登录上限（0=不限制） |

**关键逻辑：**
- 机器码绑定：首次登录自动绑定；该项目已绑定其他设备则返回 **1014**，需先解绑。
- 会员过期不允许登录，返回 **1018**。
- 项目 `login_limit > 0` 且今日次数超限，返回 **1015**。
- 登录后 token 版本 +1，之前所有 token 失效。

### 3.5 兑换（激活）卡密

```
POST /api/user/exchange
Content-Type: application/json
```

> 按用户名兑换，无需登录。

```json
{
  "project_id": "100001",
  "cdkey": "XXXXXXXXXXXXXXXX",
  "username": "testuser"
}
```

响应 `data`：

```json
{
  "type_id": 1,
  "type_name": "月卡",
  "days": 30,
  "project_id": "100001",
  "expires_at": "2026-09-11 10:00:00"
}
```

> 兑换天数在原有到期时间上累加（已过期则从当前起算）。卡密不属于该项目返回 1001「卡密不属于该项目」。

### 3.6 用户自助解绑

```
POST /api/user/unbind
Content-Type: application/json
```

```json
{
  "project_id": "100001",
  "username": "testuser",
  "super_password": "654321"
}
```

响应 `data`：`{"unbound": true}`

> 校验超级密码；解绑后可在新设备登录重新绑定。项目设置了 `unbind_limit > 0` 时，今日自助解绑超限返回 **1017**（管理员解绑不受限）。

### 3.7 获取项目轮播图

```
GET /api/user/carousels?project_id=100001
```

响应 `data`（数组）：

```json
[
  { "id": 1, "image_url": "/uploads/image/1723440000123.jpg", "link": "https://..." }
]
```

`link` 为空表示不跳转。`image_url` 在 `upload.base_url` 为空时为相对路径。

### 3.8 获取项目广告（富文本）

```
GET /api/user/ad?project_id=100001
```

响应 `data`：

```json
{
  "project_id": "100001",
  "content": "<p>广告内容</p>",
  "updated_at": "2026-08-12 10:00:00"
}
```

未设置时 `content` 为空字符串。

### 3.9 检测更新（用户端）

```
GET /api/user/update?project_id=100001&platform=android
```

`platform` 取值：`android` / `pc` / `ios`。

响应 `data`（该平台无版本时 `data` 为 null）：

```json
{
  "platform": "android",
  "version": "1.2.0",
  "file_name": "app-1.2.0.apk",
  "file_url": "/uploads/app/100001/1723440000123_app-1.2.0.apk",
  "file_size": 12345678,
  "created_at": "2026-08-12 10:00:00"
}
```

---

## 4. 百度扫码确认（需用户登录 + 100001 会员）

### 4.1 加密说明（data 字段）

前端把账号凭证按 `用户名----密码----cookie` 拼接后加密：

```
明文 = 用户名----密码----cookie
密文 = AES-ECB PKCS7 加密（密钥 = security.qrlogin_aes_key，16 字节）
data = 密文 Base64 编码（单层）
```

> 服务端优先按单层 base64 解密；失败自动回退「双重 base64」兼容旧约定。

### 4.2 接口（SSE 流式）

```
POST /api/xhl/qrlogin
Authorization: Bearer <用户token>
Content-Type: application/json
```

```json
{
  "loginUrl": "https://wappass.baidu.com/wp/?qrlogin&sign=xxxx&lp=pc",
  "data": "单层base64密文"
}
```

| 参数 | 说明 |
|---|---|
| `loginUrl` | 前端识别二维码中的链接（含 `sign`、可选 `lp`） |
| `data` | 加密后的 `用户名----密码----cookie` |

**鉴权/校验（不满足则返回普通 JSON 错误，不进入流）：**
- 需用户登录（未登录返回 **1002**）。
- 需是项目 `100001`（小火龙扫码登录器）会员且未过期（否则返回 **1004** / **1016**）。

**通过后返回 `text/event-stream`，逐步推送进度：**

```
data: {"type":"log","message":"正在解密账号信息"}
data: {"type":"log","message":"正在生成环境信息"}
data: {"type":"log","message":"正在获取代理"}        ← 仅配置了代理池时
data: {"type":"log","message":"正在请求百度确认"}
data: {"type":"result","ok":true,"errno":"0","message":"确认成功"}
```

| 事件字段 | 说明 |
|---|---|
| `type=log` | 进度日志，客户端逐条追加到日志框 |
| `type=result` | 最终结果（最后一个事件，随后流结束） |
| `ok` | 是否确认成功 |
| `errno` | 百度返回的 errno（字符串） |
| `message` | 结果/错误信息 |

解密后的账号凭证（用户名/密码/cookie）会写入 `ckdata` 表（每个用户一条，覆盖更新）。
扫码请求使用代理池：配置了 `proxy_url` 时从该地址拉取一个代理；未配置则不走代理。

---



## 5. 管理后台接口

> 除 `POST /api/admin/login` 外均需携带 `Authorization: Bearer <admin token>`。
> 标注 **【super】** 的接口仅超级管理员可用；其余管理员即可用（管理员被禁用后自动拒绝）。

### 5.1 管理员登录

```
POST /api/admin/login
Content-Type: application/json
```

```json
{
  "username": "admin",
  "password": "123456",
  "captcha_id": "xxxxxxxx",
  "captcha_code": "1234"
}
```

响应 `data`：

```json
{
  "token": "eyJ...",
  "role": "super",
  "username": "80453421",
  "nickname": "超级管理员"
}
```

`role` 取值：`super`（超级管理员，yaml 配置）/ `admin`（普通管理员，查表）。

---

### 5.2 管理员管理

#### 查询管理员列表

```
GET /api/admin/admins?keyword=&page=1&page_size=10
```

`keyword` 模糊匹配用户名/昵称。响应 `data` 为分页结构：

```json
{
  "list": [
    { "id": 1, "username": "admin", "nickname": "", "remark": "",
      "status": 1, "created_at": "2026-08-01 10:00:00", "updated_at": "2026-08-01 10:00:00" }
  ],
  "total": 1, "page": 1, "page_size": 10
}
```

`status`：1 启用 / 0 禁用。

#### 创建管理员 【super】

```
POST /api/admin/admins
```

```json
{ "username": "newadmin", "password": "123456", "nickname": "张三", "remark": "备注" }
```

密码 6-32 位。响应 `data`：`{"id": 2}`。

#### 修改管理员 【super】

```
PUT /api/admin/admins/:id
```

```json
{ "password": "newpass", "nickname": "李四", "remark": "备注" }
```

`password` 非空才修改。不能修改超级管理员（返回 1003）。

#### 冻结 / 解冻管理员 【super】

```
PUT /api/admin/admins/:id/status
```

```json
{ "status": 0 }
```

`status`：1 启用 / 0 禁用。不能冻结超级管理员或当前登录账号。

---

### 5.3 用户管理

#### 查询用户列表

```
GET /api/admin/users?keyword=&page=1&page_size=10
```

`keyword` 模糊匹配用户名。列表项：

```json
{
  "id": 1, "username": "testuser", "status": 1,
  "created_at": "2026-08-01 10:00:00", "updated_at": "2026-08-01 10:00:00"
}
```

#### 用户会员情况

```
GET /api/admin/users/:id/membership
```

响应 `data`：

```json
{
  "id": 1,
  "username": "testuser",
  "projects": [
    {
      "project_id": "100001",
      "project_name": "项目A",
      "expires_at": "2026-09-11 10:00:00",
      "has_time": true,
      "bindings": [ { "machine_code": "a1b2c3", "bound_at": "2026-08-01 10:00:00" } ],
      "today_login_count": 2
    }
  ]
}
```

`expires_at` 为 null 表示该用户在此项目无记录。

#### 清零用户今日登录次数

```
POST /api/admin/users/:id/clear-login
```

```json
{ "project_id": "100001" }
```

`project_id` 为 0 时清零该用户全部项目。响应 `data` 不返回。

#### 冻结 / 解冻用户

```
PUT /api/admin/users/:id/status
```

```json
{ "status": 0 }
```

`status`：1 启用 / 0 冻结。

#### 修改用户密码

```
PUT /api/admin/users/:id/password
```

```json
{ "password": "newpass", "ex_password": "newsuper" }
```

`password`（登录密码）/ `ex_password`（超级密码）至少填一个，长度 6-12。

#### 管理员解绑用户

```
POST /api/admin/users/:id/unbind
```

```json
{ "project_id": "100001" }
```

`project_id` 为空则解绑全部项目。响应 `data`：`{"unbound": true}`。管理员解绑不受每日次数限制。

---

### 5.4 项目管理

#### 查询项目列表

```
GET /api/admin/projects?keyword=&page=1&page_size=10
```

`keyword` 模糊匹配名称/备注。列表项：

```json
{
  "id": 1, "name": "项目A", "remark": "备注",
  "login_limit": 0, "unbind_limit": 0,
  "created_at": "2026-08-01 10:00:00", "updated_at": "2026-08-01 10:00:00"
}
```

`login_limit`：每日登录次数上限（0 不限制）；`unbind_limit`：每日自助解绑次数上限（0 不限制）。

#### 创建项目

```
POST /api/admin/projects
```

```json
{ "id": "100001", "name": "项目A", "remark": "备注", "login_limit": 0, "unbind_limit": 0 }
```

`id` 必填，6 位数字（用户自定义，创建后不可修改）。响应 `data`：`{"id": "100001"}`。

#### 修改项目

```
PUT /api/admin/projects/:id
```

```json
{ "name": "项目A", "remark": "备注", "login_limit": 3, "unbind_limit": 1 }
```

#### 删除项目（软删除）

```
DELETE /api/admin/projects/:id
```

伪删除，前端不再显示。

---

### 5.5 版本管理（按项目）

#### 查询项目版本

```
GET /api/admin/projects/:id/versions?platform=android
```

`platform` 可选过滤（`android` / `pc` / `ios`）。列表项：

```json
{
  "id": 1, "project_id": "100001", "platform": "android", "version": "1.2.0",
  "file_name": "app-1.2.0.apk",
  "file_url": "/uploads/app/100001/1723440000123_app-1.2.0.apk",
  "file_size": 12345678,
  "created_at": "2026-08-12 10:00:00"
}
```

#### 上传版本（multipart 表单）

```
POST /api/admin/projects/:id/versions
Content-Type: multipart/form-data
```

| 表单字段 | 说明 |
|---|---|
| `platform` | 必填，`android` / `pc` / `ios` |
| `version` | 必填，版本号 |
| `file` | 必填，更新文件 |

响应 `data`：`{"id":1,"platform":"android","version":"1.2.0","file_name":"...","file_url":"...","file_size":123}`

#### 删除版本

```
DELETE /api/admin/versions/:id
```

---

### 5.6 项目变量（按项目）

> 变量用于给客户端下发配置，类型 `string` / `int` / `bool` / `json`。

#### 查询项目变量

```
GET /api/admin/projects/:id/variables
```

```json
{
  "id": 1, "project_id": "100001", "key": "max_version", "type": "string",
  "value": "1.2.0", "created_at": "...", "updated_at": "..."
}
```

#### 新增变量

```
POST /api/admin/projects/:id/variables
```

```json
{ "key": "max_version", "type": "string", "value": "1.2.0" }
```

同一项目下 `key` 唯一。响应 `data`：`{"id": 1}`。

#### 修改变量

```
PUT /api/admin/variables/:id
```

```json
{ "key": "max_version", "type": "string", "value": "1.3.0" }
```

#### 删除变量

```
DELETE /api/admin/variables/:id
```

---

### 5.7 轮播图（按项目）

#### 查询项目轮播图

```
GET /api/admin/projects/:id/carousels
```

```json
{
  "id": 1, "project_id": "100001", "image_url": "/uploads/image/1723440000123.jpg",
  "link": "https://...", "created_at": "..."
}
```

#### 添加轮播图（multipart 表单）

```
POST /api/admin/projects/:id/carousels
Content-Type: multipart/form-data
```

| 表单字段 | 说明 |
|---|---|
| `image` | 必填，图片文件（自动裁剪为 380×65 jpg） |
| `link` | 跳转链接，可空 |
| `apply_all` | `1` 则同时为所有项目添加该轮播图 |

响应 `data`：

```json
{ "count": 1, "ids": [1], "image_url": "/uploads/image/1723440000123.jpg" }
```

#### 删除轮播图

```
DELETE /api/admin/carousels/:id
```

---

### 5.8 富文本广告（按项目）

#### 查询广告内容

```
GET /api/admin/projects/:id/rich-text
```

```json
{ "project_id": "100001", "content": "<p>广告</p>", "updated_at": "..." }
```

#### 保存广告内容（upsert）

```
PUT /api/admin/projects/:id/rich-text
```

```json
{ "content": "<p>新广告</p>" }
```

响应 `data`：`{"project_id":1,"content":"<p>新广告</p>"}`

---

### 5.9 卡密类型（按项目）

#### 查询卡密类型

```
GET /api/admin/card-types?project_id=100001&keyword=
```

```json
{
  "id": 1, "project_id": "100001", "name": "月卡", "days": 30,
  "created_at": "...", "updated_at": "..."
}
```

#### 创建卡密类型

```
POST /api/admin/card-types
```

```json
{ "project_id": "100001", "name": "月卡", "days": 30 }
```

`days` 必须 > 0，类型名同项目下唯一。响应 `data`：`{"id": 1}`。

#### 删除卡密类型（软删除）

```
DELETE /api/admin/card-types/:id
```

伪删除，防止已生成/已使用的卡密报错。

---

### 5.10 卡密（按项目）

#### 查询卡密

```
GET /api/admin/cards?project_id=100001&type_id=&keyword=&status=&start_time=&end_time=&page=1&page_size=10
```

| 参数 | 说明 |
|---|---|
| `project_id` | 必填 |
| `type_id` | 按类型过滤，可选 |
| `keyword` | 模糊匹配卡密 / 使用人用户名 |
| `status` | `0` 未使用 / `1` 已使用 |
| `start_time` / `end_time` | 按生成时间过滤，格式 `2006-01-02 15:04:05` |

列表项：

```json
{
  "id": 1, "cdkey": "XXXXXXXXXXXXXXXX", "type_id": 1, "type_name": "月卡",
  "days": 30, "status": 0,
  "user_id": null, "username": "", "used_at": null,
  "created_at": "2026-08-01 10:00:00"
}
```

`status`：0 未使用 / 1 已使用；已使用时 `user_id`、`username`、`used_at` 有值。

#### 批量生成卡密

```
POST /api/admin/cards/generate
```

```json
{ "type_id": 1, "count": 100 }
```

`count` 1~10000。响应 `data`：

```json
{
  "count": 100,
  "project_id": "100001",
  "type_id": 1,
  "cdkeys": ["XXXXXXXXXXXXXXXX", "..."]
}
```

#### 删除卡密

```
DELETE /api/admin/cards/:id
```

仅未使用卡密可删除（已使用返回 1006）。

---

### 5.11 代理池配置（所有项目公共）

> 存储在 Redis（key `xhl:proxy_url`），供扫码确认（/api/xhl/qrlogin）拉取代理。未配置则不使用代理。

#### 查询代理池配置

```
GET /api/admin/proxy-config
```

响应 `data`：

```json
{ "proxy_url": "http://api.example.com/getproxies" }
```

未配置时 `proxy_url` 为空字符串。

#### 保存代理池配置

```
PUT /api/admin/proxy-config
```

```json
{ "proxy_url": "http://api.example.com/getproxies" }
```

`proxy_url` 为空表示不使用代理。代理提取地址返回首行作为代理，支持
`host:port`、`http://host:port`、`host:port:user:pass` 三种格式。

---

## 6. 静态资源

- 上传文件访问：`/uploads/...`（如 `/uploads/image/xxx.jpg`、`/uploads/app/100001/xxx.apk`）。
- `upload.base_url` 非空时，`image_url` / `file_url` 返回完整地址（`base_url + /uploads/...`）；为空时返回相对路径。
