# 多媒体能力接入技术路线

> 决策基线：2026-08-15。当前依赖为 `github.com/cloudwego/eino v0.9.13`、`github.com/cloudwego/eino-ext/components/model/openai v0.1.13`、`gopkg.in/telebot.v4 v4.0.0-beta.10`。

## 最终决策

项目只采用一条多媒体传输路线：把 Telegram 收到的图片、音频或视频直接流式上传到当前用户模型端点的 OpenAI-compatible Files API，取得 provider `file_id`，再在 Chat Completions 的 user message 中引用该 `file_id`。

这项能力必须由每个用户显式开启：

- 新用户与现有用户默认关闭。
- 用户执行 `/ai files on`，即主动声明自己的 `AIBaseURL` 同时支持 Files API 和 Chat Completions 的 file content part。
- 关闭时，图片、音频和视频 update 直接忽略；不下载 Telegram 文件、不调用 Files API、不创建聊天请求，也不提示用户。
- 不做服务端能力探测，不维护 provider 白名单。端点不兼容时返回本次请求错误，由用户修正端点或关闭能力。

模型按前提统一视为原生多模态，不做模型名判断或能力探测。本路线只处理“用户发送图片、音频、视频，模型返回文字”；通用文档、Sticker、联系人、位置，以及模型生成媒体后回传 Telegram 均不在范围内。

全链路禁止把媒体正文内联进 Eino message、session JSON 或聊天请求。Files API 的 multipart 上传是媒体正文唯一出现的位置。

## 协议合同

用户开启 Files 能力，代表其端点同时满足以下合同，而不只是存在一个同名 `/files` 路由。

### 1. 上传文件

使用用户自己的 `AIBaseURL` 与 `AIAPIKey`：

```http
POST {AIBaseURL}/files
Authorization: Bearer {AIAPIKey}
Content-Type: multipart/form-data; boundary=...

purpose=user_data
file=<原始二进制流>
```

`AIBaseURL` 沿用 ChatModel 当前语义。例如用户配置 `https://provider.example/v1`，上传地址就是 `https://provider.example/v1/files`，不能另行猜测 host、版本前缀或专用上传域名。

成功响应至少包含非空文件 ID：

```json
{
  "id": "file_abc123",
  "object": "file"
}
```

首版固定使用 OpenAI 的 `purpose=user_data`，不增加 provider 方言配置。如果某个端点要求不同 purpose，它不属于首版兼容合同。

如果上传响应包含 `status` 且仍处于 `uploaded`、`pending` 或 `processing`，客户端通过 `GET {AIBaseURL}/files/{file_id}` 有界轮询；只有进入可用状态后才创建聊天请求。响应没有 `status` 时按同步 Files API 处理。失败状态或等待超时都删除该文件并结束本次请求。

### 2. 在 Chat Completions 中引用文件

项目当前使用 `/chat/completions`，因此首版固定生成 Chat Completions file content part：

```json
{
  "role": "user",
  "content": [
    {
      "type": "text",
      "text": "<time>...</time><message>请描述这段视频</message>"
    },
    {
      "type": "file",
      "file": {
        "file_id": "file_abc123"
      }
    }
  ]
}
```

图片、语音、音频和视频统一使用这个 file part；媒体类型由上传文件的名称、MIME 和 provider 文件记录识别。首版不接入 Responses API 的 `input_file` / `input_image` 方言，也不为不同 provider 生成不同请求体。

Files API 上传成功不等于聊天接口必然接受 `file_id`。正式开放前必须用合同测试证明目标端点同时接受上述两段协议。

### 3. 删除文件

```http
DELETE {AIBaseURL}/files/{file_id}
Authorization: Bearer {AIAPIKey}
```

会话压缩、清空会话、关闭 Files 能力、删除账户和失败回滚都需要调用删除接口。删除失败进入有限重试；不能为了删除而长期保存旧 API Key。

## 当前项目缺口

