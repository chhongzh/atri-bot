# atri-bot

基于 Telebot v4 和 CloudWeGo Eino ADK TurnLoop 的 Telegram 角色聊天机器人。

## 运行

```bash
go build -o atri-bot ./cmd/atri-bot

./atri-bot
```

默认使用 SQLite 数据库 `atri-bot.db`，并从
`https://github.com/mihari-bot/chardef` 的 `v2` 分支加载远端角色。
本地角色放在 `<cwd>/chardefs/<character-id>.yaml`。

启动时从工作目录的 `config.yaml` 读取配置。`telegram.bot_token` 为必填项：

```yaml
telegram:
  bot_token: telegram-bot-token
bot:
  max_rounds: 36
```

可选配置：

- `atri_cwd`：runner 管理的数据与本地角色根目录。
- `character_repository_url`：默认角色 Git 仓库。
- `character_repository_branch`：默认角色仓库分支。
- `bot.max_rounds`：新用户的默认会话轮数；用户可单独修改。

第一个与机器人交互的用户会自动成为管理员。命令使用 shell 风格参数解析，
发送 `/help` 查看按权限动态生成的命令列表。

AI 配置不提供共享默认值，每位用户必须分别设置自己的连接信息：

```text
/ai base-url https://api.openai.com/v1
/ai key <api-key>
/ai model <model-name>
/ai rounds <positive-integer>
```

