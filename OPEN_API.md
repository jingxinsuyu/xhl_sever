# 小火龙工具箱 · 开放平台接口文档

> 面向第三方开发者：通过 **API Key** 调用扫码确认接口，实现自动登录确认。
> 调用方只需携带 `xhlkey` 请求头鉴权，普通 JSON 请求 / 响应，无需加密。

---

## 1. API Key

### 1.1 获取

联系平台管理员创建 API Key（也可在管理后台自行创建）。key 格式为 `sk-` 前缀 + 32 位十六进制字符，例如：

```
sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 1.2 鉴权方式

所有开放接口在请求头携带：

```
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

- 缺少或无效的 key → 返回 `1002`。
- key 被禁用 / 删除 → 同样返回 `1002`。

---

## 2. 基础约定

### 2.1 基础地址

| 环境 | Base URL |
|---|---|
| 生产 | `http://103.36.223.143:8888` |

### 2.2 响应结构

统一 JSON（HTTP 200，业务结果用 `code` 区分）：

```json
{ "code": 0, "message": "ok", "data": { } }
```

### 2.3 业务错误码

| code | 含义 |
|---|---|
| 0 | 成功 |
| 1001 | 参数错误 |
| 1002 | 缺少 / 无效 / 已禁用的 xhlkey |
| 1003 | 无权限（该接口仅项目 100001 的 key 可调用） |
| 1004 | 项目不存在 / key 不存在 |
| 1016 | 服务端网络环境加载失败 |
| 1020 | 积分不足 |

---

## 3. 扫码确认

### 3.1 接口

```
POST /api/open/qrlogin
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

> 功能：对手机百度 App 上**已扫码待确认**的二维码，用目标账号的 cookie 代为确认登录。
> 一次调用确认一个二维码，同步返回结果。

### 3.2 请求参数

```json
{
  "qrUrl": "https://wappass.baidu.com/wp/?qrlogin&sign=xxxx&lp=pc",
  "ck": "BDUSS=xxx; STOKEN=xxx"
}
```

| 参数 | 必填 | 说明 |
|---|---|---|
| `qrUrl` | 是 | 二维码内容（登录链接，须含 `sign`；可选 `lp`） |
| `ck` | 是 | 目标百度账号的 cookie 串，**至少包含 `BDUSS`**（缺 BDUSS 将确认失败） |

### 3.3 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "ok": true,
    "errno": "0",
    "code": "0",
    "message": ""
  }
}
```

`data` 字段说明：

| 字段 | 说明 |
|---|---|
| `ok` | `true` 确认成功；`false` 确认失败（二维码已消费 / 凭证失效 / 风控等） |
| `errno` | 服务端返回的错误码（字符串） |
| `code` | 服务端返回的状态码 |
| `message` | 结果信息（可为空） |

### 3.4 失败响应

- 参数 / 鉴权 / 项目校验失败（HTTP 200，`code` 非 0）：

```json
{ "code": 1002, "message": "缺少 xhlkey 请求头" }
```

- 确认失败：HTTP 200，`code=0` 但 `data.ok=false`：

```json
{
  "code": 0,
  "message": "ok",
  "data": { "ok": false, "errno": "-1", "code": "400202", "message": "二维码已过期" }
}
```

---

## 4. BDUSS 转网盘 Cookie

### 4.1 接口

```
POST /api/open/netdisk-cookie
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

> 功能：用传入 cookie 里的 `BDUSS`（+ 可选 `PTOKEN`）调用百度 passport auth 接口，换取该账号的**网盘（netdisk）专属 STOKEN**，并组装成可直接用于百度网盘的完整 cookie 返回。
>
> 限制：**仅项目 100001（小火龙扫码登录器）发放的 API Key 可调用**；其他项目 key 返回 `1003`。
> 积分：每次调用扣 **1 积分**（调用即扣，与换出成败无关）；积分不足返回 `1020`。
> 说明：换出成功后，**调用方传入的原 ck** 会按扫码同款方式存入平台的 cookie 库（来源 = `开放平台:<key名>`），可由管理员在后台筛选 / 发放。

### 4.2 请求参数

```json
{
  "ck": "BDUSS=xxx; PTOKEN=xxx; STOKEN=xxx; BAIDUID=xxx"
}
```

`ck` 的传法与扫码接口（`/api/open/qrlogin`）完全一致，直接传完整 cookie 串即可。

| 参数 | 必填 | 说明 |
|---|---|---|
| `ck` | 是 | 完整百度 cookie 串。后端只取其中的 `BDUSS`（必填）与 `PTOKEN`（可选，有则换出的 cookie 更完整），其余字段（如 `BAIDUID`/`STOKEN`）忽略 |

### 4.3 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "ok": true,
    "cookie": "BDUSS=xxx; PTOKEN=xxx; STOKEN=1cb205...",
    "netdisk_stoken": "1cb205...",
    "balance": 9984
  }
}
```

`data` 字段说明：

