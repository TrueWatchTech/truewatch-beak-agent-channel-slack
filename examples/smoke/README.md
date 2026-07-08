# Slack 联调 smoke 工具

`examples/smoke` 是一个**手动、真实凭证**的联调工具,脱离 Beak host、直接对真实 Slack 平台验证连接器的四件事:凭证连通、验签、入站事件解析、出站投递。它**不属于自动化测试**;凭证只从环境变量读,绝不硬编码,也不会打印 credential 本身。

三个子命令:

| 命令 | 证明什么 |
| --- | --- |
| `smoke validate` | 真实 API 连通 + 鉴权(打印 `Valid` / `AccountKey` / 解析出的 `State`,含 `bot_identity`) |
| `smoke serve [addr]` | 真实 webhook 入站:收回调 → `HandleWebhookRequest`(真验签+解析)→ 日志打印入站消息 |
| `smoke send <chat_id> <text>` | 真实出站投递 |

## 通用前置

- Go 1.23+。
- **一个公网可达的 HTTPS 地址**转发到本地 `:8080`(平台回调必须能打到你)。最简单:`ngrok http 8080`,拿到 `https://<public-host>`。

## 需要准备的环境变量与平台配置

在 [api.slack.com/apps](https://api.slack.com/apps) 创建一个 App(选测试 workspace):

| 环境变量 | 来源 |
| --- | --- |
| `BEAK_BOT_TOKEN`(必填) | OAuth & Permissions → Bot User OAuth Token(`xoxb-...`) |
| `BEAK_SIGNING_SECRET`(必填) | Basic Information → App Credentials → Signing Secret(入站 HMAC 验签用) |

**Bot Token Scopes**(OAuth & Permissions → Scopes,按要测的能力加):

- `chat:write` —— `send` 出站必需(`chat.postMessage`)
- `app_mentions:read` —— 接收 @机器人 事件
- `channels:history` / `groups:history` —— 接收频道/私有频道消息(且机器人须被拉进该频道)
- `im:history` / `im:read` —— 接收私聊 DM
- `validate` 走 `auth.test`,任何有效 token 均可,无需额外 scope

安装 App 到 workspace 后拿到上述 token。`chat_id` 是频道 ID(形如 `C0XXXXXXX`),@机器人后可从 `serve` 的入站日志里看到。

## 执行(在本 SDK 目录下)

```bash
export BEAK_BOT_TOKEN=xoxb-...
export BEAK_SIGNING_SECRET=...

# 1) 验证凭证 + 连通
go run ./examples/smoke validate

# 2) 起 webhook 接收(另开终端:ngrok http 8080)
go run ./examples/smoke serve :8080
#    Slack: Event Subscriptions → Enable → Request URL = https://<public-host>/
#    (工具自动应答 url_verification 握手,Slack 显示 Verified)
#    订阅 bot events: message.im / message.channels / app_mention → Save
#    然后在 Slack 里 @机器人 或发 DM
#    预期 serve 终端打印:
#      EnsureChatSession chat_type=... chat_id=...   <-- 真实入站已到 gateway
#      CreateMessage ... content="你发的内容"        <-- 已解析的入站消息

# 3) 出站
go run ./examples/smoke send <chat_id> "hello from beak"
#    预期:SendResult message_id=...,且会话里出现该消息
```

## 常见坑

| 症状 | 原因 |
| --- | --- |
| `validate` 返回 `invalid_auth` | token 不对或 App 未安装 |
| Request URL 一直 not verified | `serve` 没起 / ngrok 没转发 / 地址不通 |
| @机器人无反应 | 没订阅 `app_mentions:read` 等 events,或机器人不在该频道 |
| DM 收不到 | 缺 `im:history` / `im:read` |
| `send` 报 `missing_scope` | 缺 `chat:write` |
| `send` 报 `not_in_channel` | 机器人没加进目标频道 |

## 说明

这是**连接器级**联调:内置的 `logGateway` 把"入站已进 gateway"打到日志,代替真正的 Beak 入库;它证明 SDK 与真实 Slack 的连通、验签、事件解析、出站都正确。真正接入 Beak host 时,需由 host 侧实现真实的 `sdk.Gateway` / `sdk.AccountStore` 并路由 HTTP 请求给 `HandleWebhookRequest`。
