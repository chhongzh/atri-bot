<a id="readme-top"></a>

<!-- 项目徽章 -->
[![Contributors](https://img.shields.io/github/contributors/chhongzh/atri-bot.svg?style=for-the-badge)](https://github.com/chhongzh/atri-bot/graphs/contributors)
[![Stars](https://img.shields.io/github/stars/chhongzh/atri-bot.svg?style=for-the-badge)](https://github.com/chhongzh/atri-bot/stargazers)
[![Issues](https://img.shields.io/github/issues/chhongzh/atri-bot.svg?style=for-the-badge)](https://github.com/chhongzh/atri-bot/issues)
[![License](https://img.shields.io/github/license/chhongzh/atri-bot.svg?style=for-the-badge)](https://github.com/chhongzh/atri-bot/blob/master/LICENSE)

<br />
<div align="center">
  <h3 align="center">atri-bot</h3>

  <p align="center">
    把二次元角色带到现实，让 Ta 真正做事
    <br />
    <a href="#开始使用">快速开始（推荐自部署）</a>
    &middot;
    <a href="https://github.com/chhongzh/atri-bot/issues">报告问题</a>
    &middot;
    <a href="https://github.com/chhongzh/atri-bot/issues">请求功能</a>
  </p>
</div>

<!-- 目录 -->
<details>
  <summary>目录</summary>
  <ol>
    <li><a href="#项目简介">项目简介</a></li>
    <li><a href="#开始使用">开始使用</a></li>
    <li><a href="#使用方法">使用方法</a></li>
    <li><a href="#后续计划">后续计划</a></li>
    <li><a href="#参与贡献">参与贡献</a></li>
    <li><a href="#开源协议">开源协议</a></li>
    <li><a href="#联系方式">联系方式</a></li>
    <li><a href="#致谢">致谢</a></li>
  </ol>
</details>

## 项目简介

atri-bot 是一个跑在 Telegram 上的角色聊天机器人。多数同类项目把功夫花在扮演上，人设丰满，台词动人，剧情能陪你演很久，但角色对现实世界没有任何手段。atri-bot 受 openclaw 这类 agent 项目的启发，在扮演能力之外加了工具能力。角色可以自己决定调用什么工具，把结果带回对话里继续聊。它是你喜欢的那个角色，同时手里有工具。

项目的出发点是把二次元角色带到现实。想要天气就自己查，想发邮件就自己发，想通过 MCP 接外部服务也可以。工具目前的重点在 MCP，内置工具里发送邮件和热点搜索已经能用，其余还在开发。

机器人是多用户的。每个用户有独立的 AI 连接配置、角色、会话历史和工具权限，互相不干扰。切换角色随时可以，会话历史按角色隔离，不会串戏。

### 官方实例（最后选择）

建议优先自己部署。运行只需要一个可执行文件，按下面的步骤来，几分钟就能跑起来。官方公共机器人 [@AtriBotIsAChatBot](https://t.me/AtriBotIsAChatBot) 是给想先体验、或者暂时没有条件部署的人用的，属于最后的选择。官方实例有两处限制，不允许访问内网地址，也不承诺 100% 在线，大多数时候在线。自部署没有这些限制，数据也完全在自己手里。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 开始使用

运行 atri-bot 只需要一个可执行文件。项目用 Go 编译成单个二进制，数据库内嵌在程序里，不需要安装任何运行时环境。内存占用低，响应快。Windows、macOS、Linux 和 Android（arm64 / amd64）都有现成的构建产物。

### 准备工作

- 一个 Telegram Bot Token。找 @BotFather 创建机器人就能拿到。
- 每位用户自己的 AI 连接信息。项目没有共享的 AI 默认配置，base URL、API key、模型都由用户自己设置，具体见下面的 `/ai` 命令。
- 一台能运行对应平台可执行文件的机器，或者一台装了 Go 的机器用来自己编译。

### 安装

1. 下载。到 Releases 页面下载对应平台的压缩包，解压后得到一个可执行文件。
2. 或者从源码构建。

```bash
go build -o atri-bot ./cmd/atri-bot
```

3. 在运行目录创建 `config.yaml`，至少填写 bot token。

```yaml
telegram:
  bot_token: "你的 bot token"

# 新用户默认值
default:
  max_rounds: 36            # 新用户会话轮数上限
  mcp_max_tools: 128        # 每用户 MCP provider 上限
  tool_permissions: {}      # 新用户工具默认权限

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

# 数据根目录
atri_cwd: "."
```

4. 运行。

```bash
./atri-bot
```

5. 给你的机器人发第一条消息。第一个交互的用户自动成为管理员，管理员可以管理其他用户的权限和 MCP 设置。
6. 配置 AI。每位用户都要设置自己的连接信息，项目没有全局的 AI 默认值。

```text
/ai base-url https://api.openai.com/v1
/ai key <你的 api key>
/ai model <模型名>
/ai rounds 36
```

各项配置的含义如下。

- `telegram.bot_token` 必填，机器人的 token。
- `default.max_rounds` 新用户压缩会话历史前保留的完整 Telegram 轮数，之后每个用户可以用 `/ai rounds` 单独修改。一次 TurnLoop 从收到的一组 user 消息开始，包含其间所有 assistant、tool call 和 tool result，直到最终 assistant 回复结束，整体计为一轮。达到上限后会先阻塞新一轮，用该用户自己的模型生成带 cutoff 的 system history；原始轮次继续保留，后续加载只读取最新 system history 与 cutoff 之后的新轮次。
- `default.mcp_max_tools` 每个用户可添加的 MCP provider 上限，默认 128，管理员可用 `/mcp limit` 单独覆盖。
- `default.tool_permissions` 新用户的默认工具权限映射。
- `security.allow_private_ip` 是否允许用户配置的网络出口访问 localhost、内网域名和私网 IP，默认 false。
- `database.type` 数据库类型，`sqlite`（默认）或 `mysql`。
- `database.path` sqlite 数据库文件路径，相对 `atri_cwd`，默认 `atri-bot.db`。
- `database.dsn` mysql 连接串，`type: mysql` 时必填。
- `external.browser_url` 启用网页读取工具 `web_read` 所需的浏览器调试地址，例如 `127.0.0.1:9222`；未配置时不注册该工具。
- `atri_cwd` 数据和本地角色的根目录，默认当前目录。数据库文件和远程角色缓存都放在这里。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 使用方法

所有命令都通过文本消息发送，参数支持引号，带空格的参数用引号包起来。发送 `/help` 可以按权限查看当前可用的命令列表。

主要命令如下。

- `/ai show|base-url|key|model|rounds [值]` 查看或修改自己的 AI 配置。
- `/characters` 列出所有可用角色。
- `/character <角色ID>` 查看详情或切换角色。
- `/toolperm list|allow|deny|reset <用户ID> [工具名]` 管理员管理用户的工具权限。
- `/mcp show|limit <用户ID> [值]` 管理员管理用户的 MCP 工具数量上限。
- `/providers`、`/provider add|set|remove|refresh` 管理员管理角色来源。
- `/admin stats|promote|demote|ban|unban|delete <用户ID>`、`/users`、`/user`、`/admins`、`/active-users` 管理员管理账户和运行状态。

### 工具与 MCP

工具权限按用户隔离。被禁用的工具会从模型可见的工具列表里消失，模型根本看不到它，对话也不会被反复打断。管理员用 `/toolperm` 管理。

```text
/toolperm list <用户ID>
/toolperm allow <用户ID> <工具名>
/toolperm deny <用户ID> <工具名>
/toolperm reset <用户ID> <工具名>
```

MCP 是当前工具能力的重点。角色通过 MCP 协议调用外部服务，具体能做什么取决于你添加的 provider。权限名 `mcp` 是总开关，`/toolperm deny <用户ID> mcp` 可以一键禁用某用户的全部 MCP 能力。

```text
/toolperm deny <用户ID> mcp
/toolperm reset <用户ID> mcp
```

管理员可以单独覆盖某个用户的 provider 上限；网络访问范围由全局 `security.allow_private_ip` 统一控制。

```text
/mcp show <用户ID>
/mcp limit <用户ID> <正整数|0>
```

### 角色与 chardef

角色文件用 YAML 编写，遵循 [chardef 规范](https://github.com/mihari-bot/chardef-spec)。本地角色放在 `<atri_cwd>/chardefs/<角色ID>.yaml`，文件名去掉扩展名就是角色 ID。

项目默认从 [mihari-bot/chardef](https://github.com/mihari-bot/chardef) 的 `v2` 分支加载角色，这个仓库欢迎所有人提交。想优化某个角色的提示词，或者为自己喜欢的角色写一份人设文件，直接去那里开 PR。提示词决定角色怎么说话、怎么用工具，是角色体验的核心。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 后续计划

- [ ] RAG 记忆库接入。给角色一个可检索的长期记忆，不再只靠窗口内的对话历史。
- [ ] 更多 Web 相关内置工具。查资料、读网页这类能力会陆续补上。
- [ ] Docker 部署。目前直接跑二进制，容器化部署在计划中。

当前状态说明。角色切换、多用户、工具权限、MCP 管理这些核心功能已经可用。

### 明确不做的事

插件系统不打算做。插件能提供的能力，MCP 都能提供，角色通过 MCP 接外部服务已经覆盖了这部分场景。Web 管理界面同样不打算做，配置用 Telegram 里的命令就够，没必要再做一个网页。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 参与贡献

任何形式的贡献都欢迎，代码、文档、测试、问题反馈都可以。最轻的入门方式是去 [mihari-bot/chardef](https://github.com/mihari-bot/chardef) 写一个角色，或者改进现有角色的提示词。这不碰代码，却直接决定角色的体验。

代码贡献的流程和其他开源项目一样。先 fork，再开分支，最后提 Pull Request。

1. Fork 项目仓库。
2. 创建功能分支（`git checkout -b feature/你的功能`）。
3. 提交修改。
4. 推送到分支。
5. 开一个 Pull Request。

报告问题或请求功能，直接开 [issue](https://github.com/chhongzh/atri-bot/issues)，写清楚复现步骤就行。

### 发布

PR 或分支推送时，CI 会自动跑一次 goreleaser snapshot，验证所有目标平台都能编译，不创建 Release。手动发布在 GitHub Actions 里触发，填版本号和是否草稿。版本号自动补 `v` 前缀并打 tag，默认以草稿形式发布，确认无误后在 Release 页面手动发布。Android 产物在 CI 中用 Android NDK 交叉编译。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 开源协议

项目以 MIT 协议开源，详见 [LICENSE](LICENSE)。商用、修改、分发都允许，保留版权声明即可。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 联系方式

官方机器人是 [@AtriBotIsAChatBot](https://t.me/AtriBotIsAChatBot)，项目主页在 [https://github.com/chhongzh/atri-bot](https://github.com/chhongzh/atri-bot)。欢迎到仓库的 issue 区反馈问题。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

## 致谢

- [mihari-bot/chardef](https://github.com/mihari-bot/chardef) 社区提供的角色定义与规范。
- [Best-README-Template](https://github.com/othneildrew/Best-README-Template) 提供的 README 结构。
- openclaw 这类 agent 项目，启发了角色调用工具的设计。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>
