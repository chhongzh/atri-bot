# AGENTS.md

本文件适用于整个仓库。修改更深层目录中的代码时，如果该目录存在更具体的 `AGENTS.md`，以更深层文件为准。

## 项目定位

本项目是基于 `telebot.v4`、Eino ADK TurnLoop、GORM 与 Fx 的 Telegram 单 Agent 角色聊天机器人。

核心设计原则：

- `runner` 是唯一的顶层装配与 Telegram API 边界，负责组合组件、管理运行目录和生命周期。
- 业务状态与业务规则必须放在对应 manager 中，不要堆到 `runner` 或命令处理函数里。
- 每个 Telegram 用户拥有独立的账户、AI 连接配置、工具配置和聊天状态。
- 每个用户同时只能拥有一个内存中的 TurnLoop；TurnLoop 是聊天执行与抢占的唯一入口。
- 会话按“用户 ID + 角色 ID”隔离，切换角色不能混用历史。

## 目录职责

- `cmd/atri-bot/`：程序入口、Fx 依赖注入、Viper 配置读取、数据库初始化和生产工具注册。不在这里实现业务逻辑。
- `internal/runner/`：组件装配根、Telebot handler/middleware 注册、命令 provider 装配、CWD 规范化和进程生命周期。
- `internal/chat/`：用户级 `UserState`、模型与 Agent 实例化、TurnLoop 执行、抢占、过期回收及消息流处理。
- `internal/account/`：用户资料、角色、封禁状态、所选角色、用户级 AI 配置和管理员不变量。
- `internal/session/`：Eino `schema.Message` 的持久化、读取、裁剪和按角色隔离。
- `internal/character/`：角色 provider、角色定义加载、远程仓库同步及系统提示词渲染。
- `internal/command/`：命令解析、provider/command 注册、鉴权和动态帮助生成。
- `internal/tools/`：工具注册、用户级结构化配置、运行时状态注入及内置配置工具。
- `internal/tools/<tool>/`：具体工具实现；一个功能完整的工具使用独立子包。
- `internal/tools/builtin/config/`：自然语言工具配置管理（list/get/configure）。
- `internal/tools/builtin/mcp/`：MCP 权限门与自然语言 MCP provider 管理工具。
- `internal/mcp/`：MCP provider 记录、内网地址检查、异步加载 worker 池与远程调用审计日志。
- `internal/msgops/`：Eino 消息构造、提取、类型判断与规范化。
- `internal/utils/`：真正跨包复用的轻量公共函数，例如日志字段和 Telegram 辅助函数。
- `chardefs/`：内置 local provider 的角色 YAML，文件名去掉扩展名后即角色 ID。
- `data/`：运行期数据，例如远程角色仓库缓存；不要把运行期产物提交到源码目录。

## 分包与代码风格

- 按职责拆包、拆文件，避免单文件持续膨胀；命令、provider、handler、model 和 middleware 应分别组织。
- 遵循现有 Go 风格、命名、构造函数和错误处理方式；提交前对修改的 Go 文件运行 `gofmt`。
- 优先修复根因，保持改动聚焦，不顺手重构无关代码。
- 不重复造轮子。已有第三方库或仓库内抽象可满足需求时直接复用。
- 只有被多个包复用的通用逻辑才放入 `internal/utils/`；仅服务单包的 helper 留在该包内。
- 不新增无意义的全局状态。manager 通过构造函数接收依赖，顶层依赖由 Fx 和 `runner` 组合。
- 对外部输入做 `TrimSpace`、格式和边界校验；错误应保留上下文，不能静默吞掉关键失败。
- 不记录 API Key、Token、完整工具密钥等敏感值；展示密钥时必须掩码。
- 保留用户现有工作区改动，不做与任务无关的清理、回滚、重命名或格式化。

## Runner 与 Telebot 约定

- `runner.Runner` 持有 `gorm.DB`、Telebot Bot 及各 manager，是组件的唯一集成层。
- `Runner.Init` 负责按依赖顺序初始化 manager、迁移表、注册工具和命令、创建 Bot、安装 middleware 与 handler。
- `runner` 只协调流程；账户、会话、角色、工具等规则应由对应 manager 提供 API。
- 所有文本消息统一注册到 `telebot.OnText`。不要使用 Telebot 自带的命令注册或参数解析。
- 仅当文本以 `/` 开头且不以 `\/` 开头时才视作命令；命令必须通过 `github.com/chhongzh/shlex` 解析。
- 命令未被消费时才进入聊天流程。不要为每个命令创建 Telebot endpoint。
- 图片、文件、语音、音频、视频和动画需要注册 handler；未支持的媒体当前直接忽略。
- 普通用户检查、封禁检查、管理员权限与日志等横切逻辑优先通过 middleware/authorizer 实现。
- 修改 CWD 相关逻辑时，所有相对运行目录都必须基于规范化后的绝对 `Config.CWD`。
- 关停时先停止所有聊天状态和 TurnLoop，再停止 Telebot。

