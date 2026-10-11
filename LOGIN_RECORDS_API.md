# 登录记录查询 · 接口文档（客户端本机执行 + 服务端授权计费）

> 项目：**100006 小火龙登录记录查询**（按次计费，每次 1）
> 代码：`internal/handler/loginrecords.go`（接口/票据/计费）、`internal/router/router.go`（路由）
> 协议配置：`config.yaml` → `login_records` 段
> 端到端验证脚本：`_verify_login_records.py`

---

## 0. 一句话架构

**服务端不产生任何上游流量。** 上游请求由**客户端用本机 IP** 发出；服务端只负责
「鉴权 → 额度预检 → 下发一张短时效一次性授权票据（票里带上游请求拼装指令 + 响应解析规则）
→ 客户端执行 → 客户端回报结果 → 成功才扣费」。

原因：上游出口 IP 集中调用会被风控封掉（服务端一台机器的 IP 去扫成千上万个账号，
几乎必然被限流/封禁）。把查询散到每个客户端自己的家宽/4G IP 上，才是这套业务能跑通的前提。

```
客户端                          服务端                        上游(百度 passport)
  │  POST /login-records/prepare │
  │  {ck}                        │ 鉴权 + 额度预检 + 日限额
  ├─────────────────────────────►│ 签票据(HMAC, nonce, exp, uid/pid/设备码, ck指纹)
  │◄─────────────────────────────┤ ticket + request指令 + parse规则（**不含 ck 明文**）
  │                                                       ▲
  │  按指令拼装 URL/headers（用本机 IP 直连）              │
  ├───────────────────────────────────────────────────────┘
  │  POST /login-records/report  │
  │  {ticket, ok, upstream_code} │ 验签/有效期/归属/一次性 nonce
  ├─────────────────────────────►│ 成功 → 扣额度 + billing_log；成功失败都写 call_log
  │◄─────────────────────────────┤ {ok, billed, cost, remaining}
```

---

## 1. 上游是谁（勘测结论，来自只读勘测 `小火龙账号管理器v2`）

| 项 | 值 |
|---|---|
| 上游 | **百度 passport**（不是自建后端、不是第三方中转） |
| URL | `https://passport.baidu.com/v3/api/user/loginrecord` |
| 固定 query | `client=pc&clientfrom=pc&lang=zh-cn&tpl=pp&ttt=<毫秒时间戳>` |
| method | `GET` |
| 认证 | **整串 Cookie（其中必须有 `BDUSS`）**，即被查询账号自己的登录态 |
| 签名/加密 | **无**。没有 sign、没有 salt、没有 AES、没有时间戳校验（`ttt` 只是防缓存） |
| 响应 | `{"code":110000,"data":{"list":[{"tplName":"…","login_type":"…","device":"…","normal":0}, …]}}` |
| 分页 | **无分页**，一次返回该账号全部记录（`list`） |
| 成功码 | `110000`（另兼容 `0`） |
| 错误码 | `400021` BDUSS 已过期/登录态失效；`50000` 触发风控；`160102` BDUSS 为空；`12` 用户信息不存在；`-202` 用户信息获取失败 |
| 参考实现 | `小火龙账号管理器v2/baidu/accountcheck/check.go` 的 `LoginRecord()` / `parseLoginRecord()` |

**参考项目里有没有内嵌密钥？** 有，但**不是这个接口的**：

- `loginui/cfg/config.go`：后端地址 `http://103.36.223.143:8888`、项目 id `100003` 写死在代码里；
  `client_aes_key` / `client_sign_salt` 以 XOR 混淆（`obfMask` + hex）内嵌在客户端——**客户端能反解出来**。
- `baidu/accountcheck/check.go`：`signSalt = "tiebaclient!!!"`、固定设备 `imei/z_id/_client_id`——
  这些是**其它**接口（`initNickname` / `tbs`）要的，**loginrecord 用不到**。

**上游协议本身不需要任何共享密钥**：唯一凭据就是调用方自己的 ck。所以本接口的「密钥」
只可能是「协议参数」（域名、UA、query、解析规则），把它放服务端下发的价值是
**协议变了不用重发客户端**，而不是「藏一个客户端算不出来的秘密」。

**参考项目本地有没有自己的数据？** 有，但登录记录是**纯转发**：
`data.db`(SQLite) 存账号，扫描结果（含登录记录列）序列化进 `scan_accounts.txt`，
服务端不存、不缓存、不做二次加工。

---

## 2. 两个接口

两个接口都在**用户端鉴权中间件 `AuthUser`** 之下，计费项目取 token 里的 `pid`
（老 token 无 `pid` 时兜底 `100006`）。

### 2.1 `POST /api/xhl/login-records/prepare` —— 申请授权票据

