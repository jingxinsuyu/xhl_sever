# 小火龙工具箱 · 开放平台接口文档

> 面向第三方开发者：通过 **API Key** 调用扫码确认接口，实现自动登录确认。
> 调用方只需携带 `xhlkey` 请求头鉴权，普通 JSON 请求 / 响应，无需加密。

---

## 1. API Key

### 1.1 获取

联系平台管理员创建 API Key（也可在管理后台自行创建）。key 格式为 `sk-` 前缀 + 32 位十六进制字符，例如：

```
sk-56f50dc7d387f7a1352a06a42758128f
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

## 4. 查询剩余积分

### 4.1 接口

```
GET /api/open/balance?key=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

> 功能：查询指定 API Key 的剩余积分。每次扫码确认（`/api/open/qrlogin`）按配置扣除积分（当前 1 积分/次，调用即扣）；积分不足返回 `1020`。
> 本接口无需鉴权头，直接以 query 参数传 key。

### 4.2 请求参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `key` | 是 | API Key（`sk-` 开头） |

### 4.3 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "key": "sk-56f50dc7d387f7a1352a06a42758128f",
    "balance": 9985
  }
}
```

| 字段 | 说明 |
|---|---|
| `key` | 查询的 API Key |
| `balance` | 剩余积分 |

### 4.4 失败响应

- key 为空：`{ "code": 1001, "message": "参数错误：key 不能为空" }`
- key 不存在 / 已删除：`{ "code": 1004, "message": "API Key 不存在" }`

---

## 5. 调用示例

### cURL

```bash
curl -X POST http://103.36.223.143:8888/api/open/qrlogin \
  -H "xhlkey: sk-56f50dc7d387f7a1352a06a42758128f" \
  -H "Content-Type: application/json" \
  -d '{"qrUrl":"https://wappass.baidu.com/wp/?qrlogin&sign=xxxx&lp=pc","ck":"BDUSS=xxx; STOKEN=xxx"}'
```

### Python

```python
import requests

url = "http://103.36.223.143:8888/api/open/qrlogin"
headers = {"xhlkey": "sk-56f50dc7d387f7a1352a06a42758128f"}
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

### 查询余额

```bash
curl "http://103.36.223.143:8888/api/open/balance?key=sk-56f50dc7d387f7a1352a06a42758128f"
```

```python
import requests

key = "sk-56f50dc7d387f7a1352a06a42758128f"
resp = requests.get(f"http://103.36.223.143:8888/api/open/balance?key={key}", timeout=30).json()
if resp["code"] == 0:
    print(f"剩余积分: {resp['data']['balance']}")
else:
    print("查询失败:", resp.get("message"))
```

---

## 6. 注意事项

1. **BDUSS 必填**：`ck` 中缺少 `BDUSS` 时确认会失败（`data.ok=false`）。
2. **二维码一次性**：一个二维码确认成功后即被消费，再次确认同一 `qrUrl` 会失败；应在每次检测到新二维码时传最新的 `qrUrl`。
3. **调用频率**：请勿高频空跑，异常调用可能触发服务端风控。
4. **积分消耗**：每次调用 `/api/open/qrlogin` 扣 1 积分（调用即扣，与确认成败无关）；积分不足返回 `1020`。可通过 `GET /api/open/balance` 查询余额，联系管理员充值。
