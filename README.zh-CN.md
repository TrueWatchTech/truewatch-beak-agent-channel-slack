# Slack Channel SDK

`github.com/TrueWatchTech/truewatch-beak-agent-channel-slack` 将 Slack 机器人账号接入 Beak Channel Gateway，实现通用
`sdk.Connector` 接口以及 slack 专属的入站入口。

平台专属逻辑（`Validate`、`SendText`、webhook 验签、入站事件解析）均已完整实现。
使用 `NewConnector()` 获取可直接注册的 `sdk.Connector`。

## Module

```
github.com/TrueWatchTech/truewatch-beak-agent-channel-slack
```

## 使用示例

```go
import (
    "fmt"

    beak "github.com/TrueWatchTech/truewatch-beak-agent-channel-slack"
)

func main() {
    connector := beak.NewConnector()
    fmt.Println(connector.Metadata().Label)
}
```

## 凭证字段

以下字段会出现在 Beak 控制台表单（`CredentialSchema`）中：

| Key | 名称 | Secret | 必填 |
| --- | --- | --- | --- |
| `bot_token` | Bot Token | 是 | 是 |
| `signing_secret` | Signing Secret | 是 | 是 |

后端字段（base URL、回调地址、offset、token 缓存）不会出现在表单中。

## Slack App 权限

建议配置以下 bot scopes：

- `chat:write`：发送出站消息。
- `reactions:write`：通过 `Acknowledge` 发送处理中 reaction。
- `users:read`：获取发送人展示名和头像。
- `channels:read`、`groups:read`、`im:read`、`mpim:read`：获取会话展示名。
- `app_mentions:read`、`channels:history`、`groups:history`、`im:history`、`mpim:history`：按 Slack Events API 订阅范围配置。

## 入站投递

模式：**webhook**

由 Beak host 持有 HTTP endpoint，并将原始请求转交给 `HandleWebhookRequest`，
由其完成验签与事件解析。

入站消息会输出 Beak 标准字段，包括 `thread_id`、`mentions`、`mentioned_me`、
`mention_all`、`chat_identity`、`chat_display_name`、`sender_display_name`。
Slack 展示信息是尽力获取：API 查询失败不会导致入站消息丢失。

## Webhook 验签

策略：**hmac_sha256**



## 出站

`Send` 将通用 `OutboundMessage`（`Text`、`Format`、`ThreadID`、`Mentions`、
`MentionAll`）映射到 Slack 发送接口（`/api/chat.postMessage`）。`ThreadID`
会作为 Slack `thread_ts` 发送；兼容从 `Raw` 的 `thread_ts` / `thread_id`
读取线程上下文。

`Acknowledge` 暴露 `AckModes=["reaction"]`，将处理中提示映射到 Slack
`reactions.add`。缺少目标消息 ID 时返回 skipped，不阻断最终 Agent 回复；
不支持的 mode 返回 `Status="unsupported"`。

## State

账号维度状态位于 `state/state.go`，通用集合与平台字段通过 `sdk.AccountStore` 持久化。