```
Header: Authorization: Bearer <用户 token>
Content-Type: application/json
Body:   { "ck": "BAIDUID=…; BDUSS=…; STOKEN=…" }     // 字段也兼容 cookie / bduss
```

服务端做的事（**不发任何上游请求**）：参数校验 → 额度预检 → 项目日调用限额 →
签票据（HMAC）→ 返回指令。

成功返回：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "ticket": "<base64url(payload)>.<hex hmac>",
    "nonce": "3f1c…",                    // 一次性标识，票据里也有
    "expires_at": 1770000000,
    "expires_in": 120,
    "project_id": "100006",
    "billing": {
      "mode": "per_call", "kind": "calls", "unit": 1,
      "charged_on": "report",
      "note": "成功才扣费：/report 里 ok=true 且 upstream_code 命中 parse.success_codes 才扣；失败只记 call_log"
    },
    "request": {
      "method": "GET",
      "url": "https://passport.baidu.com/v3/api/user/loginrecord",
      "query": { "client": "pc", "clientfrom": "pc", "lang": "zh-cn", "tpl": "pp", "ttt": "${TS_MS}" },
      "query_string": "client=pc&clientfrom=pc&lang=zh-cn&tpl=pp&ttt=${TS_MS}",
      "headers": {
        "Cookie": "${CK}",
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) …Chrome/69.0.3497.100…",
        "Content-Type": "application/x-javascript;charset=utf-8"
      },
      "placeholders": {
        "${CK}": "调用方这次提交的 ck 原串（整串 cookie），装进 Cookie 头",
        "${TS_MS}": "发起请求那一刻的毫秒时间戳（字符串）"
      },
      "timeout_seconds": 15,
      "note": "由客户端用本机 IP 直连上游；服务端不代发请求，也不产生上游流量"
    },
    "parse": {
      "format": "json",
      "code_field": "code",
      "success_codes": [110000, 0],
      "records_path": "data.list",
      "name_field": "tplName",
      "login_type_field": "login_type",
      "device_field": "device",
      "name_fallback": "name_field 为空串时，退化为「未知应用(login_type)」；再空则「未知应用(device)」；都空则「未知应用」",
      "error_codes": {
        "400021": "BDUSS 已过期/登录状态失效（账号离线）",
        "50000": "触发风控", "160102": "BDUSS 为空",
        "12": "用户信息不存在", "-202": "用户信息获取失败"
      }
    },
    "report_url": "/api/xhl/login-records/report"
  }
}
```

**客户端要做的事**：把 `${CK}` 换成自己这次提交的 ck、`${TS_MS}` 换成当前毫秒时间戳，
按 `method` + `url` + `query_string` + `headers` 用**本机 IP** 发一次请求；
按 `parse` 规则解析响应；然后调 `/report`。

> `name_fallback` 这条很重要：实测百度对没识别出的应用会返回 `tplName: ""`，
> 只取 `tplName` 会把整条记录丢掉，表现为「状态成功但登录记录一片空白」。

### 2.2 `POST /api/xhl/login-records/report` —— 回报执行结果（成功才扣费）

```
Header: Authorization: Bearer <用户 token>
Body:   { "ticket": "…", "ok": true, "upstream_code": 110000, "record_count": 3 }
```

| 字段 | 说明 |
|---|---|
| `ticket` | **必填**，`/prepare` 原样回传，一个字节都不能改（签名覆盖整个载荷） |
| `ok` | 客户端是否成功发出请求并解出响应（网络失败/超时传 `false`） |
| `upstream_code` | 上游 `code`，数字或数字字符串都收。**扣费判定用它**，防止「什么都不做就报成功」 |
| `record_count` | 解出的记录条数，仅留痕 |
| `error` | 失败摘要（写进 call_log 的 message） |

**判定成功（= 扣费）的条件：`ok == true` 且 `upstream_code` 命中 `parse.success_codes`。**
两者缺一不扣：只看 `ok` 会被「空跑报成功」白扣客户钱，只看 `code` 又分不清网络失败。

返回：

```json
{ "code": 0, "message": "ok",
  "data": { "ok": true, "billed": true, "billing_mode": "per_call",
            "cost": 1, "remaining": 4, "upstream_code": "110000", "record_count": 3 } }
