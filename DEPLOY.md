# 小火龙工具箱后端 · 部署文档

> 卡密式订阅系统后端：Go + Gin + GORM + Redis + JWT。
> 数据库使用 MySQL（自动建库建表），Redis 用于验证码 / 登录限流 / 每日登录次数。

---

## 1. 目录结构

```
xhl_sever/
├── main.go                      # 程序入口
├── config.yaml                  # 运行配置（含密钥，不入库，用 config.example.yaml 为模板）
├── config.example.yaml          # 配置模板（占位符 CHANGE_ME）
├── internal/
│   ├── config/                  # 配置加载
│   ├── database/                # MySQL 连接 + 自动建表
│   ├── redisclient/             # Redis 连接
│   ├── router/                  # 路由
│   ├── middleware/              # 鉴权中间件（admin / super / user）
│   ├── handler/                 # 接口处理器
│   ├── model/                   # 数据模型
│   ├── crypto/                  # 服务自身加解密（登录密码 / qrlogin data / AES 原语）
│   ├── baidu/                   # 百度相关（device 设备指纹 / qrlogin 扫码确认）
│   ├── migration/               # 一次性数据迁移（project id 字符串化）
│   └── util/                    # JWT / 密码哈希 / 签名 / 卡密生成 / 响应封装
├── Dockerfile                   # Docker 部署参考镜像（根目录）
├── docker-compose.yml           # Docker 部署参考编排（根目录）
├── deploy.py                    # 一键部署脚本（内含服务器连接信息，不入库）
└── uploads/                     # 上传目录（轮播图 / 更新文件，不入库）
```

## 2. 配置说明（config.yaml）

配置以 `config.example.yaml` 为模板，复制为 `config.yaml` 后填入真实值：

| 配置块 | 字段 | 说明 |
|---|---|---|
| `server` | `port` | HTTP 监听端口（本地默认 8080，容器 8888） |
| `server` | `mode` | `debug` / `release` |
| `server` | `static_dir` | 前端静态资源目录（相对 config.yaml 所在目录，默认 `web`） |
| `database` | `host/port/user/password/dbname` | MySQL 连接信息；库不存在会自动创建 |
| `redis` | `host/port/password/db` | Redis 连接信息 |
| `jwt` | `secret` | JWT 签名密钥，**生产必须换随机值** |
| `jwt` | `expire_hours` | 后台管理员 token 有效期（小时，默认 72） |
| `jwt` | `user_expire_hours` | 用户端 token 有效期（小时，默认 8760=1 年） |
| `security` | `login_rate_limit_per_min` | 每账号每分钟登录次数上限（防撞库） |
| `security` | `captcha_expire_seconds` | 图形验证码有效期（秒） |
| `security` | `client_sign_salt` | 客户端登录参数签名盐，**与客户端一致，务必修改** |
| `security` | `client_aes_key` | 客户端登录密码 AES 密钥（16 字节），**与客户端一致，务必修改** |
| `security` | `qrlogin_aes_key` | 扫码确认 data 加密 AES 密钥（16 字节），**与前端一致，务必修改** |
| `upload` | `dir` | 上传根目录（默认 `uploads`） |
| `upload` | `base_url` | 静态资源对外访问地址；**容器部署留空** → 返回 `/uploads` 相对路径 |
| `super_admin` | `username/password/nickname` | 超级管理员（yaml 配置，不落库），**生产务必修改默认密码** |

> 表由 GORM 自动创建（`AutoMigrate`），无需手工建表。

## 3. 本地开发运行

**依赖**：Go 1.21+、MySQL、Redis。

```bash
# 1. 准备配置
cp config.example.yaml config.yaml   # 填入本机 MySQL/Redis 信息

# 2. 启动（自动连接 MySQL、Redis，自动建库建表）
go run .
```

启动后：
- 健康检查：`GET /api/health`
- 管理后台前端（如构建）放在 `web/` 目录，由后端直接托管 + SPA 回退

