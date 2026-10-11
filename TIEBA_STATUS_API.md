# 贴吧状态查询 · 接口文档（客户端本机执行 + 服务端授权计费）

> 项目：**100007 小火龙贴吧状态查询器**（**会员制**：会员有效期内可用，不按次扣费）
> 代码：`internal/handler/tiebainfo.go`（接口/票据/计费）、`internal/router/router.go`（路由）
> 协议配置：`config.yaml` → `tieba_status` 段
> 端到端验证脚本：`_verify_tieba_status.py`
> 同架构参考：`LOGIN_RECORDS_API.md`（100006 登录记录查询）

---

## 0. 一句话架构

**服务端不产生任何上游流量。** 上游请求由**客户端用本机 IP** 发出；服务端只负责
「鉴权 → 额度预检 → 下发一张短时效一次性授权票据（票里带上游请求拼装指令 + **字段映射与单位换算**
+ 错误码释义）→ 客户端执行 → 客户端回报结果」。

原因与 100006 完全一致：上游出口 IP 集中调用会被贴吧风控封掉（服务端一台机器的 IP 去扫成千上万个
贴吧主页，几乎必然被限流）。把查询散到每个客户端自己的家宽/4G IP 上，才是这套业务能跑通的前提。

```
客户端                            服务端                        上游(贴吧 home/get/panel)
  │  POST /tieba-status/prepare   │
  │  {ck, username?}              │ 鉴权 + 额度预检 + 日限额
  ├──────────────────────────────►│ 签票据(HMAC, nonce, exp, uid/pid/设备码, ck指纹)
  │◄──────────────────────────────┤ ticket + request指令 + parse规则(字段映射/单位/错误码，**不含 ck 明文**)
  │                                                          ▲
  │  按指令拼 URL/headers（用本机 IP 直连，可走固定/动态代理）  │
  ├──────────────────────────────────────────────────────────┘
  │  POST /tieba-status/report    │
  │  {ticket, ok, upstream_code,  │ 验签/有效期/归属/一次性 nonce
  │   status, age, post_num, …}   │ 写 call_log；**会员制不按次扣费**(billing_log 仅在真需计费时写)
  │◄──────────────────────────────┤ {ok, billed:false, billing_mode, …}
```

---

## 1. 上游是谁（勘测结论，来自只读勘测 `小火龙账号管理器v2`）

参考实现：`小火龙账号管理器v2/baidu/accountcheck/check.go`
（`PanelURL` / `Panel()` / `parsePanel()` / `PanelResult`），需求文档 `任务需求.md`、抓包说明 `接口说明.txt`。

| 项 | 值 |
|---|---|
| 上游 | **百度贴吧**（网页公开接口，不是自建后端、不是第三方中转） |
| URL | `https://tieba.baidu.com/home/get/panel` |
| 固定 query | `ie=utf-8` + `un=<用户名>` + `red_tag=<10 位随机数字>` |
| method | `GET` |
| 认证 | **不需要凭据**：`un` 是用户名，接口按用户名公开查询。参考实现里这一步传的 Cookie 就是空串（`getWithCookie(..., "")`）；纯 cookie 行（没有用户名）时改用 `Cookie: ${CK}` 且**丢掉 `un` 参数**，返回该账号自己的主页 |
| headers | 照抓包：`Accept: */*`、`Accept-Language: zh-CN`、`X-Requested-With: XMLHttpRequest`、WebUA（Chrome/86） |
| 签名/加密 | **无**（`red_tag` 上游不校验，随机只为避免固定指纹） |
| 响应 | `{"no":0,"error":"成功","data":{"name":"…","tb_age":8,"post_num":12,"followed_count":34}}` |
| 分页 | **无分页**，一次返回该用户的全部状态数字 |
| 成功码 | `0` |
| 错误码 | `no=1130032` + `error="被屏蔽"`（此时 `data` 是**空数组**）；`errno=230004`「请先登录」= 未登录/登录态失效（该码属于同一条检测链上的 `pmc/listmsg`/`initNickname`，本接口的释义表里一并保留） |
| 降级响应 | `no=0` 但 `tb_age` 与 `post_num` **两个都缺** —— 实测是并发被限流时的形态（参考实现当失败重试） |
| 已知坑 | Go 只要设了自定义 `TLSClientConfig`/代理就会**默认关掉 HTTP/2**，而该接口在 HTTP/1.1 下更容易返回上面的降级数据（参考实现注释：h1.1 12/12 降级、h2 12/12 正常） |

