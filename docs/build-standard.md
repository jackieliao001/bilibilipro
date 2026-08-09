# bilipro 镜像构建标准（统一规范）

> 生效日期：2026-08-09（v1.1：tag/文件名增加 v 前缀，消除纯日期歧义）
> 适用范围：bilipro 项目所有 Docker 镜像构建
> 本文件为**唯一构建标准**，后续所有构建一律按此执行。

## 一、构建方式（固定）

- **工具链**：WSL Ubuntu 内的 Docker（多阶段构建）
- **构建文件**：项目根目录 `Dockerfile`（多阶段：golang:1.24-alpine 编译 → base-files 制备证书/时区 → alpine:3.21 运行层）
- **禁止**：单阶段二进制注入、本机 Windows docker 构建

## 二、Tag 命名规则（固定）

```
格式：bilipro:v<构建日期 YYYYMMDD>        # v = version，前缀消除纯日期歧义
示例：docker build -t bilipro:v20260809 .
同日多次构建：v20260809（首次）、v20260809-2（当日第二次）、v20260809-3 ...（依次递增序号）
```

- 日期取构建当日（Asia/Shanghai 时区），8 位数字；`v` 为固定前缀
- **功能迭代版本与日常构建统一使用日期 tag**（不再使用 v0.x.x 语义版本号）
- `bilipro:latest` 标签同时保留（供 docker-compose 默认引用），指向最新一次构建

## 三、导出命名规则（固定）

```
文件名格式：bilipro-v<构建日期 YYYYMMDD>.tar.gz
命令：
  docker save -o bilipro-v<日期>.tar bilipro:v<日期>
  gzip bilipro-v<日期>.tar              # 生成 bilipro-v<日期>.tar.gz
```

- 示例（2026-08-09 构建）：

```bash
docker save -o bilipro-v20260809.tar bilipro:v20260809
gzip bilipro-v20260809.tar              # 产物：bilipro-v20260809.tar.gz
```

- 交付/部署用 **.tar.gz** 文件：`docker load -i bilipro-v20260809.tar.gz`（docker 直接支持 gz）

## 四、标准构建脚本（WSL 内执行）

```bash
#!/bin/bash
set -e
TAG=v$(date +%Y%m%d)                     # 形如 v20260809
BUILD_DIR=/root/bilipro-build-$TAG
mkdir -p "$BUILD_DIR"
cd /mnt/d/WorkSpace/GitSpace/BiliBiliToolPro/bilipro
cp -r internal main.go go.mod go.sum config.example.yaml cookies.example.json Dockerfile "$BUILD_DIR"/

cd "$BUILD_DIR"
docker build -t bilipro:$TAG .
docker tag bilipro:$TAG bilipro:latest

cd /mnt/d/WorkSpace/GitSpace/BiliBiliToolPro/bilipro
docker save -o bilipro-$TAG.tar bilipro:$TAG
gzip -f bilipro-$TAG.tar
ls -lh bilipro-$TAG.tar.gz
```

## 五、产物存放

- 构建产物（.tar.gz）统一放在项目根目录：`D:\WorkSpace\GitSpace\BiliBiliToolPro\bilipro\`
- 镜像在 WSL docker 内保留（`bilipro:v<日期>` + `bilipro:latest`），不主动清理历史镜像

## 六、部署方式（服务器）

```bash
docker load -i bilipro-v20260809.tar.gz
docker images | grep bilipro        # 应显示 bilipro:v20260809
# 之后按 docs/deploy.md 初始化 config、login、compose up -d
```

## 七、构建前置条件（国内网络）

1. 镜像加速器已配置（参考 docs/wsl-build-notes.md §四）
2. `golang:1.24-alpine` 与 `alpine:3.21` 基础镜像已拉取
3. Dockerfile 已内置 GOPROXY（goproxy.cn）与 apk 阿里云源

## 八、变更记录

| 日期 | 变更 |
|------|------|
| 2026-08-09 | v1.0 本标准发布：统一 WSL 多阶段构建、日期 tag、save+gzip 导出命名规范 |
| 2026-08-09 | v1.1 修订：tag/文件名增加 `v` 前缀（`bilipro:v20260809`、`bilipro-v20260809.tar.gz`），消除纯日期歧义 |
| 2026-08-09 | v1.2 修订：功能迭代版本统一使用日期 tag（不再用 v0.x.x 语义号）；同日多次构建追加序号（v20260809-2） |