```

`billed=false` 表示这次没扣（上游失败 / 会员制项目只累计次数）。

---

## 3. 票据（ticket）说明

载荷（JSON，`base64url` 编码后拼接签名）：

| 字段 | 含义 |
|---|---|
| `ver` | 票据版本（当前 1） |
| `nonce` | 一次性标识（16 字节随机 hex），`/report` 用 Redis `SETNX` 原子占位，重复上报直接拒 |
| `uid` / `username` | 归属用户 |
| `pid` | 归属项目（决定计费与额度） |
| `machine_code` | 该用户在该项目已绑定的设备码（服务端从 `user_binding` 读，客户端无法伪造） |
| `ck_hash` | `sha256(ck)` 指纹——**票据里只放指纹，不放 ck 原文** |
| `action` | 固定 `login-records`（防票据被挪用到别的接口） |
| `iat` / `exp` | 签发/过期（Unix 秒），`exp - iat` = `login_records.ticket_ttl_seconds` |

签名：

```
body = base64url(json(payload))                    # 无 padding 的 URL-safe base64
sig  = hex(HMAC_SHA256(ticket_secret, body))
ticket = body + "." + sig
```

`/report` 校验顺序（任一不过 → 全部记一条失败的 call_log）：
签名（常数时间比较）→ 版本 → `action` → 是否过期 → 签发时间是否异常（> 现在 +60s）→
`uid`/`pid` 是否等于当前 token → 额度 → **一次性 nonce**（Redis `SETNX`，TTL = 票据有效期 + 60s）。

**非一次性不生效的重复上报会被拒（1024），且不会二次扣费。**

---

## 4. 计费语义（写明）

- 计费模式与单价取**项目生效模式 + 项目单价**（`user_entitlement` 覆盖优先，否则 `project_setting`），
  与 `figure.go` 完全同口径：`membership` 不扣次数只累计调用；`per_call` 扣 `remaining_calls`；
  `credits` 扣 `credits`。
- **成功才扣费**（`charged_on = report`）。理由：上游 `loginrecord` 是只读查询，
  `400021`（BDUSS 失效）/`50000`（风控）这类失败对调用方没有价值；与「设置虚拟形象成功才扣」
  的既有先例一致，也避免客户为死号投诉。
- 参数错、额度不足、票据无效/过期/重放、**上游失败** → **一律不扣费**。
- 扣费时写 `billing_log`（`kind=calls|credits`、`delta=-unit`、`reason=call`、`remark=登录记录查询成功`）；
  `membership` 项目只累计 `total_calls`，不写账本（与既有实现一致）。
- 无论成功失败都写 `call_log`：`action='login-records'`、`source='user'`、`ok`、`errno`(上游码)、
  `message`、`cost_kind`/`cost`、耗时、IP。

调用记录/扣费记录在后台「项目 → 调用记录 / 扣费记录」里按项目 100006 可见
（动作显示为 `login-records`；后台前端的动作**筛选下拉**是已打包的旧版本，没有这一项，
用「全部动作」即可看到，筛选下拉要等前端重新构建）。

---

## 5. 协议参数放在 config.yaml 的哪一段 / 怎么轮换

```yaml
login_records:
  upstream_url: ""          # 上游地址；留空 = 内置 https://passport.baidu.com/v3/api/user/loginrecord
  method: "GET"
  query: {}                 # 附加 query（值里 ${TS_MS} 由客户端替换）
  headers: {}               # 附加请求头（${CK}=调用方 ck、${UPSTREAM_KEY}=upstream_key）
  user_agent: ""            # 留空 = 内置照抓包 UA
  timeout_seconds: 15       # 给客户端的建议超时（1..60）
  success_codes: []         # 视为「查到记录」的上游 code；留空 = [110000, 0]
  ticket_secret: ""         # 票据 HMAC 密钥；**留空回退 security.client_sign_salt**
  ticket_ttl_seconds: 120   # 票据有效期（10..600）
  upstream_key: ""          # 可选上游密钥片段，以 ${UPSTREAM_KEY} 注入下发的 query/headers