### 1.1 字段映射（本项目的核心口径）

| 业务名 | 上游字段（点分路径） | 单位 | 说明 |
|---|---|---|---|
| **吧龄** | `data.tb_age` | **年**（`year`） | 可能是 JSON 数字 `8`，也可能是字符串 `"8.5"`；缺失时**留空**（不假装 0） |
| **发帖数** | `data.post_num` | 计数 | 数字或数字字符串；**缺失兜底 0** |
| **发言数** | `data.followed_count` | 计数 | 上游字段名叫 `followed_count`（关注数），业务口径当**发言数**用；**缺失兜底 0** |
| 用户名回显 | `data.name` | — | 仅用于客户端明细展示 |

**单位换算**：票内下发 `{path, label, from_unit, to_unit, factor}`，
客户端做 **`展示值 = 上游原始值 × factor`**。默认 `from_unit=to_unit=year, factor=1`；
若上游改成「天」，服务端把 `age_unit: day` 配上，会下发 `from_unit=day, to_unit=year, factor=1/365`
（`days_per_year` 可配），客户端不用发版。

**数值文本容错**（客户端实现，服务端在 `parse.value_note` 里声明）：
上游值可能是数字或数字字符串，还可能带单位；客户端解析时兼容 `"3年"`→3、`"8.5 年"`→8.5、
`"1.2万"`→12000、`"1,234"`→1234、`"365天"`→365，空/`—`/`暂无` 视为「缺字段」。

### 1.2 客户端表格：**7 列**（前面带勾选框）与「贴吧状态」综合列

客户端主界面表格**只有这 7 列**（密码 / 在线状态 / 贴吧封禁状态 / 手机号 / 接码链接 / 运行状态 / 备注
都不单独开列）：

```
[☑] 序号 │ 用户名 │ Cookie │ 贴吧状态 │ 吧龄 │ 发帖数 │ 发言数
```

- **Cookie** 列截断显示（超过 58 字符加省略号），**完整值在双击明细弹窗里**（那行叫 `Cookie(完整)`）。
- **吧龄** 列带单位显示（如 `3.2 年`），但**筛选/排序一律用内部数值**（单位=年）比较，不是按字符串比。
- 三个数值列可点表头排序；没查出来的行（缺值）无论升降序都排最后。

**「贴吧状态」是综合列**：把「在线状态 + 贴吧封禁状态 + 查询结果 + 失败原因」合并成一列中文描述。
取值与判定（客户端实现，见 `mainwindow/panel/source.go` 的 `StatusTextFromParse`）：

| 展示文案 | 触发条件 |
|---|---|
| `正常` | 上游 `no` 命中成功码，且 `data` 是对象、`tb_age`/`post_num` 至少一个可用 |
| `已被封禁` | 上游 `no=1130032` 或 `error` 文案含「被屏蔽」（此时 `data` 是空数组） |
| `离线(<码>)` | `no=230004`（请先登录）/ `400021`（BDUSS 已过期）/ `160102`（BDUSS 为空），或文案含「请先登录/未登录/已过期/登录状态失效/离线」 |
| `账号异常:上游未返回吧龄/发帖数,疑似被限流降级` | `no` 命中成功码但 `tb_age` 与 `post_num` **两个都缺**（降级响应） |
| `账号异常:上游 data 不是对象(无主页/被屏蔽)` | `no` 命中成功码但 `data` 不是对象 |
| `查询失败:<原因>(<码>)` | 其它非成功码（如 `50000` 风控、`1`/`-1` 异常） |
| `查询失败:<原因>` | 网络失败 / 响应解析失败 / 响应里没有状态码字段 |
| `格式错误:<原因>` | 本地拦下（缺 BDUSS / 无 cookie），不发票据 |
| `待查询` / `查询中…` / `已停止` | 未开始 / 正在查 / 用户停止 |

失败原因**直接写在「贴吧状态」列里**（不再单独开「备注」列）；成功行的综合列就是 `正常`，不带原因。
文本筛选可以按这一列搜（例如 `已被封禁`、`1130032`、`查询失败`）。

### 1.3 动态代理（客户端「配置」页）—— 对外最简、对内不退化

配置页只保留这几项（其余内部固定、不暴露；查询并发数仍在同一对话框的「查询设置」里，默认 1）：

```
代理模式   [ 不使用代理 / 固定代理(host:port) / 动态代理(提取链接) ]
提取链接   [____________________________________]   ← 仅「动态代理」时可见/可填
换 IP 策略 [ 失效即换(默认) / 每个账号一个 ]
           [ 测试提取 ]
```

