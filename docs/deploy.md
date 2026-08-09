# bilipro Docker 长期运行部署文档（compose 常驻 cron 模式）

本文档描述如何在服务器上用 Docker Compose 将 bilipro 部署为**常驻定时模式**：
容器以 `restart: unless-stopped` 长期运行，启动后进入常驻调度模式，内置 cron 调度器按 `config/config.yaml` 的 `scheduler` 段（`enabled: true` + `cron` 表达式）**每日定时**执行每日任务；配置文件与 Cookie、日志通过**双卷挂载**持久化在宿主机，容器重建不丢失。

> 部署目标结构：
>
> ```
> bilipro/
> ├── docker-compose.yml   # 常驻 cron 模式编排
> ├── Dockerfile           # 多阶段构建（基础镜像可切换）
> ├── config/              # 挂载卷 1：config.yaml + cookies.json（持久化）
> └── logs/                # 挂载卷 2：bilitoolgo.log（结构化日志）
> ```

---

## 1. 前置要求

- 一台可长期在线的服务器（Linux x86_64 / arm64 均可）
- 已安装 **Docker Engine**（20.10+）
- 已安装 **Docker Compose 插件**（`docker compose` 子命令，非旧版 `docker-compose`）

验证：

```bash
docker version
docker compose version
```

> 若 `docker compose` 报 `command not found`，请安装 compose 插件（Debian/Ubuntu：`apt install docker-compose-plugin`；或参考 Docker 官方文档安装）。

## 2. 上传 / 克隆项目

将本项目（`bilipro/` 目录）上传到服务器，或直接克隆：

```bash
git clone <项目仓库地址> bilipro
cd bilipro
```

后续所有命令均在该目录（含 `docker-compose.yml` 的目录）下执行。

## 3. 初始化配置

```bash
# 3.1 创建挂载目录（config 存放配置+Cookie，logs 存放日志）
mkdir -p config logs

# 3.2 复制配置示例为实际配置
cp config.example.yaml config/config.yaml
```

按需修改 `config/config.yaml`（编辑器或 `vi config/config.yaml`）：

- **cron 时间**：`scheduler.cron`（6 段含秒，如 "0 30 8 * * *" = 每天 08:30:00），并**务必同时设置 `scheduler.enabled: true`**。注意：compose 的 `command` 只负责启动程序（["daily"]），**不再携带 `-cron` 参数**，调度完全由配置文件决定。
- **任务开关**：`tasks.daily.*`（看视频/分享/投币等）、`tasks.unfollow.*`
- **钉钉推送**：`push.dingtalk.enabled` / `webhook_url` / `secret`
- **随机沉默开关**：`security.random_sleep_enabled`（见下文「随机沉默与 cron 常驻的配合」）

> 首次部署时 Cookie 为空没关系，下一步用 `login` 命令交互扫码生成。

## 4. 构建镜像

```bash
docker compose build
```

构建产物标记为 `bilipro:latest`（`docker-compose.yml` 中 `image: bilipro:latest`）。默认基础镜像为 `alpine:3.21`，如需切换 distroless/scratch 等，参见 `Dockerfile` 顶部注释。

> 镜像只需构建一次；之后更新代码重新 build 即可（见第 9 节）。

## 5. 初始化 Cookie（交互扫码登录）

```bash
docker compose run --rm bilitool login
```

- 终端会打印二维码（以及链接），用**手机 B 站 App 扫码确认**即可
- 登录成功后 Cookie 写入**挂载卷** `config/cookies.json`（容器内路径 `/app/config/cookies.json`），容器重建/升级后 Cookie 依然保留
- `--rm` 表示该一次性容器运行完即删除，不影响常驻服务
- 多账号：重复执行 `login` 可追加账号

> 扫码登录在**有交互的终端**执行（SSH 直连即可）。若服务器完全无交互环境，可改为手动填写 Cookie：浏览器登录 bilibili.com → F12 → Network → 复制请求头中的 `Cookie` 到 `config/cookies.json`（格式见 `cookies.example.json`）。

## 6. 验证配置与 Cookie

```bash
docker compose run --rm bilitool test
```