**交叉编译 Linux 二进制**（供服务器使用）：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o xhl_sever-linux .
```

## 3.1 项目 id 字符串化迁移（一次性，本地 + 服务器）

> 新版本 project 弃用自增 id，改为 **6 位数字字符串**（用户自定义）。
> 已有数据库需迁移：目标项目（名含「小火龙」，回退 id=1）→ `100001`，其余项目 → `LPAD(旧id,6,'0')`。

```bash
# 备份 + 迁移（先导出 JSON 快照，再改列类型 + 迁移数据，最后校验）
./xhl_sever -migrate-project-id
# 可选：目标项目识别关键字 / 新 id
./xhl_sever -migrate-project-id -project-name 小火龙 -target-id 100001
```

- 迁移工具读 `config.yaml` 的数据库配置，**本地/服务器同一二进制**都能跑。
- 自动快照存到 `软件根目录/project_id_migration_backup_<时间戳>.json`，供回滚。
- 已迁移过会自动检测并跳过；历史硬删项目遗留的孤儿引用仅警告，不阻断。
- **服务器执行**：上传新 `xhl_sever-linux` → `chmod +x` → 在 `/worker/xhl_sever` 下执行。容器内 config 的 host 是服务名 `mysql`（仅容器内可解析），宿主机直连需覆盖 host：
  ```bash
  ./xhl_sever -migrate-project-id -db-host 127.0.0.1
  ```
- ⚠ 迁移后旧客户端（写死 project_id=1）登录会失败，需随客户端升级改用 `100001`。

## 4. 生产 Docker 部署

全容器化：一个 `docker-compose.yml` 管理 **MySQL + Redis + Go 后端** 三个服务，参考文件为仓库根目录的 `Dockerfile` 与 `docker-compose.yml`。

### 4.1 服务器目录结构（/worker）

```
/worker/
├── docker-compose.yml            # 三服务编排
├── mysql/
│   ├── conf/my.cnf               # utf8mb4
│   └── data/                     # 数据卷
├── redis/
│   ├── redis.conf                # requirepass + appendonly
│   └── data/
└── xhl_sever/
    ├── config.yaml               # 容器内配置（host 用服务名 mysql/redis）
    ├── xhl_sever-linux           # Linux 静态二进制
    ├── web/                      # 前端 dist（Gin 托管）
    └── uploads/
```

### 4.2 后端镜像（根目录 Dockerfile）

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone
WORKDIR /app
COPY xhl_sever-linux /app/xhl_sever
EXPOSE 8888
ENTRYPOINT ["/app/xhl_sever"]
```

- 基于 **alpine + 静态二进制**；时区设为上海；`ca-certificates` 供百度扫码确认（passport.baidu.com）等外部 HTTPS 调用。
- `config.yaml / web / uploads` 由 compose 以卷挂载，**不用打进镜像**。

### 4.3 容器内 config.yaml 要点

```yaml
server:
  port: 8888          # 前后端同源
  mode: release
  static_dir: web     # 托管前端 dist

database:
  host: mysql         # ← compose 服务名，容器内网络互通
redis:
  host: redis         # ← compose 服务名

upload:
  base_url: ""        # 留空 → 返回 /uploads 相对路径
```

### 4.4 docker-compose.yml 要点（根目录 docker-compose.yml）

- `xhl-mysql`：mysql:8.0，仅绑定 `127.0.0.1:3306`，带 healthcheck；
- `xhl-redis`：redis:7-alpine，加载 redis.conf（requirepass + AOF 持久化），仅绑定 `127.0.0.1:6379`；
- `xhl-sever`：构建自 `.`（根目录，含 Dockerfile），`depends_on` 等 mysql/redis healthcheck 通过；对外端口 **8888**；
- 卷挂载：`config.yaml`、`web/`、`uploads/` 均在宿主机仓库根目录下，**改配置/换前端免重建镜像**。

```bash
cd /worker && docker compose up -d --build     # 首次/更新后端后执行
docker compose ps                              # 查看状态
docker compose logs -f xhl_sever               # 看日志
curl http://127.0.0.1:8888/api/health          # 健康检查，应返回 {"code":0,"message":"ok"}
```

### 4.5 一键部署脚本 deploy.py

> 该脚本内含服务器 SSH/SFTP 密码，**仅保留在本地，禁止提交 git**（已在 .gitignore）。

```bash
python deploy.py backend     # 上传 xhl_sever-linux → chmod +x → docker compose up -d --build → 健康检查
python deploy.py frontend    # 上传 ../xhl-admin/web → 静态卷挂载，无需重启容器
```

后端更新流程：本地改代码 → 交叉编译 → 执行 `python deploy.py backend`。

### 4.6 上线前安全清单

- [ ] `jwt.secret` 换成随机值
- [ ] `super_admin` 默认账号密码改掉
- [ ] `client_sign_salt` / `client_aes_key` 与客户端保持一致并更换
- [ ] MySQL / Redis 使用强密码（勿用文档/仓库中出现的默认值）
- [ ] 服务器防火墙只放行必要端口（SSH、8888 等）

## 5. 常见问题

| 现象 | 处理 |
|---|---|
| 启动报 `初始化数据库失败` | 确认 MySQL 地址/密码正确、网络可达 |
| 启动报 `初始化 Redis 失败` | 确认 Redis 密码（`requirepass`）与 config 一致 |
| 前端刷新 404 | 确认 `static_dir` 指向 web 目录，后端已做 SPA 回退 |
| 图片/文件访问 404 | 确认 `upload.base_url` 配置；容器部署留空用相对路径 |
| 扫码确认接口报外部 HTTPS 错误 | 确认镜像装了 `ca-certificates`（Dockerfile 已含） |
| 旧客户端登录报「项目不存在」 | 项目 id 已改为字符串，需升级客户端改用新 project_id |
| SFTP 上传后二进制无执行权限 | 执行 `chmod +x xhl_sever-linux`（SFTP 会丢可执行位） |
