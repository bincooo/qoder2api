# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本仓库中工作时提供指导。

## 项目概览

Qoder2API 是一个 Go 编写的 HTTP 服务器，它对 [Qoder](https://qoder.sh) 编码智能体后端进行逆向工程，
并将其重新暴露为一个 **OpenAI 兼容**的 `/v1/chat/completions` 端点（同时支持流式 SSE 和
非流式 JSON）。CLI 工具 / 本地 LLM 客户端可以把请求指向这个桥接服务，而不必直接调用 OpenAI API。

核心工作在于协议仿真，而非 Web 服务：
- 该桥接将 OpenAI 聊天请求翻译成 Qoder 私有的 `agent_chat_generation` 请求结构（由内嵌的
  `baseprompt.json` 构建），再把 Qoder 的 SSE 响应解码回 OpenAI 的 chunk/completion。
- 每次上游调用都必须携带经过签名、加密的 "COSY" Bearer Token，以及机器标识/签名。这些机制
  位于 `internal/cosy`。

本模块是对早期 Java 实现的干净 Go 移植；仓库现在是纯 Go（已无任何 Java 残留）。

## 构建与运行命令

### 标准命令
```bash
go build ./...              # 构建 cmd/qoter2api 二进制
go test ./...               # 运行所有 internal 包的单元测试
go test -v ./...            # 显示详细的测试输出
go test -run TestName       # 运行特定测试
go run ./cmd/qoder2api      # 启动服务器（需要 QODER_PAT 环境变量）
```

### 环境变量配置
| 配置项       | 环境变量      | 默认值        | 用途                                       |
|--------------|---------------|---------------|--------------------------------------------|
| 个人令牌     | `QODER_PAT`   | *(必填)*      | 用于换取 job token 的 Qoder 账户令牌       |
| 主机         | `QODER_HOST`  | `127.0.0.1`   | HTTP 服务器的绑定地址                      |
| 端口         | `QODER_PORT`  | `8963`        | 监听端口                                   |

若 `QODER_PAT` 未设置，服务器启动时以 "Token required!" 退出。服务绑定到
`http://<host>:<port>/v1/chat/completions`。`baseprompt.json` 在构建时通过 `//go:embed`
从 `internal/bridge/` 嵌入——运行时不依赖文件系统。

## 架构总览

```
cmd/qoder2api main
  → auth.ExchangeJobToken(pat)        # POST center.qoder.sh .../jobToken（RFC-1123 日期 + MD5 签名）
  → cosy.NewSession(identity, mid...) # RSA 加密会话密钥 → cosy-key；AES 加密 identity → info
  → http.ServeMux /v1/chat/completions → bridge.Handler
      → 加载内嵌的 baseprompt 模板，填充 {UUID*}/{TIME1} 占位符
      → 将 OpenAI 消息映射为 Qoder 消息对象
      → 通过 SessionContext.SignedPostStream（SSE）POST api3.qoder.sh .../agent_chat_generation
        （epoch 秒的 cosy-date + MD5 Bearer 签名）
      → 将解码出的增量重新封装为 OpenAI SSE chunk（或聚合为一个 JSON completion）
```

### 包结构详情

#### **`internal/cosy`** — COSY 登录/请求层（字节级关键；每个文件都曾对照（现已移除的）Java
  原始实现逐字节校对过）：
- `signature.go` — `CurrentDate()`（RFC-1123 GMT，字面量 `GMT` 后缀）、`Sign(date)` = 对
  `appcode&secret&date` 求 MD5，以及未导出的 `md5Hex`。
- `qodencoding.go` — Qoder 自定义 base64 变体（`Encode`/`Decode`）：先做标准 Base64，再做
  确定性的 `a = n/3` 三段重排，外加自定义 64 字符字母表映射和 `$` 填充。这里出 bug 会
  破坏所有上游调用。
- `bearerbuilder.go` / `session.go` — `AuthIdentity`/`SessionContext`、`ServerPubkeyPEM`
  （硬编码的 RSA 服务器公钥）、`NewSession`（RSA 加密 16 位十六进制临时密钥 → `cosyKey`，
  用同一临时密钥作为 key+IV 做 AES-CBC 加密 identity → `info`）、`BuildPayloadB64`
  （按 TreeMap 排序的键）、`SignRequest`（对 `payloadB64\ncosyKey\ncosyDate\nbody\npathSig`
  求 MD5；注意此处 `cosy-date` 的值是 epoch 秒，与 `signature.go` 的 RFC-1123 不同）、
  `ComposeBearer` → `authorization: Bearer COSY.<payloadB64>.<sig>`。
- `bearerclient.go` — `SignedPostStream`：原样附加 17 个 `cosy-*` 请求头，对请求签名，并以
  回调方式逐行流式读取 SSE（4 MiB scanner）。`PathSig` 会剥离前导 `/algo`。
- `uuid.go` — `NewUUID()`，RFC-4122 v4。

#### **`internal/auth`** — 仅有一个 `jobtoken.go`：`BuildExchangeBody` + `ExchangeJobToken`，
  对接 `center.qoder.sh`（此处使用 RFC-1123 日期方案），把 Java 中两个近乎重复的 job-token
  客户端合并为一个。

#### **`internal/bridge`** — OpenAI↔Qoder 映射：
- `template.go` — 内嵌 `baseprompt.json`，`FillTemplate`/`LoadEmbeddedTemplate`。
- `tools.go` — `ParseToolCallsText`、`NormalizeToolCalls`、`NormalizeToolArguments`、
  `SummarizeUnresolvedToolCalls`、`IsPotentialToolCallText`（识别 `"Tool calls:"` 文本前缀）。
- `streaming.go` — `Delta`、`ToolCallAccumulator`（按 index 拼接参数片段）、
  `StreamAccumulator`（缓冲可能是工具调用前导的文本，首个发出的 chunk 携带 role）。
- `openai.go` / `openai_messages.go` / `messages.go` — `Bridge.Handler`、消息提取与转换、
  `BuildQoderMessages`/`ConvertIncomingMessage`（含四个工具调用特殊分支）、非流式聚合 +
  `parseToolCallsText` 兜底。

## 关键行为说明

### 时间方案（非常重要！）
存在两种不同的时间方案，**绝不能混淆**：
- Job-token 客户端（`internal/auth`）和 `signature.go` 使用 **RFC-1123 GMT**
- Bearer/流式客户端（`bearerclient.go` 中的 `SignedPostStream`）签名的是 **epoch 秒**的 `cosy-date`
混用会破坏上游鉴权——这与原始 Java 一致：两个客户端本就使用不同的方案。

### 消息转换热点函数
最繁忙的辅助函数：`BuildQoderMessages`、`ConvertIncomingMessage`、`normalizeContent*`、
`ExtractLatestUserPrompt`，以及 `ParseToolCallsText`/`NormalizeToolCalls` 中的工具调用解析。

工具调用根据 OpenAI 请求是否声明了 `tools` 分两条路径处理：
- **启用 tools**：透传 `tools`，将 assistant/tool 消息重建为结构化 `tool_calls`，并以流式
  发出 `tool_calls` 增量（`ToolCallAccumulator` 按 `index` 拼接参数片段）。
- **未启用 tools**：Qoder 仍可能输出形似工具调用的文本；代码会识别 `"Tool calls:"` 文本
  前缀（`IsPotentialToolCallText`），尝试将其重新解析为结构化调用（`ParseToolCallsText`），
  解析失败则丢弃缓冲。

### 流式处理行为
`StreamAccumulator` 会缓冲可能是工具调用前导的文本以便重新解析；首个 SSE chunk 还会携带
`role`。工具调用参数按 index 在各个 delta 之间拼接。

## 修改代码注意事项

- `baseprompt.json` 必须保持可被 JSON 解析（运行时用 `encoding/json` 解析）。它位于
  `internal/bridge/` 以便 `//go:embed`；编辑它会改变线上请求的形态。
- 顺序不可更改：`QoderEncoding.encode`、`buildPayloadB64` 的键排序、`signRequest` 的字段
  顺序，都是对照真实 Qoder 服务器调试出来的。改动任何一项，所有请求都会被上游拒绝。
- 上游是一个移动靶；`secret`、`SERVER_PUBKEY_PEM`、端点路径以及 `agent_chat_generation` 请求体
  结构都会随 Qoder 的变动而变化。`Sign` 和 `QoderEncoding` 的测试向量锁定了移植版的推导
  结果，算法漂移时需要同步更新。
- 真实上游行为（SSE 分帧、chunk 形状）目前仅通过纯函数的单元测试和构建期接线来验证——真正的
  `center.qoder.sh` / `api3.qoder.sh` 端点只有在集成阶段使用真实的 `QODER_PAT` 才会被实际
  调用。

## 文件位置

所有源代码位于 `/home/bincooo/workspaces/golang/20260830/qoder2api/` 下：

| 路径 | 描述 |
|------|------|
| `cmd/qoder2api/main.go` | 应用入口点 |
| `internal/auth/jobtoken.go` | 与 center.qoder.sh 交换 job token |
| `internal/cosy/*.go` | COSY 认证层（7 个源文件，4 个测试文件） |
| `internal/bridge/*.{go,json}` | OpenAI↔Qoder 映射（8 个源文件，6 个测试文件 + 模板） |

## 测试

运行所有测试：`go test ./...`

各包的测试套件揭示了重要行为：
- `internal/cosy/`: `TestComposeBearer`, `TestQoderEncoding`, `TestSignature` — 验证 bearer token 格式、base64 编码、签名计算
- `internal/bridge/`: `TestExtractLatestUserPrompt_LastUserWins`, `TestBuildExchangeBody`, 流式测试 — 提取最新用户提示、处理对话历史
- 每个测试文件都对应验证生产代码的行为

## 常见开发任务

1. **运行特定包的测试**：`go test -v ./internal/cosy/...`
2. **运行单个测试**：`go test -v -run TestName ./internal/...`
3. **构建二进制**：`go build -o qoder2api ./cmd/qoder2api`
4. **启动服务器**：`QODER_PAT=your_token go run ./cmd/qoder2api`
5. **检查覆盖率**：`go test -cover ./...`

## 相关文件

- `README.md` — 快速入门指南
- `go.mod` — 模块定义（Go 1.26.6，仅标准库）
- `.gitignore` — Git 忽略规则
- `internal/bridge/baseprompt.json` — 嵌入的 Qoder 请求模板
