# syntax=docker/dockerfile:1
# ============================================================
# bilipro 多阶段构建 — 基础镜像可切换（方案 A）
# 默认(alpine):    docker build -t bilipro .
# 切 distroless:   docker build \
#                    --build-arg BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot \
#                    --build-arg APP_UID=65532 \
#                    -t bilipro .
# 切 scratch:      docker build --build-arg BASE_IMAGE=scratch -t bilipro .
# 运行:            docker run --rm -v ./config:/app/config -v ./logs:/app/logs bilipro daily
# ============================================================

ARG BASE_IMAGE=alpine:3.21

# ---------- Stage 1: 构建 ----------
FROM golang:1.24-alpine AS builder
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o bilipro ./main.go

# ---------- Stage 2: 系统数据（CA 证书 + 时区 + 目录骨架），与运行基镜像解耦 ----------
FROM alpine:3.21 AS base-files
RUN apk add --no-cache ca-certificates tzdata \
 && mkdir -p /app/config /app/logs

# ---------- Stage 3: 运行（基镜像可切换，本阶段无 RUN，兼容无 shell 基镜像） ----------
FROM ${BASE_IMAGE}

ARG APP_UID=10001
ENV TZ=Asia/Shanghai \
    CONFIG_PATH=/app/config/config.yaml \
    COOKIE_PATH=/app/config/cookies.json \
    LOG_FILE=/app/logs/bilitoolgo.log

WORKDIR /app

# 证书/时区/目录：基镜像已有则覆盖无害（同为 Mozilla CA 包与 TZif 时区数据）
COPY --chown=${APP_UID}:${APP_UID} --from=base-files /app /app
COPY --chown=${APP_UID}:${APP_UID} --from=builder /build/bilipro /app/bilipro
COPY --from=base-files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=base-files /usr/share/zoneinfo /usr/share/zoneinfo

COPY config.example.yaml /app/config/config.yaml.example
COPY cookies.example.json /app/config/cookies.json.example

USER ${APP_UID}:${APP_UID}
VOLUME ["/app/config", "/app/logs"]
ENTRYPOINT ["/app/bilipro"]
CMD ["daily"]
