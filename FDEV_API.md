# fdev 签发服务 · API 文档

> 给第三方/客户端开发者用的独立文档：用平台发放的 **API Key** 获取百度安全 SDK 的设备凭证 **`z_id`**。
> 服务端**只出加密包 + 负责解密**；请求由**你自己的客户端带代理发出**，响应原文回传服务端解密后直接拿 `token`。
> **`FB`（= f(dev)，设备凭证派生结果）只存在服务端**，任何情况下都不下发、不写日志。

| 项 | 值 |
|---|---|
| 基础地址（生产） | `http://103.36.223.143:8888` |
| 所属项目 | `100004` / **fdev签发服务** |
| 鉴权方式 | 请求头 `xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |
| 接口数量 | 2 个（出包 / 解密） |
| 计费 | **出包 1 积分/次**（调用即扣）；**解密不扣费** |

---

## 0. 快速上手（TL;DR）

```text
① POST /api/open/fdev/issue   {android_id, uuid}      → handle + url + body_b64 + headers
② 用你自己的代理：POST url（body = base64decode(body_b64)，headers 用返回的 headers）
                                                     → 拿到 sofire 响应原文 raw
③ POST /api/open/fdev/open    {handle, response_b64=base64(raw)}  → token / st / nt
```

`st == "56"` 且 `token_bytes == 65` 就是签发成功，把 `token` 当作 `z_id` 使用。

---

## 1. 背景与设计

- **要解决的问题**：贴吧私信发图时服务端会校验 `z_id`；缺失/不可信会被 UGC 过滤。`z_id` 需要由设备指纹派生并向百度 `sofire` 服务换取。
- **算法放在服务端**：`f(dev)`（RC4 + 固定异或）与整套加解密只在服务端，客户端拿不到。
- **请求由客户端发出**（本服务**不代发**）：这样签发出口分散在你的代理上，不容易被关联/风控。
- **响应由服务端解密**：解密需要 `FB`，而 `FB` 不下发，所以客户端把响应原文回传，服务端解密后只回 `token`。

两种角色分工：

```
你的客户端                                        fdev 签发服务（本项目）           百度 sofire
   │  POST /api/open/fdev/issue  ───────────────►│
   │                                              │ 生成加密包（内部算 FB，FB 不外传）
   │  ◄── handle + url + body_b64 + headers ──────│
   │                                                                              
   │  POST url（带 headers，走你的本地代理）────────────────────────────────────►│
   │  ◄────────────────────────────────────────────── sofire 响应原文 raw ────────│
   │                                                                              
   │  POST /api/open/fdev/open {handle, raw} ────►│  用 FB 解密
   │  ◄── token / st / nt / valid / token_bytes ──│
```

---

## 2. 鉴权

所有请求带请求头：

```
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

- 缺少 / 无效 / 已禁用 → `1002`
- key 不属于项目 `100004` → `1003`
- 本项目当前 API Key（示例）：`sk-a21f784044b2547545e7808a0b58f24f`（**余额 0，需先充值**）

查询余额（无需鉴权头，key 走 query）：

```
GET /api/open/balance?key=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

```json
{ "code": 0, "message": "ok", "data": { "key": "sk-…", "balance": 5534 } }
```

---

## 3. 接口一：出包

```
POST /api/open/fdev/issue
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

### 3.1 请求参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `android_id` | 二选一 | 设备指纹里的 android_id（16 位 hex） |
| `uuid` | 二选一 | 设备指纹里的 uuid |
| `xyus` | 二选一 | 也可直接给算好的 `xyus`（32 位大写 hex + \|0 后缀） |

任选一种写法：

```json
{ "android_id": "0123456789abcdef", "uuid": "12345678-0000-4000-8000-000000000000" }
```
```json
{ "xyus": "26FFD50C4B43FD44828E9D7C3818326C|0" }
```

