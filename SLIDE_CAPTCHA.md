# 滑动拼图验证码 · 客户端接入文档

> 面向扫码登录器客户端前端。扫码接口每日调用**每 N 次**（当前 `qrlogin: 20`）会触发一次滑动拼图验证码。
> 前端需在扫码前检测到触发，弹出拼图面板，用户拖动拼块到缺口后，后端校验通过才能继续扫码。

## 1. 流程总览

```
客户端调 POST /api/xhl/qrlogin
  ├─ 返回 code=1021「请先完成拼图验证」→ 前端弹拼图面板
  │      ↓
  │   POST /api/xhl/captcha             → 拿背景图+拼图块 base64 + captcha_id
  │      ↓
  │   用户拖动拼块（前端记录轨迹）
  │      ↓
  │   POST /api/xhl/captcha/verify      → {ok:true} 通过（服务端发 60s 凭证）
  │      ↓
  └─ 重试 POST /api/xhl/qrlogin         → 正常放行
```

> **关键**：验证码通过后服务端发一个 **60 秒有效期的凭证**（按用户），期间再调扫码接口不会再触发验证码。
> 凭证被消费（扫码成功后）或 60s 过期后，下一次到步进点仍需再过一次。

## 2. 接口定义

所有接口需带用户登录态：`Authorization: Bearer <user_token>`。

### 2.1 触发检测

```
POST /api/xhl/qrlogin
```

- 若返回 `{"code":1021,"message":"请先完成拼图验证"}` → 触发验证码流程。
- 其他返回码 → 正常扫码流程不变。

### 2.2 获取验证码

```
POST /api/xhl/captcha
```

请求体：`{}`（空）

响应：
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "captcha_id": "af2e7642fb4617b93f5e6497f5ee7044",
    "bg_base64": "data:image/png;base64,iVBOR...",
    "piece_base64": "data:image/png;base64,iVBOR..."
  }
}
```

| 字段 | 说明 |
|---|---|
| `captcha_id` | 本次验证码唯一 id，校验时原样带回 |
| `bg_base64` | 背景图（250×150），带缺口遮罩，画布直接显示 |
| `piece_base64` | 拼图块（透明 PNG），需前端作为可拖动元素 |

### 2.3 校验

```
POST /api/xhl/captcha/verify
```

请求体：
```json
{
  "captcha_id": "af2e7642fb4617b93f5e6497f5ee7044",
  "data": "AES加密后的字符串"
}
```

`data` 为 **AES 加密的 JSON 密文**（不传明文坐标/轨迹），明文结构：
```json
{
  "x": 82,
  "y": 109,
  "trace": [
    {"t": 0,   "x": 10, "y": 0},
    {"t": 150, "x": 30, "y": 4},
    {"t": 400, "x": 80, "y": 2},
    {"t": 900, "x": 80, "y": 1},
    {"t": 1200,"x": 82, "y": 0}
  ]
}
```

响应：
```json
{ "code": 0, "message": "ok", "data": { "ok": true } }
```
- `ok:true` → 通过，可重试扫码
- `ok:false` → 失败，前端可重新调 `/api/xhl/captcha` 换一张重试

## 3. 加密说明

`data` 用 **AES-ECB PKCS7 + 双重 base64** 加密（与登录密码加密同一机制），密钥为 `client_aes_key`（客户端写死、XOR 混淆存储）。

客户端已有此能力：`api/crypto.go` 的 `aesEncryptDouble64(data, key)`。

```go
raw, _ := json.Marshal(map[string]any{
    "x": x, "y": y, "trace": trace,
})
enc, _ := aesEncryptDouble64(string(raw), cfg.ClientAESKey())
// data = enc
```

> 前端代码中轨迹/坐标只在内存生成，**不得以明文出现在请求体**。

## 4. 前端交互要点

### 4.1 画布与拼块

- **背景图**：`<img src="bg_base64">`，固定尺寸 250×150（CSS 可缩放显示）。
- **拼图块**：`<img src="piece_base64">`，绝对定位在画布上，初始位置在**左侧起点**（x≈10px），y 对齐用户习惯高度。
- **拖动**：监听鼠标/触摸拖动，拼块跟随 X 方向移动（Y 固定或允许小幅微调）。

### 4.2 记录轨迹

拖动过程中，每次位置变化记录一个点：

```ts
// 全局起始时间
const startTime = Date.now()
const trace: { t: number; x: number; y: number }[] = []

function onMove(x: number, y: number) {
  trace.push({ t: Date.now() - startTime, x, y })
}
```

松手时 `trace` 末尾追加一个停留点（模拟人类减速确认），取 `x = 拼块最终位置`。

### 4.3 提交

松手后调 `verify`，用 `x`（拼块最终 X）、`y`（拼块 Y，对齐缺口）、`trace` 加密上报。

### 4.4 校验约束（前端需满足，否则会被拒）

| 约束 | 要求 |
|---|---|
| 轨迹点数 | ≥ 3 |
| 总时长 | 300ms ~ 5s |
| 无瞬移 | 相邻点 X 位移 ≤ 30px |
| 终点对齐 | 轨迹末点 X 与上报 x 差 ≤ 10px |
| 减速特征 | 轨迹中至少有一次位移变小的减速/停留 |

> 简言之：**真实平滑拖动即可通过**；机器直接跳到目标坐标（瞬移）或轨迹异常会被拒。

## 5. 触发步进配置

服务端按接口配置触发频率（`config.yaml security.captcha_step`）：

```yaml
security:
  captcha_step:
    qrlogin: 20   # 扫码接口每 20 次触发一次验证码（0=不触发）
```

如需调整频率，改服务端配置即可，客户端无需改动。

## 6. 完整示例（伪代码）

```ts
async function doQrLogin() {
  const resp = await post('/api/xhl/qrlogin', payload)
  if (resp.code === 1021) {
    await doSlideCaptcha()          // 弹拼图面板
    await doQrLogin()               // 验证通过后重试
  }
}

async function doSlideCaptcha() {
  const cap = await post('/api/xhl/captcha', {})
  showPanel(cap.data.bg_base64, cap.data.piece_base64)

  const { x, y, trace } = await waitDragComplete()   // 用户拖动
  const raw = JSON.stringify({ x, y, trace })
  const data = aesEncryptDouble64(raw, clientAesKey)

  const res = await post('/api/xhl/captcha/verify', {
    captcha_id: cap.data.captcha_id,
    data,
  })
  if (!res.data.ok) {
    await doSlideCaptcha()  // 失败换一张重试
  }
}
```