- `internal/runner/init.go` 只把 `telebot.OnText` 接入聊天；所有媒体通过 `telebot.OnMedia` 进入空 handler 后被忽略。
- `internal/chat.Request` 只有 `Text`，TurnLoop 输入、抢占重排和 session 持久化都只构造纯文本 `schema.Message`。
- Telebot 收到的是 `FileID`、`UniqueID`、大小、MIME、文件名等元数据，不是模型端点能够读取的文件引用。`Bot.File` 内部先用 Telegram `file_id` 获取 `file_path`，再用 Bot Token 下载并返回 `io.ReadCloser`。
- Eino `schema.MessageInputImage`、`MessageInputAudio`、`MessageInputVideo` 和 `MessageInputFile` 只有 URL/内联数据字段，没有用户输入 `FileID` 字段。
- 当前 Eino OpenAI adapter 支持图片 URL、视频 URL和内联音频，但不支持 file part，也不能把 `schema.Message` 直接转换成上述 `file_id` 请求体。
- 当前 adapter 已提供 `openai.WithRequestPayloadModifier`，它能在 Eino 完成基础序列化后读取原始 messages 与 JSON payload，适合作为最小兼容接缝。
- 间接依赖 `github.com/meguminnnnnnnnn/go-openai v0.1.2` 虽然提供 Files API，但 `CreateFileBytes` 和 `CreateFile` 都先把完整 multipart body 写入 `bytes.Buffer`。图片尚可能勉强工作，音频和视频会造成与文件大小同阶的额外内存占用，不能直接复用。
- session manager 已直接 JSON 序列化 `schema.Message`，可以继续沿用；只需在 message `Extra` 中持久化本地文件引用，不需要迁移到 `schema.AgenticMessage`。

## 目标数据流

```text
Telegram media update
        │
        ├── AIFilesEnabled=false ──► 直接忽略
        │
        ▼
runner 提取媒体元数据 / 聚合相册
        │
        ▼
Telegram io.ReadCloser
        │  流式复制，仅保留小块缓冲
        ▼
media.Manager ── POST {用户 AIBaseURL}/files
        │
        ▼
可用的 provider file_id + 本地 staged 记录
        │
        ▼
chat.Request {caption, receivedAt, local file refs}
        │
        ▼
唯一 UserState / TurnLoop.Push / AnySafePoint
        │
        ├── schema.Message.Content：渲染后的文字
        └── schema.Message.Extra：本地 file ref
                    │
                    ▼
Eino OpenAI ChatModel 基础序列化
                    │
                    ▼
RequestPayloadModifier 把 ref 改写成 file_id content part
                    │
                    ▼
用户的 /chat/completions ──► assistant 文本 ──► Telegram
```

媒体二进制不会落入 `chat.Request`、TurnLoop、Eino message、session、数据库或日志。上传完成后只流转本地引用与 provider `file_id`。

## Eino 适配方案

### 保持 `schema.Message`

不迁移到 `schema.AgenticMessage`，也不复制整个 OpenAI ChatModel。当前 ChatModelAgent、工具调用、TurnLoop、session 与流式输出全部基于 `*schema.Message`，为一个请求字段迁移整条链路没有收益。

有媒体的 user message 仍然保持普通文字 `Content`，例如 caption 或“用户发送了一段语音”；本地引用写入保留 key：

```go
message.Extra["atri_file_refs"] = []string{"01J...", "01J..."}
```

这里保存的是项目生成的 opaque 本地记录 ID，不是媒体正文、Telegram `file_id` 或 provider `file_id`。无媒体消息不创建该 key，旧 session JSON 保持兼容。

### 永久模型包装器

在 `internal/chat/` 增加一个很薄的 `model.ToolCallingChatModel` 包装器：

- `Generate` 与 `Stream` 始终追加 `openai.WithRequestPayloadModifier`。
- `WithTools` 调用 inner model 后，再包装返回的新 model，确保 Eino 的工具绑定不会绕过 modifier。
- modifier 按当前 user ID、character ID 和 AI 配置 revision 一次批量解析所有 `atri_file_refs`。
- modifier 解析原始 payload 的 `messages`，严格校验数量、顺序与 role，再只改写带引用的 user message `content`；model、tools、tool choice、stream 和其他未知字段原样保留。
- 一个 user message 先放 text part，再按 Telegram 消息顺序放 file parts。
- 文件记录缺失、已删除、属于其他用户/角色或 revision 不一致时，不发送该 file part，只保留原来的文字说明。
- 普通聊天、工具循环和 session compression 都使用同一个包装后的 Agent，因此所有模型调用走相同转换，不存在只在首轮有效的旁路。