### 3.2 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "handle": "9833a27455ecfab73f8a0c2fe10e0fdf",
    "url": "https://sofire.baidu.com/c/11/z/100/200012/1791124739/50a988eb9843a351728f593526e96b32?skey=221U3f8mTItdgmbvug2lMw%3D%3D%0A",
    "body_b64": "…（base64 的密文 body）…",
    "headers": {
      "User-Agent": "x6/200012/15.71.0.10/5.0.9.5",
      "Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
      "x-device-id": "6713aa044389e114bd2f1e08b091d7e0",
      "x-sdk-ver": "sofire/3.7.1.7",
      "x-plu-ver": "x6/5.0.9.5",
      "x-app-ver": "com.baidu.searchbox/15.71.0.10",
      "x-api-ver": "32",
      "Accept": "*/*",
      "Accept-Language": "zh",
      "Accept-Encoding": "gzip",
      "Pragma": "no-cache"
    },
    "x_dev": "6713aa044389e114bd2f1e08b091d7e0",
    "xyus": "26FFD50C4B43FD44828E9D7C3818326C|0",
    "send_within_seconds": 15,
    "cost": 1,
    "balance": 5533
  }
}
```

### 3.3 字段说明

| 字段 | 说明 |
|---|---|
| `handle` | 本次出包凭证，传给 `/open` 解密用；**与创建它的 API Key 绑定**，5 分钟内有效，解密成功后立即失效 |
| `url` | sofire 的目标地址（已含时间戳与签名） |
| `body_b64` | 请求体（base64），**必须先解码成二进制**再作为 POST body |
| `headers` | 发送时必须**逐条设置**这些请求头 |
| `x_dev` | 即 `x-device-id`，回显便于排查 |
| `xyus` | 本次使用的设备串 |
| `send_within_seconds` | 请求内嵌时间戳，**请在 15 秒内发出** |
| `cost` | 本次扣费积分 |
| `balance` | 扣费后余额 |

### 3.4 计费

- **每次调用扣 1 积分，调用即扣**（与后续发送/解密成败无关）。
- 余额不足 → `1020 积分不足`，此时**不扣费**也不会出包。

---

## 4. 接口二：解密（拿 token）

```
POST /api/open/fdev/open
xhlkey: sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

### 4.1 请求参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `handle` | 是 | `/issue` 返回的 handle |
| `response_b64` | 是 | sofire **响应原文**的 base64 |
| `response` | 备选 | 也可直接传响应原文（非 base64 时按原文处理） |

```json
{ "handle": "9833a27455ecfab73f8a0c2fe10e0fdf", "response_b64": "H4sIAAAA…" }
```

### 4.2 成功响应

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

| 字段 | 说明 |
|---|---|
| `token` | 即要用的 `z_id`，87 字符 base64url（解码 65 字节） |
| `st` | 服务端判定码，`"56"` = 审核通过 |
| `nt` | 有效期（秒），实测 1800 |
| `valid` | `st=="56"` 且 `token_bytes==65` |
| `token_bytes` | token 解码后的字节数（应为 65） |

### 4.3 行为

- **不扣费**（余额 0 也能解密）。
- 只允许**创建该 handle 的那个 API Key** 调用，别的 key 用 → `1003`。
- 解密**成功后 handle 立即失效**（一次性）；失败可重试，直到 5 分钟过期。

---

## 5. 错误码

| code | 含义 |
|---|---|
| `0` | 成功 |
| `1001` | 参数错误（缺 `android_id`+`uuid` / `xyus`；缺 `handle` / `response_b64`）；或响应解密失败 |
| `1002` | 缺少 / 无效 / 已禁用的 `xhlkey` |
| `1003` | 不是项目 `100004` 的 API Key；或 handle 不属于当前 API Key |
| `1004` | 项目不存在或已停用；或 handle 不存在/已过期 |
| `1020` | 积分不足（出包扣费时余额不够） |

---

## 6. 客户端完整示例（Python）