| 字段 | 说明 |
|---|---|
| `cookie` | 可直接用于百度网盘的完整 cookie（`BDUSS` + 可选 `PTOKEN` + 网盘 `STOKEN`） |
| `netdisk_stoken` | 换出的网盘专属 STOKEN（与 passport/贴吧的 STOKEN 不同） |
| `balance` | 扣费后剩余积分 |

### 4.4 失败响应

```json
{ "code": 1001, "message": "参数错误：ck 不能为空" }
{ "code": 1001, "message": "ck 中缺少 BDUSS" }
{ "code": 1003, "message": "仅项目 100001 的 API 可调用" }
{ "code": 1020, "message": "积分不足" }
```

- BDUSS 失效 / 未开通网盘：HTTP 200，`code=1001`，`message` 为百度返回的错误、`BDUSS 换取失败（可能已失效）` 或 `未返回网盘 STOKEN，可能该 BDUSS 未开通网盘`。
- 以上失败均属「调用即扣」，积分已在调用时扣除；**仅成功后**才把传入的原 ck 存入 cookie 库。

---

## 5. 查询剩余积分

### 5.1 接口

```
GET /api/open/balance?key=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

> 功能：查询指定 API Key 的剩余积分。扫码确认（`/api/open/qrlogin`）与网盘转换（`/api/open/netdisk-cookie`）均按 1 积分/次扣除（调用即扣）；积分不足返回 `1020`。
> 本接口无需鉴权头，直接以 query 参数传 key。

### 5.2 请求参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `key` | 是 | API Key（`sk-` 开头） |

### 5.3 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "key": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
    "balance": 9985
  }
}
```

| 字段 | 说明 |
|---|---|
| `key` | 查询的 API Key |
| `balance` | 剩余积分 |

### 5.4 失败响应

- key 为空：`{ "code": 1001, "message": "参数错误：key 不能为空" }`
- key 不存在 / 已删除：`{ "code": 1004, "message": "API Key 不存在" }`

---

## 6. 调用示例

### cURL

```bash
curl -X POST http://103.36.223.143:8888/api/open/qrlogin \
  -H "xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"qrUrl":"https://wappass.baidu.com/wp/?qrlogin&sign=xxxx&lp=pc","ck":"BDUSS=xxx; STOKEN=xxx"}'
```

### Python

```python
import requests

url = "http://103.36.223.143:8888/api/open/qrlogin"
headers = {"xhlkey": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
data = {
    "qrUrl": "https://wappass.baidu.com/wp/?qrlogin&sign=xxxx&lp=pc",
    "ck": "BDUSS=xxx; STOKEN=xxx",
}
resp = requests.post(url, json=data, headers=headers, timeout=30).json()
if resp["code"] == 0 and resp["data"]["ok"]:
    print("登录确认成功")
else:
    print("失败:", resp.get("message") or resp["data"].get("message"))
```

### BDUSS 转网盘 Cookie

```bash
curl -X POST http://103.36.223.143:8888/api/open/netdisk-cookie \
  -H "xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"ck":"BDUSS=xxx; PTOKEN=xxx; BAIDUID=xxx"}'
```

```python
import requests

url = "http://103.36.223.143:8888/api/open/netdisk-cookie"
headers = {"xhlkey": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
data = {"ck": "BDUSS=xxx; PTOKEN=xxx; BAIDUID=xxx"}
resp = requests.post(url, json=data, headers=headers, timeout=30).json()
if resp["code"] == 0 and resp["data"]["ok"]:
    print("网盘 cookie:", resp["data"]["cookie"])
else:
    print("失败:", resp.get("message"))
```

### 查询余额

```bash
curl "http://103.36.223.143:8888/api/open/balance?key=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

```python
import requests

key = "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
resp = requests.get(f"http://103.36.223.143:8888/api/open/balance?key={key}", timeout=30).json()
if resp["code"] == 0:
    print(f"剩余积分: {resp['data']['balance']}")
else:
    print("查询失败:", resp.get("message"))
```

---

## 7. 注意事项

1. **BDUSS 必填**：`ck` 中缺少 `BDUSS` 时确认会失败（`data.ok=false`）。
2. **二维码一次性**：一个二维码确认成功后即被消费，再次确认同一 `qrUrl` 会失败；应在每次检测到新二维码时传最新的 `qrUrl`。
3. **调用频率**：请勿高频空跑，异常调用可能触发服务端风控。
4. **积分消耗**：每次调用 `/api/open/qrlogin` 或 `/api/open/netdisk-cookie` 扣 1 积分（调用即扣，与成败无关）；积分不足返回 `1020`。可通过 `GET /api/open/balance` 查询余额，联系管理员充值。
5. **网盘转换仅限项目 100001**：`/api/open/netdisk-cookie` 只接受项目 100001 发放的 API Key，其他项目调用返回 `1003`。成功后传入的原 ck 会存入平台 cookie 库（与扫码同款），如需批量导出可联系管理员。

---

## 8. fdev 签发服务（百度 sofire `z_id` 加密出包）

> 面向项目 **100004（fdev签发服务）** 发放的 API Key。
> 服务端**出加密包**，并**负责解密响应**：请求由调用方用**本地代理**发出，响应原文回传服务端解密，直接拿到 `token`。
> `FB`（= f(dev)，设备凭证派生结果）**只存在服务端**，绝不下发、绝不写日志。

### 8.1 出包

```
POST /api/open/fdev/issue
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