- **提取链接原样粘贴、原样 GET**：客户端不重拼、不改写、不追加任何参数
  （`qty`/`time`/`format`/`split`/`sign`/`apikey` 全部保持用户给的样子，见 `tieba.ExtractTarget`）。
- **换 IP 策略二选一**：
  - `失效即换`（默认，`on_failure`）：提取一个 IP 后一直用它，直到某次请求失败且被判为
    **代理层错误**（连接被拒 / 超时 / 代理握手失败 / 407 / DNS 解析失败）→ 重新提取一个新 IP 并
    **自动重试当前账号**（重试上限**内部固定 3 次**，不暴露给用户）；重试用尽才把该账号记为失败。
  - `每个账号一个`（`per_account`）：每个账号提取一次。
- **业务失败绝不换 IP**：上游返回 `400021`（BDUSS 过期）/ `50000`（风控）/ `160102`（BDUSS 为空）/
  `1130032`（被屏蔽）这类「查询成功但结果是异常状态」时，代理没问题，换 IP 没意义 → 不换、不重试。
  分类实现见 `tieba.IsProxyLayerError` / `tieba.ErrorKind`。
- **提取结果解析**兼容：`ip:port`、`http://ip:port`、多行（取第一行）、JSON（含 `ip`/`port` 字段，
  也支持 `{"data":[…]}` / `{"proxy":"ip:port"}` 这类包裹）。
- **解析不出来 = 提取失败** → 客户端**跳过该账号**，并把脱敏后的原因写进「贴吧状态」列
  （如 `查询失败:动态代理提取失败:提取接口返回的内容里没解析出 ip:port`）。
- **「测试提取」按钮**：点一下立刻提取一次，把拿到的 `ip:port` 显示在状态行，便于验证链接是否可用。
- **脱敏**：日志/备注里绝不出现完整的 `apikey`/`sign`/提取链接，只显示域名 + `…(已脱敏)`
  （见 `tieba.MaskLink`；单测里有「日志与备注不含密钥原文」的断言）。

---

## 2. 两个接口

两个接口都在**用户端鉴权中间件 `AuthUser`** 之下，计费项目取 token 里的 `pid`
（老 token 无 `pid` 时兜底 `100007`）。

### 2.1 `POST /api/xhl/tieba-status/prepare` —— 申请授权票据

```
Header: Authorization: Bearer <用户 token>
Content-Type: application/json
Body:   { "ck": "BAIDUID=…; BDUSS=…; STOKEN=…", "username": "贴吧用户名(可选)" }
        // ck 也兼容字段名 cookie / bduss
```

`username` 只用于让客户端少走一步（上游 `un` 参数），**服务端不落库、不写日志、不校验**。

服务端做的事（**不发任何上游请求**）：参数校验 → 额度预检 → 项目日调用限额 → 签票据（HMAC）→ 返回指令。