```python
import base64, json, requests

BASE = "http://103.36.223.143:8888"
KEY  = "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
# 你的本地代理（服务端不代发，出口在你这边）
PROXIES = {
    # http 代理：
    "http":  "http://user:pass@host:port",
    "https": "http://user:pass@host:port",
    # 或 socks5（需 pip install requests[socks]）：
    # "http":  "socks5h://user:pass@host:port",
    # "https": "socks5h://user:pass@host:port",
}

# ① 出包
r = requests.post(f"{BASE}/api/open/fdev/issue",
                  headers={"xhlkey": KEY},
                  json={"android_id": "0123456789abcdef",
                        "uuid": "12345678-0000-4000-8000-000000000000"},
                  timeout=20).json()
assert r["code"] == 0, r
d = r["data"]
print("handle:", d["handle"], "余额:", d["balance"])

# ② 用本地代理发出（注意 body 要 base64 解码成二进制）
raw = requests.post(d["url"], data=base64.b64decode(d["body_b64"]),
                    headers=d["headers"], proxies=PROXIES, timeout=20).content

# ③ 回传响应原文，服务端解密
r2 = requests.post(f"{BASE}/api/open/fdev/open",
                   headers={"xhlkey": KEY},
                   json={"handle": d["handle"],
                         "response_b64": base64.b64encode(raw).decode()},
                   timeout=20).json()
assert r2["code"] == 0, r2
t = r2["data"]
print(f'st={t["st"]} nt={t["nt"]} token={len(t["token"])} 字符 / {t["token_bytes"]} 字节')
if t["valid"]:
    z_id = t["token"]          # ← 拿去用
```

> 发送那一步务必**逐条设置** `headers`；用 `requests` 时直接 `headers=d["headers"]` 即可。
> `body_b64` 要解码成字节，不要当字符串发。

---

## 7. 安全约束（必读）

1. **`FB` 永不出服务端**：接口不会返回任何密钥材料，只回 URL/Body/Headers 与最终 `token`。
2. **一台设备一套 `xyus`**：不要把同一个 `dev` 分给一批账号使用（等于共用设备凭证，会关联）。
3. **请求包不能缓存复用**：内含 `now_ms` / `ts`，`send_within_seconds`（15s）内必须发出。
   **可以缓存的是最终 `token`**（`nt`≈1800s）。
4. **handle 与 API Key 绑定且一次性**：泄露 handle 给别的 key 也用不了；解密成功即失效。
5. **不要在日志里打印 `body_b64` 之外的东西**：`handle` 本身不敏感，但别把整个响应体长期留存。
6. `skey` 末尾的换行（URL 里是 `%0A`）是协议的一部分，**不要手工改写 URL**，直接用返回的 `url`。

---

## 8. 常见问题

| 现象 | 原因 / 处理 |
|---|---|
| `1020 积分不足` | 该 API Key 余额为 0，去后台充值 |
| `1003 仅项目 100004…` | 用了别的项目的 key（如 100001 的） |
| `1004 handle 不存在或已过期` | handle 超过 5 分钟；或已被成功解密过一次（一次性）；重新 `/issue` |
| `1001 解密失败：…` | 回传的不是 sofire 响应原文（比如传了 JSON 包装/被截断）；要传**原始字节** |
| `st` 不是 `"56"` | 设备凭证未被认可：确认用的 device 指纹真实、且该 `dev` 没被多个号共用 |
| `token_bytes` 不是 65 | 同上；`valid=false` 时不要使用该 token |
| 直接访问 `url` 报 403/超时 | 该请求必须带返回的 `headers`，并且建议走你的代理出口 |

---

## 9. 实现位置（服务端）

| 文件 | 作用 |
|---|---|
| `internal/fdev/fdev.go` | z_id 签名/加解密算法（纯标准库，无 cgo） |
| `internal/fdev/fdev_test.go` | 9 个已知向量 + 出包结构 + 出包/解密往返单测；联网 live 用例默认跳过（`FDEV_LIVE=1` 才跑） |
| `internal/handler/fdev.go` | 本服务两个接口的实现、计费、handle 暂存与鉴权 |
| `internal/router/router.go` | 路由注册 `/api/open/fdev/{issue,open}` |
| `internal/config/config.go` | `cost.fdev_issue_cost` 计费配置 |

自检命令：

```bash
go test ./internal/fdev -v                                   # 向量 + 往返（不联网）
FDEV_LIVE=1 go test ./internal/fdev -run TestLiveIssue -v    # 真打 sofire，期望 st=56
```

---

## 10. 变更记录

| 版本/时间 | 变更 |
|---|---|
| 初版 | `/api/open/fdev/issue` + `/api/open/fdev/rkey`（服务端只出包，客户端本地解密） |
| 修订 | **删除 `/rkey`**（`rkey = resp_skey XOR FB`，`resp_skey` 由调用方自填，发全 0 即可反推 `FB` 并解开请求明文）→ 改为服务端解密 `/open`；handle 绑定 API Key + 一次性 |
| 修订 | 计费：**出包 1 积分/次**，解密不扣费 |