## Chat 与 TurnLoop 约定

- 每个用户在内存中最多一个 `UserState`，其中包含用户 ID、角色 ID、Eino Agent、Runner、TurnLoop 和当前 Telebot context。
- 每个用户只有一个 TurnLoop；不要在 handler 或每轮消息中创建额外聊天循环。
- 新消息通过 `TurnLoop.Push` 进入，并使用 `adk.AnySafePoint` 抢占旧轮次，使新消息能在任意安全点打断当前生成。
- 状态按 `StateTTL` 在空闲后动态销毁；状态失效与进程关闭必须正确停止并等待 TurnLoop。
- Agent 和 OpenAI model 必须从该用户数据库记录中的 `AIBaseURL`、`AIAPIKey`、`AIModel` 实例化。
- AI Base URL、API Key、Model 和 Max Rounds 都是用户级配置。禁止添加共享 AI 默认值、环境变量回退、全局模型或跨用户复用凭据。
- Base URL、API Key 或 Model 任一缺失时，不得创建模型；应提示用户先完成自己的 AI 配置。
- 用户修改 AI 配置、角色或影响 Agent/工具行为的状态后，必须使对应聊天状态失效；角色 provider 变化后使全部聊天状态失效。
- 历史消息由本项目显式拼接，不依赖 Agent 暗含的共享历史。
- 每轮开始时从 character manager 重新生成 system prompt，不把旧 system prompt 当作持久状态复用。
- 在状态创建/销毁、轮次准备/完成/抢占、模型错误和消息发送错误处添加有意义的 logger 埋点；不得记录敏感配置。

## Session 约定

- 直接 JSON 序列化 Eino `schema.Message`，保留 user、assistant 和 tool 等消息结构。
- 会话查询和写入必须同时使用 Telegram 用户 ID 与 Character ID 作为隔离键。
- 动态生成的首条 system message 不持久化；加载历史后在本轮前重新注入。
- 轮数以 user message 计数，并保留该轮相关的 assistant/tool 消息。
- 默认最大轮数为 `36`；可由运行配置决定新用户初始值，之后由每个用户自己的 `AIMaxRounds` 控制。
- 数据库访问使用 `WithContext`；涉及多步写入、裁剪或管理员不变量时使用事务。
- 不在 manager 外直接拼 GORM 查询以绕过 session/account/tool 的业务规则。

## Tool 约定

- 工具实现放在 `internal/tools/<tool>/`，并暴露形如 `Register(*tools.Manager) error` 的 registrar。
- 生产工具在 `cmd/atri-bot/` 中通过 `[]tools.Registrar` 显式注册，manager 通过 `RegisterAll` 统一加载。
- 可配置工具优先使用泛型 `tools.Register[C, I, O]`，由 manager 推导配置类型并包装 Eino tool。
- 配置类型必须是结构体；配置以用户 ID + 工具名为键，在 GORM 中保存为经过校验和规范化的 JSON。
- 工具本身必须无状态。用户 ID、角色 ID、Telebot context 等调用期信息只通过 `tools.RunningState` 传递。
- 工具调用前由 manager 自动装载当前用户配置；工具实现不要自行查询或缓存其他用户的配置。
- 自然语言配置工具由 `internal/tools/builtin/config/` 提供，允许模型列出、读取和修改当前用户自己的工具配置。
- MCP 相关工具由 `internal/tools/builtin/mcp/` 提供，配合 `internal/mcp/` 的 manager 使用；工具调用、provider 变更会触发对应聊天状态失效。
- `RegisterBuiltin` 只用于不需要 manager 自动管理配置的工具，不要用它绕过可配置工具注册流程。
- 新增配置字段时保证 JSON 标签稳定，拒绝未知字段，并补充默认配置、类型错误和用户隔离测试。
- 工具配置更新需要记录用户 ID 和工具名，但不要记录配置正文或密钥。

## Command 约定

