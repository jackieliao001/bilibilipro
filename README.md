# bilitoolgo

B 站自动化任务 CLI —— BiliBiliToolPro 的 Go 重写版。

基于 [BiliBiliToolPro](https://github.com/RayWangQvQ/BiliBiliToolPro) 的功能设计，使用 Go 1.22 标准库 + `gopkg.in/yaml.v3` 实现，零第三方业务依赖，可离线编译，单二进制分发，支持 Docker 部署。
## 致谢

本项目是 [RayWangQvQ/BiliBiliToolPro](https://github.com/RayWangQvQ/BiliBiliToolPro) 的 **Go 语言重写版**，参考了原项目的功能设计与 B 站 API 调用逻辑。感谢原作者 **RayWangQvQ** 的开源贡献（原项目 MIT License）。


## 功能

| 命令 | 说明 |
| ---- | ---- |
| `login` | 扫码登录，生成并保存 Cookie |
| `daily` | 每日任务：观看视频、分享视频、投币、获取每日经验 |
| `unfollow` | 取关任务：清理指定分组（如"天选时刻"）中关注的大量账号 |
| `test` | 连通性测试：校验配置与 Cookie 是否有效 |

## 快速开始

```bash
# 1. 构建
go build -o bilipro ./main.go

# 2. 准备配置（首次）
cp config.example.yaml config.yaml

# 3. 连通性测试
./bilipro test

# 4. 扫码登录获取 Cookie（会写入 cookies.json）
./bilipro login

# 5. 执行每日任务
./bilipro daily

# 6. 执行取关任务
./bilipro unfollow
```

> 二进制也可通过 `-config path` / `-cookies path` 指定配置文件路径；
> 未指定时依次使用环境变量 `CONFIG_PATH` / `COOKIE_PATH`（Docker 镜像内置默认值），
> 最后回退到运行目录下的 `config.yaml` / `cookies.json`。

### 环境变量一览

| 环境变量 | 作用 | 优先级 |
| --------- | ---- | ------ |
| `CONFIG_PATH` | 配置文件路径 | `-config` flag > CONFIG_PATH > `config.yaml` |
| `COOKIE_PATH` | Cookie 文件路径 | `-cookies` flag > COOKIE_PATH > RAY_COOKIE_FILE > `cookies.json` |
| `LOG_FILE` | 日志文件路径（配置中 `log.file` 为空时生效） | `log.file` 配置 > LOG_FILE > 仅控制台 |
| `RAY_COOKIE_FILE` | Cookie 文件路径（旧版兼容） | 见 COOKIE_PATH |
| `RAY_LOG_LEVEL` | 日志级别覆盖（debug/info/warn/error） | 高于配置文件 |
| `RAY_DINGTALK_WEBHOOK` | 钉钉 Webhook 地址（设置后自动启用推送） | 高于配置文件 |

### 常用 flag 一览

| flag | 作用 | 默认值 |
| ---- | ---- | ------ |
| `-config path` | 配置文件路径 | `CONFIG_PATH` 或 `config.yaml` |
| `-cookies path` | cookies JSON 文件路径 | `COOKIE_PATH` / `RAY_COOKIE_FILE` 或 `cookies.json` |
| `-accounts 1,2` | 账号索引列表（1-based，逗号分隔） | 全部 |
| `-debug` | 开启 debug 日志级别 | 关闭 |
| `-cron expr` | 进入常驻调度模式（6 段 cron，含秒） | 读取 `config.scheduler` |
| `-random-sleep` | 启用随机沉默（时长程序随机，最晚不超过当日 23:00:00 开始执行） | 读取 `security.random_sleep_enabled` |

## 定时调度

内置 cron 调度器，支持到点自动执行任务（常驻进程模式，Ctrl+C 优雅退出）：

```bash
# 每天 08:30:00 执行每日任务
./bilipro daily -cron "0 30 8 * * *"
```

也可以写在 `config.yaml` 中，效果与 `-cron` 相同（flag 优先于配置）：

```yaml
scheduler:
  enabled: true   # 是否常驻调度
  cron: "0 30 8 * * *"  # 每天 08:30:00
```

### 随机沉默

任务触发后先随机沉默一段时间再执行，避免定时任务集中触发被风控。沉默时长由**程序随机**，最晚不超过当日 23:00:00 开始执行（为任务留足运行时间，不会跨天）：

- 命令行：`./bilipro daily -random-sleep`
- 配置文件：`security.random_sleep_enabled: true`
- `false` 或未配置表示禁用（不沉默）；不指定 flag 时默认取配置值
- 一次性模式（不带 `-cron`）同样生效：启动后先随机沉默再执行任务

### 防重叠

若上一次任务尚未执行完，本次 cron 触发会自动跳过（不会并发执行两个任务实例），无需额外配置。

### cron 表达式说明

本工具使用 **6 段 cron 表达式，第一位是秒**（与 Linux crontab 的 5 段不同）：

```
秒 分 时 日 月 星期
0  30 8  *  *  *
```

示例：

- `"0 30 8 * * *"`：每天 08:30:00
- `"0 */30 * * * *"`：每 30 分钟
- `"0 0 9 * * 1"`：每周一 09:00:00


## 配置说明

所有配置项见 [config.example.yaml](config.example.yaml)，复制为 `config.yaml` 后按需修改：

- **log**：日志级别（`debug`/`info`/`warn`/`error`）与日志文件路径，`file` 留空则仅输出控制台。
- **bilibili**：多账号任务间隔秒数（建议 10-20，防限流）、请求 UA、HTTP 代理（留空直连）。
- **security**：`random_sleep_enabled` 随机沉默开关（开启后任务触发时随机沉默一段时间再执行，时长程序随机，最晚不超过当日 23:00:00 开始执行，防定时集中触发）。
- **scheduler**：常驻调度开关（`enabled`）与 cron 表达式（`cron`，6 段含秒，等价于 `-cron` 参数）。
- **cookies**：账号列表；为安全起见建议留空，真实 Cookie 放在 `cookies.json`（见下）。
- **tasks.daily**：每日任务开关与参数 —— 是否看视频/分享/投币（三个开关**默认开启**，不配置或 `true` 均视为开启；显式设为 `false` 时每日任务执行到对应环节会跳过并继续下一步）、投币数（1-5，需 `donate_coin` 开启且数量>0 才投）、保留硬币阈值、Lv6 后是否省币、是否投专栏、优先支持的 UP 主 UID、上报设备平台。
- **tasks.unfollow**：取关任务开关、目标分组名、取关数量（0=全部）、保留 UID 列表。
- **push.dingtalk**：钉钉机器人推送开关、Webhook 地址、加签密钥。

### Cookie 获取与格式

`cookies.json` 格式见 [cookies.example.json](cookies.example.json)：

```json
{
  "cookies": [
    { "name": "我的账号", "cookie_str": "SESSDATA=xxx; bili_jct=xxx; DedeUserID=xxx" }
  ]
}
```

**获取方式**：浏览器登录 bilibili.com 后，按 F12 打开开发者工具 → Network 标签 → 刷新页面 → 任选一个请求 → 复制 Request Headers 中的 `Cookie` 头内容，粘贴到 `cookie_str` 字段。

> ⚠️ Cookie 等同账号凭证，请勿提交到 Git（已加入 .gitignore），也不要泄露给他人。

## Docker 部署

### 方式一：docker compose（推荐）—— 常驻 cron 定时模式

```bash
# 1. 构建镜像
cd bilipro
mkdir -p config logs
cp config.example.yaml config/config.yaml
# 按需修改 config/config.yaml（scheduler.enabled: true + scheduler.cron 时间、任务开关、钉钉推送、随机沉默开关）

docker compose build

# 2. 交互扫码登录，初始化 Cookie（写入挂载卷 config/cookies.json）
docker compose run --rm bilitool login

# 3. 验证配置与 Cookie（可选但推荐）
docker compose run --rm bilitool test

# 4. 启动常驻：容器后台长期运行，内置 cron 每日 08:30 自动执行任务
#    （restart: unless-stopped，崩溃自动拉起；config/logs 双卷挂载持久化）
docker compose up -d

# 5. 查看日志（出现"调度已启动"即 cron 常驻生效）
docker compose logs -f
```

默认每日 08:30:00 执行（scheduler.cron 在 config/config.yaml 的 scheduler 段配置，6 段含秒；请确认 scheduler.enabled: true，修改后 docker compose restart 生效）。完整的部署、更新升级、目录权限（容器内非 root 10001 运行）与随机沉默开关说明见 **[docs/deploy.md](docs/deploy.md)**。

### 方式二：docker run

```bash
# 一次性执行每日任务
docker run --rm \
  -v "$(pwd)/config:/app/config" \
  -v "$(pwd)/logs:/app/logs" \
  ghcr.io/raywangqvq/bilitoolgo:latest daily

# 扫码登录（生成 cookies.json 持久化到 ./config）
docker run --rm -it \
  -v "$(pwd)/config:/app/config" \
  ghcr.io/raywangqvq/bilitoolgo:latest login
```

### cron 定时执行（Linux 宿主机）

```cron
# 每天 09:30 执行一次每日任务
30 9 * * * docker run --rm -v /opt/bilitool/config:/app/config -v /opt/bilitool/logs:/app/logs ghcr.io/raywangqvq/bilitoolgo:latest daily >> /opt/bilitool/logs/cron.log 2>&1
```

镜像默认推送到 `ghcr.io/raywangqvq/bilitoolgo`，国内网络可替换为 `docker.io/zai7lou/bilitoolgo`。

### 基础镜像选择

Dockerfile 支持切换基础镜像（默认 alpine:3.21，已升版替换 EOL 的 3.19）：

| 镜像 | 大小 | 特点 | 适用 |
|------|------|------|------|
| alpine:3.21（默认） | ~3.4 MB | 有 shell/apk，排障方便 | 通用部署 |
| distroless/static-debian12:nonroot | ~2 MB | 无 shell，内置 CA/tzdata，预置非 root | 生产/安全优先 |
| scratch | 0 B | 极致体积（base-files 阶段自动制备证书/时区） | 极致优化 |
| chainguard/static | ~2-3 MB | 官方 0 CVE 承诺 | 合规审计 |

构建方式：

``bash
# 默认 alpine
docker build -t bilipro .
# distroless 变体（独立 Dockerfile）
docker build -f Dockerfile.distroless -t bilipro:distroless .
# 单 Dockerfile 切换基镜像
docker build --build-arg BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot --build-arg APP_UID=65532 -t bilipro .
``


## 消息推送（多渠道，默认关闭）

支持 6 个推送渠道（**全部默认关闭**，启用后任务完成自动推送结果摘要）：

| 渠道 | 配置段 | 必填参数 | 说明 |
|------|--------|----------|------|
| 钉钉 | push.dingtalk | webhook_url | 支持加签（secret） |
| Server酱 | push.serverchan | send_key | Turbo 版，markdown 正文 |
| PushPlus | push.pushplus | token | channel/topic/webhook 可选 |
| 企业微信机器人 | push.workweixin | webhook_url | 群机器人 markdown |
| Telegram | push.telegram | bot_token + chat_id | 支持代理与自定义 api_host |
| 自定义 API | push.custom_api | url | 模板占位符替换，兜底任意服务 |

启用方式（以钉钉为例，其余渠道同理）：

1. 在钉钉群「群设置 → 智能群助手 → 添加机器人 → 自定义」创建机器人；
2. 在 `config/config.yaml` 的 `push` 段填写对应渠道配置（见 config.example.yaml 注释），将 `enabled` 设为 `true`；
3. 或直接设置环境变量自动启用（设置即生效）：`RAY_DINGTALK_WEBHOOK`、`RAY_SERVERCHAN_KEY`、`RAY_PUSHPLUS_TOKEN`、`RAY_WORKWEIXIN_WEBHOOK`、`RAY_TELEGRAM_BOT_TOKEN`+`RAY_TELEGRAM_CHAT_ID`、`RAY_CUSTOM_API_URL`；
4. 多渠道可同时启用，任务完成后并发推送；单个渠道失败不影响其他渠道。

## 目录结构

```
bilipro/
├── main.go                  # 入口：参数解析、配置加载、任务分发
├── go.mod / go.sum
├── config.example.yaml      # 配置示例
├── cookies.example.json     # Cookie 文件示例
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── .gitignore
├── README.md
├── docs/
│   └── deploy.md            # Docker 长期运行部署文档（compose 常驻 cron 模式）
└── internal/
    ├── model/               # 数据模型：Cookie / Config / B 站响应 / 任务结果
    ├── api/                 # HTTP 客户端、中间件、Wbi 签名
    │   └── bilibili/        # B 站各业务接口封装
    ├── log/                 # slog 日志初始化（控制台 + 文件双输出）
    ├── push/                # 推送抽象与钉钉实现（支持加签）
    ├── scheduler/           # cron 常驻调度与随机沉默
    └── task/                # 任务层：login / daily / unfollow / test
```

## 免责声明

本项目仅供学习与测试使用，请勿用于任何商业或非法用途；请合理控制任务频率，遵守 B 站用户协议与相关法律法规。使用本项目造成的一切后果由使用者自行承担。