成功返回（关键部分）：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "ticket": "<base64url(payload)>.<hex hmac>",
    "nonce": "3f1c…",
    "expires_at": 1770000000, "expires_in": 120,
    "project_id": "100007",
    "billing": {
      "mode": "membership", "kind": "", "unit": 0,
      "charged_on": "report",
      "note": "会员制：会员有效期内可查询，不按次扣费；失败只记 call_log"
    },
    "request": {
      "method": "GET",
      "url": "https://tieba.baidu.com/home/get/panel",
      "query": { "ie": "utf-8", "red_tag": "${RED_TAG}", "un": "${UN}" },
      "query_string": "ie=utf-8&red_tag=${RED_TAG}&un=${UN}",
      "headers": {
        "Cookie": "${CK}",
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) …Chrome/86.0.4240.198…",
        "Accept": "*/*", "Accept-Language": "zh-CN", "X-Requested-With": "XMLHttpRequest"
      },
      "placeholders": {
        "${CK}": "调用方这次提交的 ck 原串（整串 cookie），装进 Cookie 头",
        "${UN}": "导入行第 1 段的用户名（UTF-8 由客户端自行 URL 编码）",
        "${RED_TAG}": "10 位随机数字（上游不校验，只为避免固定指纹）",
        "${TS_MS}": "发起请求那一刻的毫秒时间戳（字符串）"
      },
      "optional_params": {
        "un": "用户名缺失（纯 ck 行）时，客户端应整条去掉 un 参数并保留 Cookie 头，改为查询该 ck 自己的主页"
      },
      "timeout_seconds": 15,
      "note": "由客户端用本机 IP 直连上游；服务端不代发请求，也不产生上游流量"
    },
    "parse": {
      "format": "json",
      "code_field": "no",
      "message_field": "error",
      "success_codes": [0],
      "data_path": "data",
      "name_field": "data.name",
      "fields": {
        "age":     { "path": "data.tb_age",         "label": "吧龄",   "from_unit": "year",  "to_unit": "year",  "factor": 1,
                     "note": "上游原始值 × factor = 展示值（单位 to_unit）；缺字段时留空" },
        "post":    { "path": "data.post_num",       "label": "发帖数", "from_unit": "count", "to_unit": "count", "factor": 1,
                     "note": "整数计数；缺字段时兜底 0" },
        "comment": { "path": "data.followed_count", "label": "发言数", "from_unit": "count", "to_unit": "count", "factor": 1,
                     "note": "整数计数（上游字段名叫 followed_count）；缺字段时兜底 0" }
      },
      "degraded_note": "no=0 但 age 与 post 两个字段都缺 = 上游降级响应（通常被限流），按失败处理，不要当成「查询成功但全 0」写库",
      "value_note": "字段值可能是 JSON 数字或数字字符串；还可能带单位（如 \"3年\"、\"1.2万\"），客户端需容错解析",
      "error_codes": {
        "0": "查询成功", "1130032": "被屏蔽（账号主页被隐私屏蔽/无主页）",
        "230004": "未登录（登录态失效，请重新登录该账号）", "50000": "触发风控",
        "1": "账号异常（接口未返回可用的吧龄/发帖数）", "-1": "账号异常（上游返回异常状态）"
      }
    },
    "report_url": "/api/xhl/tieba-status/report"
  }
}
```

**客户端要做的事**：把 `${CK}/${UN}/${RED_TAG}/${TS_MS}` 替换（query 里的用户名要做 URL 编码；
用户名为空时整条丢掉 `un`），按 `method` + `url` + `query_string` + `headers` 用**本机 IP** 发一次请求；
按 `parse` 的字段映射与 `factor` 解析；然后调 `/report`。

### 2.2 `POST /api/xhl/tieba-status/report` —— 回报执行结果

```
Header: Authorization: Bearer <用户 token>
Body:   { "ticket": "…", "ok": true, "upstream_code": 0,
          "status": "查询成功", "age": "8.5", "post_num": "12", "comment_num": "34" }
```

| 字段 | 说明 |
|---|---|
| `ticket` | **必填**，`/prepare` 原样回传，一个字节都不能改（签名覆盖整个载荷） |
| `ok` | 客户端是否成功发出请求、解出响应且命中成功码 |
| `upstream_code` | 上游 `no`，数字或数字字符串都收；**成功判定用它**，防止「什么都没做就报成功」。没拿到 `no` 时必须整字段省略 |
| `status` | 客户端判定的中文状态（仅留痕，便于后台排查） |
| `age` / `post_num` / `comment_num` | 三个指标（仅留痕，便于核对字段映射是否被上游改动） |
| `error` | 失败摘要（写进 call_log 的 message） |

**判定成功：`ok == true` 且 `upstream_code` 命中 `parse.success_codes`。**

返回：

```json
{ "code": 0, "message": "ok",
  "data": { "ok": true, "billed": false, "billing_mode": "membership",
            "cost": 0, "remaining": 0, "upstream_code": "0",
            "status": "查询成功", "age": "8.5", "post_num": "12", "comment_num": "34" } }