之所以不直接构造 `UserInputMultiContent`，是因为当前 adapter 会在 modifier 执行前尝试转换这些 part；file 类型会报 unsupported，音频也没有可用的 `file_id` 表达。普通 `Content + Extra` 可以先完成合法序列化，再由 modifier 做最后一步协议改写。

后续若 Eino 与其 OpenAI adapter 原生支持 Chat Completions file part，可以删除包装器并把本地引用解析到官方字段；Files 生命周期、session 数据与 Telegram 链路不需要重做。

## Files 客户端

在 `internal/media/` 使用标准库实现最小 OpenAI-compatible Files client，不调用当前 `go-openai` 的 `CreateFileBytes` 或 `CreateFile`：

- 用 `io.Pipe` 与 `multipart.Writer` 直接把 Telegram reader 复制到 HTTP request body。
- 保持常量级复制缓冲；不得先 `io.ReadAll`，不得把完整文件写入 `bytes.Buffer`。
- Telegram 已知 `FileSize` 时，可计算并设置精确 `Content-Length`；未知时使用流式 request。若目标端点拒绝标准流式 multipart，应视为不满足首版合同，不能回退到整文件内存缓冲。
- 上传字段固定为 `purpose=user_data` 与 `file`；文件名只保留经过清理的 basename，去除路径、控制字符和异常长度。
- 上传响应带异步状态时，用相同 client 有界轮询 `GET /files/{file_id}`；file ID 必须经过 path escaping，轮询响应中的字节数存在时应与实际上传量核对。
- 上传前检查 Telegram 声明大小，复制时再用硬上限检查实际字节数，不能只信 update 元数据。
- 读取前 512 字节做 MIME sniff，再通过 `io.MultiReader` 把这段小前缀放回上传流；声明 MIME、扩展名与 sniff 结果冲突时按安全规则规范化或拒绝。
- request 取消时同时关闭 Telegram body、pipe reader/writer 和 HTTP response body，避免一个方向失败后另一个 goroutine 永久阻塞。
- 非可重放流不做透明 POST 重试。上传失败由用户重新发送；返回错误体只读取有限长度并清除凭据与超长 provider 内容。
- 上传、删除与 ChatModel 共用当前安全 HTTP transport、私网访问策略和超时配置。

Telebot 云端 Bot API 的普通下载路径存在文件大小上限，首版单文件限制不得高于当前 Telegram 下载能力。若未来接入本地 Bot API server，应单独验证并调整限制，不能仅修改一个常量后宣称支持大文件。

## 用户配置

扩展 `internal/config.UserSettings`：

```go
type UserSettings struct {
    AIBaseURL       string `json:"ai_base_url"`
    AIAPIKey        string `json:"ai_api_key"`
    AIModel         string `json:"ai_model"`
    AIMaxRounds     int    `json:"ai_max_rounds"`
    AIFilesEnabled  bool   `json:"ai_files_enabled"`
    AIConfigRevision uint64 `json:"ai_config_revision"`
}
```

具体要求：

- JSON 缺失 bool 字段自然解析为 `false`，现有用户无需数据迁移。
- `/ai files on|off` 只接受明确的 `on` 或 `off`；`on` 表示用户确认协议合同，不主动调用探测请求。
- `/ai show` 展示 `Files API：已启用/未启用`，不展示 revision。
- `/ai` usage 更新为 `/ai [show|base-url|key|model|rounds|files] [value]`。
- 开关、Base URL 或 API Key 变化后必须 invalidate 对应 chat state。
- Base URL 或 API Key 的有效值发生变化时递增 `AIConfigRevision`；单纯修改 model 不递增，因为 Files 仍属于同一端点与账户。
- Files 记录保存创建时的 revision。旧 revision 的记录永远不能注入新端点的聊天请求。
- 关闭开关后，新媒体立即忽略，历史 file part 立即停止注入，并开始清理当前 revision 下的 provider 文件。
- 重新开启只影响之后的新上传，不能自动复活已经进入删除流程的旧引用。

设置更新必须通过 `account.Manager` 的事务性 API 完成，不能在命令 handler 中直接改 JSON。

## Provider 文件记录与生命周期

在 `internal/model/` 增加 provider 文件记录，由 `internal/media.Manager` 管理：

