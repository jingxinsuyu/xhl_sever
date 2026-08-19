# 小火龙工具箱后端 · 静态二进制运行镜像
FROM alpine:3.20

# 时区 + HTTPS 根证书（百度扫码确认等外部 HTTPS 调用需要）
RUN apk add --no-cache tzdata ca-certificates \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY xhl_sever-linux /app/xhl_sever

# config.yaml / web / uploads 由 compose 以卷挂载，见 docker-compose.yml
EXPOSE 8888

ENTRYPOINT ["/app/xhl_sever"]