```

`billed=false` 是本项目的**正常结果**：会员制不按次扣费。只有当某项目被（用户覆盖或项目配置）
改成 `per_call`/`credits` 时 `billed` 才可能为 true —— 这不是本项目的设计口径。

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
| `action` | 固定 `tieba-status`（防票据被挪用到别的接口） |
| `iat` / `exp` | 签发/过期（Unix 秒），`exp - iat` = `tieba_status.ticket_ttl_seconds` |

签名：

```
body = base64url(json(payload))                    # 无 padding 的 URL-safe base64
sig  = hex(HMAC_SHA256(ticket_secret, body))
ticket = body + "." + sig
```

`/report` 校验顺序（任一不过 → 全部记一条失败的 call_log）：
签名（常数时间比较）→ 版本 → `action` → 是否过期 → 签发时间是否异常（> 现在 +60s）→
`uid`/`pid` 是否等于当前 token → 额度 → **一次性 nonce**（Redis `SETNX`，TTL = 票据有效期 + 60s）。

**重复上报会被拒（1024），且不会二次计费。**

---

## 4. 计费语义（写明）

- **本项目是会员制（`membership`），不按次扣费**：会员有效期内可查询，
  `/report` 成功后 `billed=false`、`cost=0`、不写 `billing_log`，只累计 `total_calls` 与 `call_log`。
- 计费模式与单价取**项目生效模式 + 项目单价**（`user_entitlement` 覆盖优先，否则 `project_setting`），
  与 `figure.go` / `loginrecords.go` 完全同口径。若被改成 `per_call`/`credits`，
  才会在成功后扣 `remaining_calls`/`credits` 并写 `billing_log`（`kind=calls|credits`、`delta=-unit`、
  `reason=call`、`remark=贴吧状态查询成功`）。
- 参数错、额度不足、票据无效/过期/重放、**上游失败** → **一律不扣费**。
- 无论成功失败都写 `call_log`：`action='tieba-status'`、`source='user'`、`ok`、`errno`(上游 no)、
  `message`(成功时带 状态/吧龄/发帖数/发言数)、`cost_kind`/`cost`、耗时、IP。

调用记录在后台「项目 → 调用记录」里按项目 100007 可见（动作显示为 `tieba-status`；
后台前端动作**筛选下拉**是已打包的旧版本，没有这一项，用「全部动作」即可看到）。

---

## 5. 协议参数放在 config.yaml 的哪一段 / 怎么轮换

```yaml
tieba_status:
  upstream_url: ""             # 上游地址；留空 = 内置 https://tieba.baidu.com/home/get/panel
  method: "GET"
  query: {}                    # 附加 query（${UN}=用户名、${RED_TAG}=10 位随机数、${TS_MS}=毫秒时间戳）
  headers: {}                  # 附加请求头（${CK}=调用方 ck、${UN}=用户名、${UPSTREAM_KEY}=upstream_key）
  user_agent: ""               # 留空 = 内置照抓包 WebUA（Chrome/86）
  timeout_seconds: 15          # 给客户端的建议超时（1..60）
  success_codes: []            # 视为「查到状态」的上游 no；留空 = [0]
  ticket_secret: ""            # 票据 HMAC 密钥；**留空回退 security.client_sign_salt**
  ticket_ttl_seconds: 120      # 票据有效期（10..600）
  upstream_key: ""             # 可选上游密钥片段，以 ${UPSTREAM_KEY} 注入下发的 query/headers
  age_field: "data.tb_age"             # 吧龄字段路径
  post_field: "data.post_num"          # 发帖数字段路径
  comment_field: "data.followed_count" # 发言数字段路径
  age_unit: "year"             # 吧龄原始单位：year（默认）/ day
  days_per_year: 365           # day→year 的换算基准（age_unit=day 时用）
  error_codes: {}              # 上游 no/errno → 中文释义（留空 = 内置表）
