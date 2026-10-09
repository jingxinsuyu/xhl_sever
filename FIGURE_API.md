# 贴吧「设置虚拟形象」接口文档

> 项目：**100005 贴吧设置虚拟形象**（按次计费）
> 代码：`internal/tieba/figure.go`（流程）+ `internal/handler/figure.go`（接口与计费）
> 移植自 `re_tieba/figure_set`（HAR 逆向），纯标准库实现。

---

## 1. 它是怎么生效的

贴吧的虚拟形象本体就是 `submitCustomFigure` 的 `figure_pid`，服务端按它渲染，
客户端从 `http://tiebapic.baidu.com/figure/pic/item/<pic_id_encode>.jpg` 拉图。
所以「把自己上传的图片做成虚拟形象」= **传图拿到 pic_id → 用它当 figure_pid 提交**。

服务端内部走三步（全部在服务端完成，调用方只要传 ck + 图片）：

```
① 传图    POST https://uploadphotos.baidu.com/Pic/upload?pid=tieba&filetype=base64   file=base64(图片)
② 传 meta POST https://tieba.baidu.com/mo/q/customfigure/uploadFigureMeta            figure_meta=base64(metaJSON)
③ 提交    POST https://tieba.baidu.com/mo/q/customfigure/submitCustomFigure          figure_pid=①的 pic_id
```

## 2. 认证要什么

**整串 Cookie**，其中**必须有 `BDUSS` 与 `SToken`**（大小写不敏感，两处叫法都认）。
缺哪个会直接在参数校验阶段被拒，不会去调百度。

## 3. 两个入口

### 3.1 用户端（客户端登录用户）

```
POST /api/xhl/figure/set
Header: Authorization: Bearer <用户 token>       ← 和小火龙扫码一样，先登录拿 token
Content-Type: multipart/form-data
```

| form 字段 | 说明 |
|---|---|
| `ck` | **必填**，完整 cookie 串（也兼容 `cookie` / `bduss` 字段名） |
| `image` | **必填**，图片文件。只收 **PNG / JPG**（按文件头判断，不看扩展名），单张 **≤ 5MB** |

计费：按**项目生效模式**扣（该项目默认 `per_call`，每次 1 次）。
- 项目 id 取 token 里的 `pid`；老 token 没 `pid` 时按 100005 算。
- **设置成功才扣费**；失败/参数错一律不扣。
- 会员制项目不扣次数（只累计调用次数，与扫码一致）。

### 3.2 开放平台（第三方）

```
POST /api/open/figure/set
Header: xhlkey: sk-xxxxxxxx        ← 后台「API 密钥」里创建
Content-Type: multipart/form-data
form: ck=<完整 cookie>&image=<图片文件>
```

计费：扣 **API Key 的积分**，单价 `cost.open_figure_cost`（**默认 1 积分**）。
**设置成功才扣**；失败不扣。积分不足直接返回 1020，不会去调百度。

## 4. 返回

成功（HTTP 200）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "ok": true,
    "pic_id": "9f2c…",
    "figure_url": "http://tiebapic.baidu.com/figure/pic/item/xxxx.jpg",
    "billing_mode": "per_call",   // 用户端返回
    "cost": 1,
    "remaining": 4,               // 用户端：扣完还剩几次
    "balance": 99,                // 开放平台：扣完还剩多少积分
    "hint": "App 里退出重进 / 下拉刷新才能看到新形象"
  }
}
```

失败：`code != 0`，`message` 是可读原因（如 `设置失败：提交形象失败：{"error":"登录失效"}`），**且不扣费**。

## 5. 错误码

| code | 含义 |
|---|---|
| 1001 | 参数错误：ck 为空 / 缺 BDUSS 或 SToken / 缺图片 / 图片格式不支持 / 图片超 5MB / ck 过长 |
| 1002 | 未登录（用户端）或缺少 `xhlkey`（开放平台） |
| 1004 | 项目不存在或已停用 |
| 1005 | 设置失败（上游百度返回错误，或网络/代理问题）—— 不扣费 |
| 1016 | 无该项目权限（会员过期等） |
| 1020 | 积分不足（开放平台） |
| 1023 | 额度不足（用户端按次/积分模式） |

## 6. curl 例子

```bash
# 用户端
curl -X POST "https://你的域名/api/xhl/figure/set" \
  -H "Authorization: Bearer <token>" \
  -F "ck=BDUSS=xxx; SToken=yyy; BAIDUID=zzz" \
  -F "image=@avatar.png"

# 开放平台
curl -X POST "https://你的域名/api/open/figure/set" \
  -H "xhlkey: sk-xxxxxxxx" \
  -F "ck=BDUSS=xxx; SToken=yyy" \
  -F "image=@avatar.png"
```

## 7. 其他

- **出口代理**：走该项目的代理配置（项目设置 → 代理），与扫码/网盘一致。
- **调用记录**：每次调用在「项目 → 调用记录」里留一条，动作 = `虚拟形象设置`；失败的也记（带原因）。
- **扣费流水**：成功扣费会在「项目 → 扣费记录」里留一条 `调用扣费 -1`。
- **图片要求**：建议正方形、≥ 200×200，png/jpg 都行；太大/太小的图百度那边可能效果不佳。
- **换域名**：万一百度换接口地址，改 `config.yaml` 的 `tieba.upload_url / meta_url / submit_url` 即可，不用改代码。
- **设置后要刷新**：贴吧 App 里退出重进 / 下拉刷新才能看到新形象（百度侧缓存）。