```text
ID                  项目生成的 opaque 本地引用
UserID              Telegram 用户 ID
CharacterID         上传时的角色 ID
AIConfigRevision    文件所属的用户 AI 配置版本
ProviderFileID      Files API 返回的 ID
Kind                image / audio / video
MIMEType            规范化后的 MIME
FileName            清理后的展示名
Bytes               实际上传字节数
Status              staged / committed / delete_pending / deleted
SessionRoundID      提交后关联的 round，可为空
CreatedAt/UpdatedAt
```

不保存媒体正文、Telegram 下载地址、Bot Token、用户 API Key 或旧 AI Base URL。provider `file_id` 不写入普通日志，日志使用本地 ID、用户 ID、角色 ID、kind、大小与阶段。

生命周期如下：

1. Telegram 流上传成功并返回 `file_id`。
2. 插入 `staged` 记录，创建 `chat.Request`；如果本地记录写入失败，立即删除刚上传的 provider 文件。
3. 模型调用期间，modifier 只解析仍为 `staged` 或 `committed`、且 revision 匹配的记录。
4. 一轮成功持久化时，在同一数据库事务中创建 `SessionRound`、把本轮引用更新为 `committed` 并写入 `SessionRoundID`。
5. 模型失败、请求被 debounce 丢弃、相册任一文件失败或本轮最终未持久化时，把本轮所有 staged 文件转为 `delete_pending` 并调用 provider 删除。
6. 进程崩溃可能留下 staged 记录；启动和定时 sweeper 删除超过短 TTL 且未绑定 round 的记录。
7. session compression 的 summary 成功提交后，cutoff 及以前 round 的文件转为 `delete_pending`。必须先提交 summary，再删 provider 文件。
8. 清空会话按 user ID + character ID 清理；删除账户清理该用户全部文件；关闭 Files 能力清理当前 revision 文件。

`session.Manager.AppendRound` 需要在自己的 GORM 事务中调用一个受限的 attachment hook，由 media manager 校验并提交引用。这样 session 仍负责 round 规则，media 仍负责文件状态，且不会出现 round 已写入而文件仍长期处于 staged 的正常路径。

删除失败只在仍能取得同一 revision 凭据时重试。用户修改 Base URL 或 API Key 前，先用旧设置 best-effort 清理旧 revision；即使清理失败也允许用户更新配置，但将本地记录标记为不可再引用。项目不会保存旧密钥来追删 provider 侧孤儿，这是明确的边界。

上传过程中若用户配置发生变化，上传任务仍只使用开始时捕获的 Base URL、API Key 与 revision。取得 `file_id` 后再次比较当前 revision；不一致就用捕获的旧 client 删除该文件，不能把它放进新 chat state。

## Telegram 入站映射

显式注册目标 handler，不能只依赖兜底 `OnMedia`：

| Telebot update | kind | 上传文件名策略 | 备注 |
| --- | --- | --- | --- |
| `OnPhoto` | image | `photo.jpg` 或 sniff 后扩展名 | Telebot 已选择最高分辨率 |
| image `OnDocument` | image | 清理后的原文件名 | 只接受实际 MIME 为 image |
| `OnVoice` | audio | `voice.ogg` | 原样上传 OGG/Opus，不在机器人内转码 |
| `OnAudio` | audio | 清理后的原文件名 | 保留 MIME、标题只作元数据 |
| audio `OnDocument` | audio | 清理后的原文件名 | 只接受实际 MIME 为 audio |
| `OnVideo` | video | 清理后的原文件名或 `video.mp4` | 原样上传 |
| `OnAnimation` | video | 清理后的原文件名或 `animation.mp4` | 按实际 MIME 处理 |
| `OnVideoNote` | video | `video-note.mp4` | 无 caption 时生成确定性说明 |
| video `OnDocument` | video | 清理后的原文件名 | 只接受实际 MIME 为 video |

其他 Document、Sticker 和非媒体 update 继续忽略。用户已声明模型原生支持这些媒体，因此机器人不做格式转码、抽帧、语音转写或模型能力判断；provider 拒绝具体格式时，把 provider 错误归类为媒体不兼容。

runner 在调用 `Bot.File` 前必须先读取用户设置并检查 `AIFilesEnabled`。关闭时不得触发 Telegram `getFile`，这是“未启用不处理”的验收点。

caption 与媒体组成同一个 user message。没有 caption 时生成简短且稳定的文字，例如“用户发送了一张图片”或“用户发送了两段视频”，使 session compression 和 file 引用失效后的历史仍然可读。

### 相册

