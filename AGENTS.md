# AGENTS.md

本文件适用于整个仓库。更深层目录中的 `AGENTS.md` 可以补充或覆盖本文件的具体约定。

## 项目开发原则

### 1. 分文件、分包

- 按职责拆分文件和 Go package，避免单个文件或 package 持续膨胀。
- 一个文件只承载相对集中的职责；禁止把数千行实现、多个不相关功能或完整业务层堆在同一个文件中。
- 新增功能应放入职责匹配的现有 package；没有合适归属时再创建小而明确的新 package。
- 保持依赖方向清晰，避免通过全局状态、循环依赖或跨包复制业务逻辑解决问题。
- 修改前先查找现有抽象，优先复用而不是重复实现。

```
internal/account: 用户账户相关
internal/character: 角色提供商相关
internal/chat: 聊天核心代码, chat manager
internal/command: 命令实现与command manager
internal/config: 全局、用户配置加载读取实现
internal/constants: 全局常量
internal/errs: 所有internal包下的所有错误，方便复用
internal/files: LLM文件操作
internal/mcp: LLM mcp接入实现
internal/model: gorm数据库模型
internal/msgops: eino相关实用操作库
internal/runner: telebot与chat manager对接核心, 程序第一层入口, 主实例
internal/security: 安全相关, 包括Safe dialer的实现
internal/session: 上下文管理
internal/stealth: 浏览器过Bot检测相关
internal/tools: local tools实现
internal/utils: 实用函数集合
```

### 2. 错误集中管理

- 项目自定义错误统一放在 `internal/errs` 包中。
- 跨 package 传递的业务错误、可识别的错误码和用户可见错误应由 `internal/errs` 定义。
- 调用方使用 `errors.Is`、`errors.As` 或 `internal/errs` 提供的辅助函数判断错误，不通过脆弱的字符串比较判断错误类型。
- 在靠近错误发生处补充上下文并保留原始错误，例如使用 `%w`；不要静默吞掉关键错误。
- 不要在各业务 package 中散落重复的错误定义。仅限局部且不会跨 package 传播的实现细节错误，可以使用标准库方式处理，但仍应优先考虑 `internal/errs`。

### 3. 函数与公共工具

- 可复用的函数统一放在 `internal/utils` 下，并按主题拆分文件或子包。
- 只服务单一 package 的小 helper 留在该 package 内，避免把所有零散函数都塞进 `utils`。
- 函数应职责单一、输入输出清晰，避免通过隐式全局状态传递数据。
- 对外部输入在边界处完成必要的格式、范围和空值校验；内部已校验的数据不重复做无意义的防御性检查。

## 语言与错误消息

- 所有错误消息必须使用英语，包括返回给用户的消息、日志中的错误描述和自定义 error 文本。
- 中文仅用于文档、开发说明和必要的非错误界面文案；不要把中文混入错误字符串。
- 错误消息应准确、简洁并保留必要上下文，但不得包含 API key、token、密码或其他敏感信息。

## 测试与验证

- 本项目偏向模拟和行为演示，测试收益有限；除非用户明确要求，或代码涉及外部网络、数据库持久化、并发、序列化等高风险边界，否则不主动新增测试。
- 不为了追求覆盖率补充没有实际价值的测试，也不要为了让测试通过修改无关行为。
- 修改 Go 文件后运行 `gofmt`；交付前至少运行 `go build ./...` 和 `git diff --check`，若环境允许再运行受影响 package 的已有测试。
- 发现既有测试失败时记录失败范围和证据，不擅自修复无关问题。

## 修改范围与风格

- 保持改动聚焦，保留用户现有工作区改动，不做无关重命名、清理或重构。
- 遵循现有 Go 命名、构造函数、依赖注入和错误处理风格。
- 不新增无意义的全局变量；优先通过构造函数传递依赖。
- 提交前检查是否泄漏敏感信息，以及新增代码是否破坏现有包边界。