输出 `连接成功` / Cookie 有效的日志即表示配置与登录均正常。此步骤可选但推荐，避免常驻后才发现配置错误。

## 7. 启动常驻服务

```bash
docker compose up -d
```

- `-d` 后台运行；容器以 `restart: unless-stopped` 策略常驻——**进程崩溃自动拉起**，宿主机重启后也会随 Docker 自动启动（`restart` 策略生效，Docker 服务需设为开机自启：`systemctl enable docker`）
- 容器启动后进入 **cron 调度模式**：按 `config/config.yaml` 中 `scheduler.cron` 每日 08:30:00 自动执行每日任务（需 `scheduler.enabled: true`），其余时间空闲等待，不再有"一次性跑完即退出"的行为

## 8. 验证运行状态

```bash
# 查看容器状态（STATUS 应为 Up）
docker compose ps

# 实时查看日志（观察"调度已启动"日志即表示 cron 常驻生效）
docker compose logs -f
```

预期日志（示意）：

```
time=... level=INFO msg="调度已启动" cron="0 30 8 * * *"
```

之后到点（如 08:30）日志会出现任务执行记录；cron 支持防重叠（上一任务未执行完自动跳过本次触发）。

**修改执行时间**：编辑 `config/config.yaml` 中 `scheduler.cron` 表达式（6 段，含秒），然后：

```bash
docker compose restart   # 重启容器使新调度时间生效
```

## 9. 更新流程

| 场景 | 操作 |
| ---- | ---- |
| 只改了 `config/config.yaml`（开关/钉钉等） | `docker compose restart`（重启容器，挂载配置即时生效） |
| 修改了 `config/config.yaml`（cron 时间 / 开关） | `docker compose restart`（重启容器） |
| 代码 / Dockerfile 变更（升级） | `docker compose build && docker compose up -d` |

升级流程示例：

```bash
# 拉取最新代码后
git pull
docker compose build
docker compose up -d
```

升级后 Cookie 与日志因挂载卷持久化不受影响；确认新版本运行正常后，可清理旧镜像：`docker image prune`。

## 10. 日志查看

```bash
# 实时跟踪容器日志
docker compose logs -f

# 只看最近 100 行
docker compose logs --tail=100

# 宿主机侧：结构化日志同时落盘到挂载目录
tail -f logs/bilitoolgo.log
```

日志文件 `logs/bilitoolgo.log`（JSON 结构化格式）持久化在宿主机 `./logs/` 目录，容器删除/重建不丢失，便于后续接入 logrotate 等宿主机日志轮转工具。

---

## 挂载目录权限说明（重要）

容器内默认以**非 root 用户（UID 10001）**运行（Dockerfile `USER ${APP_UID}:${APP_UID}`）。宿主机 bind mount 的目录若不可写，会导致容器内无法写 Cookie/日志。首次部署建议：

```bash
# 方式一：放开权限（简单直接）
chmod -R 777 config logs

# 方式二：改为容器用户属主（更规范）
chown -R 10001:10001 config logs
```

> 若登录/运行时报 `permission denied` 写文件失败，优先检查此项。

## 随机沉默开关与 cron 常驻的配合

`config.yaml` 的 `security` 段提供随机沉默开关：

```yaml
security:
  # 随机沉默开关：开启后任务触发时随机沉默一段时间再执行（防定时集中触发）
  # 沉默时长由程序随机，最晚不超过当日 23:00:00 开始执行（为任务留足运行时间，不会跨天）
  random_sleep_enabled: false
```

- 设为 `true` 后，**每次 cron 触发任务时**，程序先随机沉默一段时间（0 ~ 到当日 23:00:00 的剩余秒数，沉默后最晚 23:00:00 前开始执行）再执行，避免大批定时任务集中在整点触发被风控
- 与 cron 常驻的配合：常驻模式每天到点触发 → 先随机沉默 → 执行任务 → 回到等待状态；沉默不影响调度本身，cron 防重叠机制依然生效
- 修改后 `docker compose restart` 生效
- 一次性模式（`docker compose run --rm bilitool daily`）同样受该开关影响；也可用 `-random-sleep` flag 覆盖