- 命令 handler 类型统一为 `func(c telebot.Context, args []string)`；handler 负责响应，manager 负责解析、查找和鉴权。
- 注册命令时必须提供 provider、描述、命令名和 usage，用这些元数据动态生成 `/help`。
- 命令按 provider 分组；普通、角色、AI、provider 管理和账户管理应保持清晰边界。
- provider 与其命令顺序属于用户界面的一部分。继续使用 `github.com/elliotchance/orderedmap`，不要用普通 map 生成帮助列表。
- 管理员 provider 设置 `AdminOnly`，权限通过 command manager 的 authorizer 判断，不在每个 handler 中重复实现。
- 参数始终使用 shlex 结果，支持引号与空格；不要对 `c.Text()` 再做脆弱的字符串切割。
- 修改 AI、角色、会话、provider 或账户状态的命令必须同步执行相应的 chat state invalidation。
- 命令错误返回对用户可理解的信息，同时记录包含操作上下文的结构化日志。

## 账户与管理员不变量

- 第一个成功创建的用户自动成为管理员，后续用户默认为普通用户。
- 被封禁用户不能继续进入命令或聊天流程。
- 管理员不能给自己降级。
- 系统必须始终保留至少一个未封禁管理员；禁止降级、封禁或删除最后一个有效管理员。
- 只有管理员能新增、修改、删除或刷新角色 provider，以及执行账户管理操作。
- 提权、降级、封禁、解封和删除账户后，需要通知所有当前有效管理员。
- 账户删除、封禁或权限变化后，使目标用户的 chat state 失效。
- 新增管理员能力时优先扩展 `account.Manager` 的事务性 API，不要仅靠命令层检查不变量。

## Character 与系统提示词约定

- local provider 永远存在，根目录固定为 `<CWD>/chardefs`，且不能作为普通远程 provider 删除。
- remote provider 使用 `go-git` 克隆和更新，运行缓存位于 `<CWD>/data/character-providers/`。
- 默认远程仓库为 `https://github.com/mihari-bot/chardef`，默认分支为 `v2`；上游可通过 runner 配置覆盖。
- 角色文件为 `<character-id>.yaml` 或 `.yml`，角色 ID 使用类似 Android 包名的稳定 ID，例如 `dev.chhongzh.atri`。
- 角色 YAML 遵循项目约定的 chardef schema；加载后保持为 `map[string]any`，避免为未知扩展字段制造僵硬结构。
- 所有 provider 的角色统一由 character manager 查询；重复角色 ID 按确定的 provider 加载顺序处理并记录警告。
- 系统提示词只有一个模板：`internal/character/system.j2`，通过 `go:embed` 嵌入二进制。
- 模板使用 Eino Jinja2 渲染，至少提供 `Time`、`Username`、`CharacterID`、`Character` 和 `CharacterYAML`。
- system prompt 每轮动态生成；修改模板或注入字段时同时更新对应渲染测试。
- provider 重载、单 provider 加载失败和重复角色必须有日志；单个 provider 失败不应无提示地污染其他 provider 结果。

## 配置约定

运行配置文件为工作目录下的 `config.yaml`。必填项只有 Telegram Bot Token；AI 连接信息不属于全局配置，必须由每位用户自行设置。

```yaml
telegram:
  bot_token: "..."

bot:
  max_rounds: 36

# 可选
atri_cwd: "."
character_repository_url: "https://github.com/mihari-bot/chardef"
character_repository_branch: "v2"

# MCP 外部工具（可选）
mcp:
  max_tools: 32        # 每用户 MCP provider 上限，管理员可用 /mcp limit 单独覆盖
  block_internal: true # 默认拦截 localhost/.internal/私网地址，管理员可用 /mcp internal 单独覆盖
```

- `bot.max_rounds` 只决定新用户的初始轮数，不是共享模型配置。
- `mcp.max_tools`、`mcp.block_internal` 为全局默认值；每用户上限与内网拦截开关可由管理员通过 `/mcp` 命令单独调整，`/toolperm deny <user-id> mcp` 可一键禁用该用户的全部 MCP 能力。
- 不得新增全局 `ai_base_url`、`ai_key` 或 `ai_model` 默认项。
- 不提交真实 Token、API Key、SMTP 凭据、数据库文件或远程仓库缓存。

## 测试与交付

- 行为改动应在相邻包补充或更新测试；仓库已有测试时不要只依赖手工验证。
- 优先先运行受影响包测试，再按风险扩展到全仓库。
- 推荐验证顺序：

```bash
gofmt -w <changed-go-paths>
go test ./...
go test -race ./...
go vet ./...
go mod verify
git diff --check
```

- 纯文档改动至少运行 `git diff --check`。
- 不为了让测试通过而修改无关行为；遇到既有失败时记录其范围和证据。
- 交付前检查数据库迁移兼容性、用户数据隔离、管理员不变量、状态失效路径和日志中是否泄漏敏感值。
