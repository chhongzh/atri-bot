# 命令与工具

所有命令都通过文本消息发送，参数支持引号，带空格的参数用引号包起来。发送 `/help` 可以按权限查看当前可用的命令列表。

## AI 配置

每位用户都有自己的 AI 连接配置，项目没有共享的 AI 默认值。第一次使用前先设置 base URL、API key 和模型。

```text
/ai show
/ai base-url https://api.openai.com/v1
/ai key <你的 api key>
/ai model <模型名>
/ai rounds 12
/ai image-size 1024
```

- `/ai show` 查看当前配置。
- `/ai base-url <url>` 设置接口地址。
- `/ai key <key>` 设置 API key。
- `/ai model <模型名>` 设置模型。
- `/ai rounds <正整数>` 设置自己的会话轮数上限。
- `/ai image-size <1-2048>` 设置自己收到图片的最长边。

## 角色与 chardef

### 列出和切换角色

```text
/characters
/character <角色ID>
```

- `/characters` 列出所有可用角色。
- `/character` 不带参数时查看当前角色。
- `/character <角色ID>` 切换到指定角色。

会话历史按角色隔离。切换角色后，之前的对话上下文不会被带过来。

### 角色文件

角色文件用 YAML 编写，遵循 [chardef 规范](https://github.com/mihari-bot/chardef-spec)。本地角色放在 `<atri_cwd>/chardefs/<角色ID>.yaml`，文件名去掉扩展名就是角色 ID。

项目默认从 [mihari-bot/chardef](https://github.com/mihari-bot/chardef) 的 `main` 分支加载远程角色，这个仓库欢迎所有人提交。想优化某个角色的提示词，或者为自己喜欢的角色写一份人设文件，直接去那里开 PR。提示词决定角色怎么说话、怎么用工具，是角色体验的核心。

管理员可以管理角色来源。

```text
/providers
/provider add <id> <url> [branch]
/provider set <id> <url> [branch]
/provider remove <id>
/provider refresh <id>
```

## 工具与 MCP

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

管理员可以单独覆盖某个用户的 provider 上限。网络访问范围由全局 `security.allow_private_ip` 统一控制。

```text
/mcp show <用户ID>
/mcp limit <用户ID> <正整数|0>
```

## 管理员

第一个成功创建的用户自动成为管理员，后续用户默认为普通用户。系统保证至少保留一个未封禁的管理员。

```text
/admin stats
/admin promote <用户ID>
/admin demote <用户ID>
/admin ban <用户ID>
/admin unban <用户ID>
/admin delete <用户ID>
/users [all|banned] [page]
/user <用户ID>
/admins [page]
/active-users [page]
```

- `/admin stats` 查看运行统计。
- `/admin promote|demote|ban|unban|delete <用户ID>` 管理账户权限。
- `/users`、`/user` 查看账户列表和详情。
- `/admins` 查看管理员列表。
- `/active-users` 查看当前活跃的聊天状态。