```

**保管**：
- `ticket_secret` 只从 `config.yaml` 读，**不写进代码、不落库、不写日志、不回传给客户端之外的任何地方**
  （客户端只拿到签名结果，拿不到密钥本身）。留空时回退 `security.client_sign_salt`（本来就只在服务端）。
- 上游 `home/get/panel` **不需要共享密钥**，所以没有「上游密钥」可保管；唯一凭据是调用方自己的 ck，
  服务端**不存 ck**（只在票据里放 `sha256(ck)` 指纹），日志里也不写 ck。
- `upstream_key` 是为「将来换成需要账号/密钥的上游」留的钩子。

**轮换**：
- 换票据密钥：改 `tieba_status.ticket_secret`（或 `security.client_sign_salt`）→ 重启服务。
  旧票据立即失效（客户端重取即可），已发生的调用记录不受影响。
- 换上游域名/参数/字段路径/单位/错误码释义：改上面任意一项 → 重启服务。**客户端不用重新发版**
  ——这正是「协议放服务端下发」的意义（上游改了字段名，只改 `age_field` 等即可）。

---

## 6. 错误码

业务码（`util/response.go`）：

| code | 含义 |
|---|---|
| 0 | 成功 |
| 1001 | 参数错误：`ck` 为空 / 缺少 `BDUSS` / 过长(>4096) / `report` 缺 `ticket` |
| 1002 | 未登录 / token 无效或过期 |
| 1016 | 无该项目权限（会员制项目未开通/已过期） |
| 1019 | 今日调用次数已达上限（项目 `call_limit`，按用户+项目+接口 每日 Redis 计数） |
| 1023 | 额度不足（按次/积分模式） |
| 1024 | 票据无效 / 已过期 / 已被使用（重放） / 归属不匹配 / 签名不匹配 |
| 1005 | 系统错误（票据签名密钥未配置、Redis 不可用等） |

上游 `no` 由客户端在 `/report` 里回报，原样记进 `call_log.errno` 并在 `parse.error_codes` 里给释义，
**不占用业务码空间**。

客户端把上游结果映射成四种表格状态：`查询成功` / `被屏蔽`(1130032) / `未登录`(230004) /
`账号异常`（其它非成功码、data 不是对象、以及**降级响应**）；本地拦下的（缺 BDUSS / 无 cookie）标 `格式错误`。

---

## 7. 防御与诚实边界（必读）

**能挡住的**：

1. **重放**：票据一次性（Redis `SETNX` 原子占位）+ 短时效（默认 120s）。
2. **跨设备/跨人盗用**：票据绑定 `uid` + `pid` + `machine_code`（设备码来自服务端 `user_binding`）。
3. **无票据白嫖计费通道**：没有合法签名票据就调不了 `/report`。
4. **凭空报成功骗扣自己钱**：`ok` 与 `upstream_code` 必须同时成立。
5. **频率滥用**：`/prepare` 走项目日调用限额（1019）；prepare/report 都有 `call_log` 留痕。
6. **协议泄露带来的固定成本**：上游域名/参数/字段路径/单位由服务端下发，随时可改；客户端不写死。

**挡不住的（必须说清楚）**：

- 客户端既然要**直连上游**，本机就一定能拿到上游返回的数据。理论上有人可以自己实现一份
  `home/get/panel` 查询逻辑、调 `/prepare` 拿票据、拿到数据后**谎报 `ok=false`**。
  本项目是会员制，这条路的"损失"只是少记一条 call_log，**不构成平台收入风险**；但也**不能假装能防**。
- 票据签名密钥不下发，所以伪造不出有效签名；但客户端见过一次票据就知道格式。
- 结论：本方案解决的是「**服务端 IP 被封**」和「**无授权白嫖接口**」，
  **不承诺"客户端一定扣费成功"**。

---

## 8. curl 例子

```bash
# 1) 申请票据
curl -X POST "https://你的域名/api/xhl/tieba-status/prepare" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ck":"BAIDUID=xxx:FG=1; BDUSS=yyy; STOKEN=zzz","username":"某用户名"}'

# 2) 客户端按 data.request 用本机 IP 直连（示例，占位符要替换）
#    GET https://tieba.baidu.com/home/get/panel?ie=utf-8&red_tag=1234567890&un=%E6%9F%90%E7%94%A8%E6%88%B7%E5%90%8D
#    Cookie: BAIDUID=xxx:FG=1; BDUSS=yyy; STOKEN=zzz
#    （纯 ck 行：去掉 un 参数，保留 Cookie）

# 3) 回报结果（成功）
curl -X POST "https://你的域名/api/xhl/tieba-status/report" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ticket":"<上一步的 ticket>","ok":true,"upstream_code":0,"status":"查询成功","age":"8.5","post_num":"12","comment_num":"34"}'

# 3') 回报结果（被屏蔽：业务错误，不换 IP、不扣费）
curl -X POST "https://你的域名/api/xhl/tieba-status/report" \
  -H "Authorization: Bearer <用户 token>" -H "Content-Type: application/json" \
  -d '{"ticket":"<ticket>","ok":false,"upstream_code":1130032,"status":"被屏蔽","error":"被屏蔽"}'
```

---

## 9. 本地验证

```powershell
cd xhl_sever_v2
python _verify_tieba_status.py
```

脚本会：备份 `config.yaml`（记 SHA256）→ 起本地**假上游**（mock 出不同吧龄/发帖数/发言数的账号，
**不打真百度**）→ 临时把 `tieba_status.upstream_url` 指到假上游 → 重编译 + 按进程名 `_v2dev.exe` 重启
→ 建项目 100007 → 建测试用户并**真实登录** → 跑完参数校验/日限额/正常链路/被屏蔽/降级/重放/篡改/
未登录 → **查库复核** `call_log` 落库与额度未被扣 → **还原 config.yaml（SHA256 逐字节比对）并重启**
→ 删掉测试用户/额度/绑定/日志/Redis 键（查库复核 0 条）。
