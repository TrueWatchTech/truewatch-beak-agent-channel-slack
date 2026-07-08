# Slack Channel SDK

`github.com/TrueWatchTech/truewatch-beak-agent-channel-slack` connects Slack bot accounts to the Beak Channel Gateway. It
implements the common `sdk.Connector` interface plus a slack-specific
inbound entry point.

Platform-specific logic — `Validate`, `SendText`, webhook verification, and
inbound event parsing — is fully implemented. Use `NewConnector()` to obtain a
`sdk.Connector` ready for registration.

## Module

```
github.com/TrueWatchTech/truewatch-beak-agent-channel-slack
```

## Usage

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

## Credential fields

These are the fields surfaced in the Beak console form (`CredentialSchema`):

| Key | Title | Secret | Required |
| --- | --- | --- | --- |
| `bot_token` | Bot Token | yes | yes |
| `signing_secret` | Signing Secret | yes | yes |

Backend-only values (base URL, callback URL, offsets, token cache) are never
exposed in the form.

## Slack app scopes

Recommended bot scopes:

- `chat:write` for outbound messages.
- `reactions:write` for `Acknowledge` reaction hints.
- `users:read` for sender display names and avatars.
- `channels:read`, `groups:read`, `im:read`, `mpim:read` for conversation display names.
- `app_mentions:read`, `channels:history`, `groups:history`, `im:history`, `mpim:history` according to the Events API subscriptions enabled for the app.

## Event delivery

Mode: **webhook**

The Beak host owns the HTTP endpoint and forwards the raw request to
`HandleWebhookRequest`, which verifies the signature and parses the event.

Inbound messages expose standard Beak fields, including `thread_id`, `mentions`,
`mentioned_me`, `mention_all`, `chat_identity`, `chat_display_name`, and
`sender_display_name`. Thread replies also expose `referenced_message`: when
`thread_ts` differs from the current message `ts`, the SDK treats `thread_ts` as
the parent message id and best-effort fetches it through
`conversations.replies`. Slack display and parent-message lookups are
best-effort: API lookup failures do not drop the inbound message.

## Webhook security

Strategy: **hmac_sha256**



## Outbound

`Send` maps the common `OutboundMessage` (`Text`, `Format`, `ThreadID`,
`Mentions`, `MentionAll`) onto the Slack send endpoint
(`/api/chat.postMessage`). `ThreadID` is sent as Slack `thread_ts`; `Raw`
`thread_ts` / `thread_id` are accepted as compatibility fallbacks.

`Acknowledge` exposes `AckModes=["reaction"]` and maps processing hints to
Slack `reactions.add`. Missing target message ids are skipped without failing
the final Agent reply path; unsupported modes return `Status="unsupported"`.

## State

Account-scoped state lives in `state/state.go`. Common collections plus the
platform fields are persisted via `sdk.AccountStore`.