请求参数（`android_id`+`uuid` 二选一，或直接给 `xyus`）：

| 参数 | 必填 | 说明 |
|---|---|---|
| `android_id` | 二选一 | 客户端指纹里的 android_id |
| `uuid` | 二选一 | 客户端指纹里的 uuid |
| `xyus` | 二选一 | 直接传算好的 xyus（32 位大写 hex + \|0 后缀） |

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "handle": "9833a27455ecfab73f8a0c2fe10e0fdf",
    "url": "https://sofire.baidu.com/c/11/z/100/200012/{ts}/{md5}?skey=...%0A",
    "body_b64": "…",
    "headers": { "User-Agent": "…", "Content-Type": "…", "x-device-id": "…", "x-sdk-ver": "…" },
    "x_dev": "6713aa044389e114bd2f1e08b091d7e0",
    "xyus": "26FFD50C4B43FD44828E9D7C3818326C|0",
    "send_within_seconds": 15,
    "cost": 1,
    "balance": 5534
  }
}
```

- 调用方把 `body_b64` 解码后，带上 `headers` 里的头，把请求 POST 到 `url`（**用本地代理发**）。
- 请求里内嵌时间戳（body 的 `now_ms`、URL 的 `ts`），**请在 `send_within_seconds`（15 秒）内发出**。
- `handle` 是本次出包的凭证，**与创建它的 API Key 绑定**，服务端按它暂存 `FB` **5 分钟**，供 8.2 解密。
- **计费：本接口（出包）每次扣 1 积分**，调用即扣、与后续成败无关；响应里的 `cost` 是本次扣费、`balance` 是扣后余额；积分不足返回 `1020`。**解密接口 `/open` 不扣费。**

### 8.2 服务端解密（拿 token）

```
POST /api/open/fdev/open
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json

{ "handle": "…", "response_b64": "sofire 响应原文的 base64" }
```

> `response_b64` 也可写成 `response` 直接传响应原文（非 base64 时按原文处理）。

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "…87 字符 base64url…",
    "st": "56",
    "nt": 1800,
    "valid": true,
    "token_bytes": 65,
    "xyus": "26FFD50C4B43FD44828E9D7C3818326C|0"
  }
}
```

- 服务端用**只存在于服务端的 `FB`** 解密（AES-128-CBC 零 IV → 去 PKCS7 → 可能 gunzip → 合并多个 JSON），只回 `token/st/nt`。
- `st == "56"` 且 `token_bytes == 65`（即 `token` 为 87 字符 base64url）才算签发通过。
- 解密**成功后该 handle 立即失效**（一次性）；失败可重试，直到 5 分钟过期。
- 该 handle **只有创建它的那个 API Key 能用**（别的 key 用会返回 1003）。
- 本接口**不扣费**。

### 8.3 失败响应

| code | 含义 |
|---|---|
| 1001 | 参数错误（缺 android_id+uuid / xyus，或 handle、response_b64）；或响应解密失败 |
| 1002 | 缺少 / 无效 / 已禁用的 xhlkey |
| 1003 | 不是项目 100004 的 API Key；或该 handle 不属于当前 API Key |
| 1004 | 项目不存在或已停用；或 handle 不存在/已过期 |
| 1020 | 积分不足（出包扣费时余额不够） |

### 8.4 调用示例

```bash
KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# 1) 出包
curl -s -X POST http://103.36.223.143:8888/api/open/fdev/issue \
  -H "xhlkey: $KEY" -H "Content-Type: application/json" \
  -d '{"android_id":"0123456789abcdef","uuid":"12345678-0000-4000-8000-000000000000"}'

# 2) 用本地代理发出：body = base64decode(data.body_b64)，headers 用 data.headers，POST 到 data.url
#    拿到响应原文 raw

# 3) 回传响应原文，服务端解密拿 token
curl -s -X POST http://103.36.223.143:8888/api/open/fdev/open \
  -H "xhlkey: $KEY" -H "Content-Type: application/json" \
  -d "{\"handle\":\"<上一步 handle>\",\"response_b64\":\"$(printf '%s' "$raw" | base64 -w0)\"}"
```

### 8.5 注意

1. **一台设备一套 xyus**：不要把同一个 `dev` 分给一批号使用（等于共用设备凭证，会关联）。
2. **不缓存请求包**：请求包有时效，生成后尽快发；可缓存的是最终 `token`（`nt`≈1800s）。
3. **`FB` 永不出服务端**：接口只回 URL/Body/Headers 与最终 token，不下发任何密钥材料。
4. **计费**：`/api/open/fdev/issue`（出包）每次扣 **1 积分**，调用即扣；`/api/open/fdev/open`（解密）**不扣费**。改价在 `config.yaml` 的 `cost.fdev_issue_cost`（0=不扣）。
