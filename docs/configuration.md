# 配置说明

运行配置文件是工作目录下的 `config.yaml`。必填项只有 Telegram Bot Token，其余字段都有默认值。

## 最小配置

```yaml
telegram:
  bot_token: "你的 bot token"
```

## 完整示例

```yaml
telegram:
  bot_token: "你的 bot token"

# 新用户默认值
default:
  max_rounds: 12            # 新用户会话轮数上限
  image_max_edge: 1024      # 新用户图片最长边默认值，最大 2048
  mcp_max_tools: 128        # 每用户 MCP provider 上限
  tool_permissions: { }      # 新用户工具默认权限

# 网络安全
security:
  allow_private_ip: false   # 是否允许访问 localhost、内网域名和私网 IP

# 数据库
database:
  type: sqlite              # sqlite 或 mysql
  path: "atri-bot.db"       # sqlite 文件路径，相对 atri_cwd
  # dsn: "user:pass@tcp(127.0.0.1:3306)/atri?charset=utf8mb4&parseTime=True&loc=Local"  # mysql 连接串

# 外部服务
external:
  browser_url: ""           # 配置后启用 web_read 工具，例如 127.0.0.1:9222

# 本地媒体缓存
files:
  max_storage_mb: 1024      # 全局媒体池空间上限
  cleanup_after: 7d         # 文件保留时间，支持 1d、3d、24h 等格式

# 数据根目录
atri_cwd: "."
```

## 字段说明

### telegram

- `bot_token` 必填。从 @BotFather 创建机器人拿到的 token。

### default

- `max_rounds` 新用户压缩会话历史前保留的完整 Telegram 轮数，默认 12。之后每个用户可以用 `/ai rounds` 单独修改。一次 TurnLoop 从收到的一组 user 消息开始，包含其间所有 assistant、tool call 和 tool result，直到最终 assistant 回复结束，整体计为一轮。达到上限后会先阻塞新一轮，用该用户自己的模型生成带 cutoff 的 system history。原始轮次继续保留，后续加载只读取最新 system history 与 cutoff 之后的新轮次。
- `image_max_edge` 新用户图片最长边默认值，默认 1024，最大 2048。用户可用 `/ai image-size` 单独修改。
- `mcp_max_tools` 每个用户可添加的 MCP provider 上限，默认 128。管理员可用 `/mcp limit` 单独覆盖。
- `tool_permissions` 新用户的默认工具权限映射。

### security

- `allow_private_ip` 是否允许用户配置的网络出口访问 localhost、内网域名和私网 IP，默认 false。开启后 web_read、MCP 等工具可以访问内网地址。

### database

- `type` 数据库类型，`sqlite`（默认）或 `mysql`。
- `path` sqlite 数据库文件路径，相对 `atri_cwd`，默认 `atri-bot.db`。
- `dsn` mysql 连接串，`type: mysql` 时必填。

### external

- `browser_url` 启用网页读取工具 `web_read` 所需的浏览器调试地址，例如 `127.0.0.1:9222`。未配置时不注册该工具。

### files

- `max_storage_mb` 全局本地媒体池上限，默认 1024 MB。媒体按 SHA-256 摘要全局去重，单文件最大 20 MB。
- `cleanup_after` 本地媒体保留时间，默认 `7d`。支持 `1d`、`3d` 及 `time.ParseDuration` 接受的格式。

### atri_cwd

数据和本地角色的根目录，默认当前目录。数据库文件、角色缓存和本地媒体都放在这里。

## 数据目录

运行时会在 `atri_cwd` 下创建以下目录。

- `chardefs` 本地角色目录，文件名去掉扩展名就是角色 ID。
- `data/character-providers` 远程角色仓库的克隆缓存。
- `data/files` 本地媒体文件。

Docker 部署时把挂载目录映射到 `/data`，升级容器不丢数据。具体见 [deployment.md](deployment.md)。
