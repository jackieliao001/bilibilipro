# bilipro 多阶段构建说明（WSL / 服务器通用）

> 更新日期：2026-08-08
> 方案说明：本项目 Docker 镜像采用**多阶段构建**（方案 A，标准方式），不再维护"预编译二进制注入"方案（相关文件已清理）。

## 一、构建方式（多阶段，唯一标准方式）

```
Dockerfile（多阶段）
├── Stage 1: builder      golang:1.24-alpine 编译（GOPROXY=goproxy.cn）
├── Stage 2: base-files   alpine:3.21 制备 CA 证书 + 时区 + 目录骨架
└── Stage 3: runtime      alpine:3.21（可切换 distroless/scratch）运行层
```

一条命令从源码出镜像，可复现：

```bash
# 默认（alpine:3.21 运行层）
docker build -t bilipro:latest .

# 生产/安全优先（distroless，无 shell 非 root，独立变体）
docker build -f Dockerfile.distroless -t bilipro:distroless .

# 切换运行基镜像（ARG BASE_IMAGE，需配套 APP_UID）
docker build --build-arg BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot \
             --build-arg APP_UID=65532 -t bilipro:latest .
```

或使用 compose（默认走 Dockerfile）：

```bash
docker compose build
```

## 二、镜像产物

| 产物 | 说明 |
|------|------|
| `bilipro:latest` / `bilipro:multistage` | 多阶段构建版（15.3MB，alpine:3.21 + 静态二进制） |
| `bilipro-multistage.tar` | 镜像导出文件，`docker load -i` 即用 |

## 三、构建前置条件

1. 基础镜像（国内网络建议配置 docker 镜像加速器，见下）：
   - `golang:1.24-alpine`（构建器，约 262MB）
   - `alpine:3.21`（运行层，约 7.8MB）
2. 依赖下载走 GOPROXY（Dockerfile 已内置 `https://goproxy.cn,direct`）
3. 构建器内 apk 安装走 alpine 软件源（Dockerfile 已切阿里云镜像）

## 四、镜像加速器配置（国内网络）

编辑 `/etc/docker/daemon.json`（参考 `wsl_daemon.json`），配置后重启 docker：

```json
{
  "registry-mirrors": [
    "https://docker.1panel.live",
    "https://docker.m.daocloud.io",
    "https://docker.1ms.run",
    "https://hub.rat.dev",
    "https://vi7h4g7w.mirror.aliyuncs.com"
  ]
}
```

## 五、已知环境问题（WSL2 + docker.io 包）

WSL2 上 docker.io 23.0.6 存在 runc 兼容缺陷，`docker run` 启动任何容器均报
`error running prestart hook #0: fork/exec /proc/<pid>/exe`（runc 升级至 1.2.6
后依旧）。**镜像本身无问题**，在标准 Linux 服务器 / Docker Desktop / 新版
docker-ce 上可正常运行。本机 WSL 修复选项：`wsl --update` 更新内核、更换
docker-ce 官方仓库版、或安装 Docker Desktop。

## 六、部署（服务器）

```bash
docker load -i bilipro-multistage.tar
mkdir -p config logs && cp config.example.yaml config/config.yaml
# 修改 config.yaml：scheduler.enabled: true + scheduler.cron（每日执行时间）
docker run --rm -v ./config:/app/config -v ./logs:/app/logs bilipro:latest login
docker compose up -d        # 常驻定时运行
docker compose logs -f
```

详细部署流程见 [deploy.md](deploy.md)。