Telegram 相册的每个成员是独立 update。按 `{chatID, userID, AlbumID}` 使用短静默窗口聚合，按 Telegram message ID 排序后形成一个 `chat.Request`：

- 相册只触发一次用户级 debounce 和一次 TurnLoop push。
- 在收齐元数据后再开始上传，避免较早成员被现有 debounce 淘汰后留下 provider 文件。
- 文件上传可以有限并发，但最终 refs 必须恢复 Telegram 顺序。
- 任一成员失败则整组回滚，删除已经上传成功的成员，不把残缺相册交给模型。
- 聚合器停止、用户状态失效或进程关停时必须结束等待并回滚 staged 文件。

## 包职责

### `internal/runner/`

- 继续作为唯一 Telegram API 边界，注册媒体 handler、提取 Telebot 元数据并打开 `Bot.File` reader。
- 在下载前完成用户开关快路径；不在 runner 中实现 Files HTTP、状态机或 session 规则。
- 管理 Telegram 相册聚合与 handler 生命周期，把 reader factory 和元数据交给 media manager。
- caption 作为聊天文字，不把媒体 caption 中的 `/...` 当作命令。
- 根据 image/audio/video 使用对应 chat action；现有文字回复发送逻辑不变。

### `internal/media/`

- 新增 manager、流式 Files client、MIME/大小校验、provider 文件状态机、删除队列与 sweeper。
- 所有读取和状态变化同时校验 user ID、character ID 与 AI config revision。
- 接收 runner 注入的 Telegram reader，不直接持有 Bot Token。
- 向 chat 暴露创建 staged refs、批量解析 refs、提交 round、回滚、按 cutoff/会话/用户清理等 API。

### `internal/chat/`

- `Request` 扩展为 `Caption`、`ReceivedAt` 与 `[]media.Ref`；`ReceivedAt` 在 update 到达时固定，重排或重试不能重新取 `time.Now()`。
- 一个 request 只构造一次规范 user message，首次推理、抢占 requeue 和最终 session 持久化复用它。
- 增加 Eino model 包装器与 payload modifier；继续使用唯一 UserState、唯一 TurnLoop 和 `adk.AnySafePoint`。
- 媒体上传完成前不进入 TurnLoop；进入后与普通文字消息使用相同抢占规则。
- state 创建时捕获开关与 revision；相关设置变化后通过现有 invalidation 重建 state。

### `internal/session/`

- 继续直接 JSON 序列化 `schema.Message`，只新增对 media attachment transaction hook 的调用。
- compression 使用同一个包装后的 Agent，所以活动 file refs 能转换为 provider `file_id`。
- summary 提交后通知 media manager 清理 cutoff 文件；删除失败不能回滚已经成功的 summary。
- 历史中失效的 ref 由 modifier 忽略，原有文字说明仍参与上下文，不能因一个旧文件让整段会话无法加载。

### `internal/account/` 与 `internal/config/`

- 保存用户级开关与 revision，提供原子更新 API。
- Base URL、Key、Files 开关变化触发 chat state invalidation；用户删除前触发文件清理。
- 不增加全局 AI endpoint、共享 API Key 或 provider 能力配置。

### `cmd/atri-bot/`

- 只通过 Fx/runner 组装 media manager 与依赖，不放业务逻辑。
- 首版可在 runner/media config 中提供运行级安全上限与清理周期，但这些只能控制资源，不能替代用户级 `AIFilesEnabled`。

## 并发与顺序

- 同一用户的媒体上传需要受限，防止连续视频占满连接和内存；不同用户之间不能共用上传状态。
- 相册聚合、上传与回滚都使用 user/album 隔离键。
- 上传采用从 Telegram 到 provider 的背压复制；provider 读取慢时自然限制 Telegram 读取速度，不建立无界 channel。
- 新文字在旧模型轮次中到达仍由 TurnLoop 抢占；尚在上传的媒体没有可用 `file_id`，不能提前推入模型。
- 为避免慢媒体上传完成后打乱同一用户 update 顺序，runner 需要给媒体入站分配序号，并在提交 TurnLoop 前经过用户级有界 sequencer。相册占一个序号；任务取消或失败也必须释放该序号。
- 关停顺序为：停止接收新 update，关闭相册与入站 sequencer，取消并等待上传，回滚 staged 文件，最后停止所有 TurnLoop 与 Telebot。

## 错误语义

