<!-- Improved compatibility of back to top link: See: https://github.com/othneildrew/Best-README-Template/pull/73 -->
<a id="readme-top"></a>

<!-- PROJECT SHIELDS -->
[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![MIT License][license-shield]][license-url]

<!-- PROJECT LOGO -->
<br />
<div align="center">
  <h3 align="center">atri-bot</h3>
  <p align="center">
    打破次元壁，让二次元角色来到现实
    <br />
    <a href="#快速开始"><strong>快速开始 »</strong></a>
    <br />
    <br />
    <a href="#使用效果">看看效果</a>
    ·
    <a href="https://t.me/AtriBotIsAChatBot">官方机器人</a>
    ·
    <a href="https://github.com/chhongzh/atri-bot/issues">报告问题</a>
    ·
    <a href="https://github.com/chhongzh/atri-bot/issues">提个需求</a>
  </p>
</div>

<!-- TABLE OF CONTENTS -->
<details>
  <summary>目录</summary>
  <ol>
    <li><a href="#这是啥">这是啥</a></li>
    <li><a href="#核心优势">核心优势</a></li>
    <li><a href="#快速开始">怎么跑起来</a></li>
    <li><a href="#使用效果">怎么用</a></li>
    <li><a href="#角色定义">一起搞角色</a></li>
    <li><a href="#后续计划">后面要干啥</a></li>
    <li><a href="#贡献指南">贡献指南</a></li>
    <li><a href="#许可证">许可证</a></li>
    <li><a href="#联系">联系</a></li>
  </ol>
</details>

<!-- ABOUT THE PROJECT -->
## 这是啥

大多数聊天机器人只注重角色扮演，陪聊还行，真让干活就歇菜。Atri Bot 不一样，受 OpenCLAW 这类 Agent 的启发，在能聊的基础上加上了各种工具调用能力。

`atri-bot` 想解决的是另一件事。

它不是问答机器。它是来打破次元壁的——让二次元角色来到现实，能调用工具，能真正做事。MCP 已经接入，其他工具还在开发中。你可以把它当成一个有记忆、有边界、有脾气的聊天搭档。