```

**保管**：
- `ticket_secret` 只从 `config.yaml` 读，**不写进代码、不落库、不写日志、不回传给客户端之外的任何地方**
  （客户端只拿到签名结果，拿不到密钥本身）。留空时回退 `security.client_sign_salt`（本来就只在服务端）。
- 上游 `loginrecord` **不需要共享密钥**，所以没有「上游密钥」可保管；唯一凭据是调用方自己的 ck，
  服务端**不存 ck**（只在票据里放 `sha256(ck)` 指纹），日志里也不写 ck。
- `upstream_key` 是为「将来换成需要账号/密钥的上游」留的钩子：填了会以 `${UPSTREAM_KEY}`
  注入到下发的 query/headers。

**轮换**：
- 换票据密钥：改 `login_records.ticket_secret`（或 `security.client_sign_salt`）→ 重启服务。
  旧票据立即失效（客户端重取即可），已扣费记录不受影响。
- 换上游域名/参数/解析规则：改 `login_records.upstream_url` / `query` / `headers` / `success_codes`
  → 重启服务。**客户端不用重新发版**——这正是「协议放服务端下发」的意义。
- 换上游密钥（若将来需要）：改 `login_records.upstream_key` → 重启服务。

---

## 6. 错误码

业务码（`util/response.go`）：

| code | 含义 |
|---|---|
| 0 | 成功 |
| 1001 | 参数错误：`ck` 为空 / `ck` 缺少 `BDUSS` / `ck` 过长(>4096) / `report` 缺 `ticket` |
| 1002 | 未登录 / token 无效或过期 |
| 1016 | 无该项目权限（会员制项目未开通/已过期） |
| 1019 | 今日调用次数已达上限（项目 `call_limit`，按用户+项目+接口 每日 Redis 计数） |
| 1023 | 额度不足（按次/积分模式） |
| 1024 | 票据无效 / 已过期 / 已被使用（重放） / 归属不匹配 / 签名不匹配 |
| 1005 | 系统错误（票据签名密钥未配置、Redis 不可用等） |

上游 `code` 由客户端在 `/report` 里回报，原样记进 `call_log.errno` 并在 `parse.error_codes` 里给释义，
**不占用业务码空间**。

---

## 7. 防御与诚实边界（必读）

**能挡住的**：

1. **重放**：票据一次性（Redis `SETNX` 原子占位）+ 短时效（默认 120s）。
2. **跨设备/跨人盗用**：票据绑定 `uid` + `pid` + `machine_code`（设备码来自服务端 `user_binding`），
   且 `AuthUser` 本身已按项目校验 `token_version`。
3. **无票据白嫖计费通道**：没有合法签名票据就调不了 `/report`，拿不到扣费结果。
4. **凭空报成功骗扣自己钱**：`ok` 与 `upstream_code` 必须同时成立——不过这类"损失"是客户自己的，
   不构成对平台的风险。
5. **频率滥用**：`/prepare` 走项目日调用限额（1019）；所有 prepare/report 都有 `call_log` 留痕，
   可以据此做异常告警（同一用户短时间大量不同 ck 等）。
6. **协议泄露带来的固定成本**：上游域名/参数/解析规则由服务端下发，随时可改；
   客户端里不写死，也就不存在"旧客户端永久知道协议"的问题。

**挡不住的（必须说清楚）**：

- 客户端既然要**直连上游**，本机就一定能拿到上游返回的数据。理论上有人可以：自己实现一份
  `loginrecord` 查询逻辑、主动调用 `/prepare` 拿票据、拿到数据后**谎报 `ok=false`**——
  于是数据到手却只花了一次额度预检、没被扣费。**这类"已授权客户端谎报失败"无法在现有架构下根治。**
- 同理，票据签名密钥不能下发（也没下发），但客户端只要见过一次票据，就知道"格式长什么样"，
  可以自己伪造格式——**没有密钥则伪造不出有效签名**，这一点是安全的。
- 结论：本方案把"**服务端 IP 被封**"和"**无授权白嫖接口**"两件事解决了，
  但**不承诺"客户端一定扣费成功"**。要彻底闭环，只能把上游查询搬回服务端（那就回到 IP 被封的问题）
  或给上游加一层只有服务端能解的加密（当前上游不支持）。

**可以继续加固的方向**（本次未做）：异常上报告警（同一用户大量 `ok=false` 直接风控）、
按用户/项目粒度的票据频次上限、把 `ticket_ttl_seconds` 压到 30s 以内、
在客户端做代码混淆与反调试（提高逆向成本，不是防线）。

---

## 8. curl 例子

```bash
# 1) 申请票据
curl -X POST "https://你的域名/api/xhl/login-records/prepare" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ck":"BAIDUID=xxx:FG=1; BDUSS=yyy; STOKEN=zzz"}'

# 2) 客户端按 data.request 用本机 IP 直连（示例，占位符要替换）
#    GET https://passport.baidu.com/v3/api/user/loginrecord?client=pc&clientfrom=pc&lang=zh-cn&tpl=pp&ttt=1770000000000
#    Cookie: BAIDUID=xxx:FG=1; BDUSS=yyy; STOKEN=zzz

# 3) 回报结果（成功）
curl -X POST "https://你的域名/api/xhl/login-records/report" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ticket":"<上一步的 ticket>","ok":true,"upstream_code":110000,"record_count":3}'

# 3') 回报结果（失败：BDUSS 已失效）
curl -X POST "https://你的域名/api/xhl/login-records/report" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ticket":"<ticket>","ok":true,"upstream_code":400021,"record_count":0,"error":"BDUSS 已过期"}'
```

---

## 9. 本地验证

```powershell
cd xhl_sever_v2
python _verify_login_records.py
```

脚本会：备份 `config.yaml`（记 SHA256）→ 起本地假上游 → 临时把 `login_records.upstream_url`
指到假上游 → 重编译 + 按进程名 `_v2dev.exe` 重启 → 建项目 100006 → 建测试用户并**真实登录** →
跑完参数校验/日限额/正常链路/重放/篡改/失败链路/额度不足 → 查库复核 → **还原 config.yaml
（SHA256 逐字节比对）并重启** → 删掉测试用户/额度/绑定/日志/Redis 键（查库复核 0 条）。