- Files 未启用或媒体类型不在范围：静默忽略。
- Telegram 下载失败：提示“无法从 Telegram 读取媒体”，不创建 provider 记录。
- 文件超限或 MIME 不支持：给用户可理解的固定错误，不调用模型。
- Files 上传失败、响应缺少 `id`、处理失败或等待可用超时：提示“模型端点文件上传失败”，不进入 TurnLoop。
- Chat Completions 拒绝 file part：提示“模型端点不接受已上传文件”；尝试删除本轮文件，不自动关闭用户设置。
- 相册部分失败：整组失败并回滚，不发送部分内容。
- 抢占发生在文件已经上传但 session 尚未提交时：保留 staged refs 随 request requeue；只有 request 最终丢弃时才删除。
- provider 删除失败：记录本地 ID、阶段和状态码，不记录 API Key、完整响应或 provider `file_id`。

## 实施阶段

### P0：先证明协议和 Eino 接缝

- [ ] 用 `httptest.Server` 同时模拟 `/files`、`/chat/completions` 与 `DELETE /files/{id}`。
- [ ] 证明流式 multipart 的字段、文件名、Authorization、取消和大小上限正确，且上传内存不随文件大小线性增长。
- [ ] 证明纯文本 `schema.Message + Extra` 能先通过当前 adapter，再由 `WithRequestPayloadModifier` 生成精确的 file content part。
- [ ] 证明 modifier 在 `Stream`、`Generate`、`WithTools` 后的 model 以及 session compression 中都生效。
- [ ] 捕获最终聊天 JSON，断言不存在媒体正文、data URL或其他内联二进制字段。
- [ ] 用计划支持的真实 provider 做最小 smoke test；只有上传、引用、删除三项都通过才标记兼容。

### P1：用户配置与 provider 文件 manager

- [ ] 增加 `AIFilesEnabled`、`AIConfigRevision`、account manager API、`/ai files` 与 `/ai show`。
- [ ] 增加 provider 文件表、流式 client、staged/commit/delete 状态机和用户/角色/revision 隔离测试。
- [ ] 增加 chat state invalidation、旧 revision 降级与设置变化中的上传竞态测试。

### P2：Eino 与 session

- [ ] 扩展 `chat.Request`，统一规范 user message 的构造、抢占 requeue 与持久化。
- [ ] 接入 model 包装器和 payload modifier，保证工具调用仍正常。
- [ ] 给 `session.AppendRound` 增加 attachment transaction hook。
- [ ] 接入 compression cutoff、清空会话、用户删除和关闭开关的文件清理。

### P3：Telegram 图片、音频和视频

- [ ] 注册 Photo、目标 Document、Voice、Audio、Video、Animation 与 VideoNote handler。
- [ ] 实现开关快路径、MIME sniff、大小限制、caption 与无 caption 说明。
- [ ] 实现相册聚合、整组回滚、用户级入站 sequencer 和有界上传并发。
- [ ] 增加 Telegram reader、相册、取消、关停及“关闭时零下载/零上传”测试。

## 验收标准

- 用户未开启 Files 时，发送任何图片、音频或视频都不会调用 Telegram `getFile`、Files API 或 ChatModel。
- 用户开启后，媒体正文只通过一次 multipart 流从 Telegram 进入其模型 provider，机器人内存中没有完整文件副本。
- `/chat/completions` 只看到 provider `file_id`，Eino message 和 session JSON 中没有媒体正文。
- 图片、语音、音频、视频、动画、视频圆形消息和目标 MIME Document 都能形成正确 file part；caption 与相册顺序正确。
- 普通文字聊天、工具调用、TurnLoop 抢占、角色隔离、历史加载和流式文字回复行为不回归。
- 文件记录严格按用户 ID + 角色 ID + AI config revision 隔离，不能跨用户或跨端点引用。
- 成功 round 可在进程重启后继续引用仍有效的 provider 文件；压缩、清空、关闭、删除和失败路径会清理文件。
- Base URL 或 API Key 变化后，旧 `file_id` 不会被发送到新端点。
- 日志、错误消息、数据库与 session 中不出现 Bot Token、API Key 或媒体正文。

文档完成后实施代码时，按仓库约定对外部网络、数据库与并发路径补测试，并依次运行受影响包测试、`gofmt`、`go build ./...`、`go vet ./...` 与 `git diff --check`。