**多用户支持**。官方有公共实例 [@AtriBotIsAChatBot](https://t.me/AtriBotIsAChatBot)，信得过作者或者不会部署可以直接用。当然也鼓励自己部署，功能更完整——官方实例不允许访问内网，也不保证 100% 在线，但通常情况下是在线的。

核心就管三件事。

**角色**。从远端仓库或本地加载角色定义，每个角色有自己的性格和背景。角色切换已有。

**对话**。基于 CloudWeGo Eino ADK TurnLoop，支持配置不同 AI 模型和对话轮数。

**工具**。MCP 是当前重点，邮件、热点查询等其他工具仍在开发中。管理员能按用户管权限，该封的封，该放的放。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- CORE ADVANTAGES -->
## 核心优势

**下载一个可执行文件就能执行**。只需安装 0 个环境。

**性能高，内存占用低**。Go 语言编译型优势，跑起来轻快。

**完全可控**。自己部署就没有官方实例的限制，内网随便访问，在线时间自己说了算。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- GETTING STARTED -->
## 怎么跑起来

### 前置要求

* Go 1.27+
* Telegram Bot Token（找 [@BotFather](https://t.me/BotFather) 申请）
* AI 服务商的 API Key（OpenAI 兼容接口都行）

### 安装

1. 克隆仓库
   ```sh
   git clone https://github.com/chhongzh/atri-bot.git
   cd atri-bot
   ```

2. 编译
   ```sh
   go build -o atri-bot ./cmd/atri-bot
   ```

3. 创建工作目录和配置文件
   ```sh
   mkdir -p chardefs
   cat > config.yaml << EOF
   telegram:
     bot_token: 你的-bot-token
   bot:
     max_rounds: 36
   EOF
   ```

4. 跑起来
   ```sh
   ./atri-bot
   ```

默认会用 SQLite 数据库 `atri-bot.db`，并从 `https://github.com/mihari-bot/chardef` 的 `v2` 分支加载角色定义。本地角色放在 `<cwd>/chardefs/<character-id>.yaml`。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- USAGE EXAMPLES -->
## 怎么用

### 第一个用户自动变管理员

跟机器人说完第一句话，你就是管理员了。发 `/help` 看命令列表，这列表是按你的权限动态生成的，不是固定那一套。

### 配置 AI

AI 配置不提供共享默认值，每位用户必须分别设置自己的连接信息。

```text
/ai base-url https://api.openai.com/v1
/ai key <api-key>
/ai model <model-name>
/ai rounds <positive-integer>
```

### 管工具权限

工具权限按用户 ID 加工具名记录。被禁止的工具不会出现在该用户的 Agent 工具节点中，模型完全看不到，不是在调用时拦截。

管理员可以这样管。

```text
/toolperm list <user-id>
/toolperm allow <user-id> <tool-name>
/toolperm deny <user-id> <tool-name>
/toolperm reset <user-id> <tool-name>
```

`reset` 删除该用户的单独记录，恢复为默认权限。

### MCP 权限

MCP 用特殊权限名 `mcp` 作为一刀切总开关。禁止后，用户的远程 MCP 工具和自然语言 MCP 管理工具都会从 Agent 工具节点中消失。

```text
/toolperm deny <user-id> mcp
/mcp show <user-id>
/mcp limit <user-id> <positive-integer|0>
/mcp internal <user-id> <on|off|default>
```

`0` 或 `default` 恢复全局默认值。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- ROADMAP -->
## 后面要干啥

- [ ] RAG 实现记忆库接入
- [ ] 更多 Web 相关内置工具
- [ ] 插件系统
- [ ] Web 管理界面
- [ ] Docker 部署

去看看 [open issues](https://github.com/chhongzh/atri-bot/issues)，能看到所有正在讨论的功能和已知问题。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- CONTRIBUTING -->
## 一起搞角色

项目采用 [chardef-spec](https://github.com/mihari-bot/chardef-spec) 规范定义角色。你可以给自己喜欢的角色写提示词文件，或者优化现有的角色定义。

**怎么写角色定义**

在 `chardefs` 目录下创建 `<character-id>.yaml` 文件，按照 chardef-spec 规范填写角色设定、性格、背景等信息。

**贡献角色**

欢迎到 [mihari-bot/chardef](https://github.com/mihari-bot/chardef) 仓库提 Pull Request，把你写的角色定义加进去。不管是热门动漫角色还是冷门游戏人物，只要写得有意思，都能来投稿。

**代码贡献**

1. Fork 这个项目
2. 建个分支 (`git checkout -b feature/AmazingFeature`)
3. 提交改动 (`git commit -m 'Add some AmazingFeature'`)
4. 推上去 (`git push origin feature/AmazingFeature`)
5. 开 Pull Request

别忘了给个项目星星！

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- LICENSE -->
## 许可证

MIT 许可证。看 `LICENSE` 文件了解详情。

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- CONTACT -->
## 联系

官方机器人：[@AtriBotIsAChatBot](https://t.me/AtriBotIsAChatBot)

项目地址：[https://github.com/chhongzh/atri-bot](https://github.com/chhongzh/atri-bot)

<p align="right">(<a href="#readme-top">回到顶部</a>)</p>

<!-- MARKDOWN LINKS & IMAGES -->
[contributors-shield]: https://img.shields.io/github/contributors/chhongzh/atri-bot.svg?style=for-the-badge
[contributors-url]: https://github.com/chhongzh/atri-bot/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/chhongzh/atri-bot.svg?style=for-the-badge
[forks-url]: https://github.com/chhongzh/atri-bot/network/members
[stars-shield]: https://img.shields.io/github/stars/chhongzh/atri-bot.svg?style=for-the-badge
[stars-url]: https://github.com/chhongzh/atri-bot/stargazers
[issues-shield]: https://img.shields.io/github/issues/chhongzh/atri-bot.svg?style=for-the-badge
[issues-url]: https://github.com/chhongzh/atri-bot/issues
[license-shield]: https://img.shields.io/github/license/chhongzh/atri-bot.svg?style=for-the-badge
[license-url]: https://github.com/chhongzh/atri-bot/blob/master/LICENSE
