# 小火龙工具箱后端 · 静态二进制运行镜像
FROM alpine:3.20

# 时区 + HTTPS 根证书（百度扫码确认等外部 HTTPS 调用需要）
RUN apk add --no-cache tzdata ca-certificates \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY xhl_sever-linux /app/xhl_sever

# 滑动验证码素材必须一起进镜像：代码用 BaseDir/captcha_assets/backgrounds 找背景图，
# 少了它 POST /api/xhl/captcha 会在运行期报「验证码素材加载失败」（不是编译错误，容易漏）。
# 注意：构建上下文（服务器 /worker 目录）里必须有 captcha_assets/，否则这一行会直接让 build 失败。
COPY captcha_assets /app/captcha_assets

# config.yaml / web / uploads 由 compose 以卷挂载，见 docker-compose.yml
EXPOSE 8888

ENTRYPOINT ["/app/xhl_sever"]
