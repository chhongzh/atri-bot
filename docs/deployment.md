# 部署

atri-bot 可以直接跑二进制，也可以用 Docker。两种方式都只需要一个可执行文件或一个镜像，没有别的运行时依赖。

## 直接跑二进制

1. 到 [Releases](https://github.com/chhongzh/atri-bot/releases) 页面下载对应平台的压缩包，解压得到一个可执行文件。Windows、macOS、Linux 和 Android（arm64 / amd64）都有构建产物。也可以从源码构建。

```bash
go build -o atri-bot ./cmd/atri-bot
```

2. 在运行目录创建 `config.yaml`，至少填 bot token。完整字段见 [configuration.md](configuration.md)。

```yaml
telegram:
  bot_token: "你的 bot token"
```

3. 运行。

```bash
./atri-bot
```

4. 给机器人发第一条消息。第一个交互的用户自动成为管理员。

数据库文件默认生成在运行目录下的 `atri-bot.db`，角色、媒体和远程角色缓存放在 `chardefs`、`data` 目录。想换地方，改 `config.yaml` 里的 `atri_cwd`。

## Docker 部署

### 拉镜像

镜像发布在 GitHub Container Registry。

```bash
docker pull ghcr.io/chhongzh/atri-bot:latest
```

镜像同时提供 `linux/amd64` 和 `linux/arm64`，Docker 会按宿主架构自动选择。想固定版本，用 `ghcr.io/chhongzh/atri-bot:<版本号>` 这样的 tag。

### 准备数据目录

机器人把配置、数据库、角色和媒体都放在容器里的 `/data`。挂载一个宿主目录，升级容器才不会丢数据。

```bash
mkdir -p /data/atri
```

在挂载目录里创建 `config.yaml`，至少填 bot token。

```yaml
telegram:
  bot_token: "你的 bot token"
```

### docker run 启动

```bash
docker run -d --name atri-bot \
  -e TZ=Asia/Shanghai \
  -v /data/atri:/data \
  --restart unless-stopped \
  ghcr.io/chhongzh/atri-bot:latest
```

几个常用参数的意思。

- `-e TZ=Asia/Shanghai` 设置容器时区。镜像默认就是 `Asia/Shanghai`，这一项可以省略。想用别的时区就改这里。
- `-v /data/atri:/data` 把宿主目录挂载成数据目录。
- `--restart unless-stopped` 让容器跟随 Docker 自动重启。

看日志用 `docker logs -f atri-bot`。修改 `config.yaml` 后执行 `docker restart atri-bot` 生效。

### docker compose 启动

仓库根目录有一份 [docker-compose.yml](../docker-compose.yml)，复制到部署目录就能用。

```bash
mkdir -p atri-data
cp docker-compose.yml .
docker compose up -d
```

compose 文件和上面的 docker run 等价，默认把当前目录下的 `atri-data` 挂载成 `/data`。记得先在 `atri-data` 里放好 `config.yaml`。

### 时区

镜像安装了完整的 `tzdata`，默认时区是 `Asia/Shanghai`。容器启动后 `/etc/localtime`、`/etc/timezone` 和 `TZ` 环境变量都会指向这个时区。

想完全跟随宿主机，启动时挂载宿主机的时区文件。

```bash
docker run -d --name atri-bot \
  -v /etc/localtime:/etc/localtime:ro \
  -v /etc/timezone:/etc/timezone:ro \
  -v /data/atri:/data \
  --restart unless-stopped \
  ghcr.io/chhongzh/atri-bot:latest
```

也可以只设置 `TZ` 环境变量，容器会读取 `/usr/share/zoneinfo` 里对应的时区数据。

### 升级

```bash
docker pull ghcr.io/chhongzh/atri-bot:latest
docker rm -f atri-bot
docker compose up -d
```

数据都在挂载目录里，删容器不丢。数据库文件是 SQLite，升级前想更稳妥可以先备份整个挂载目录。

## 常见问题

- 启动后机器人没反应。先看日志，`docker logs atri-bot`，或者直接跑二进制看终端输出，确认 `config.yaml` 的 bot token 正确。
- 容器里想用 web_read 工具。镜像没有内置浏览器，需要在宿主机或另一个容器跑一个带调试端口的浏览器，再把地址填进 `config.yaml` 的 `external.browser_url`。
- 数据库想用 MySQL。在 `config.yaml` 里把 `database.type` 改成 `mysql` 并填 `database.dsn`。镜像装了完整的根证书，连接远程 MySQL 不需要额外配置。
