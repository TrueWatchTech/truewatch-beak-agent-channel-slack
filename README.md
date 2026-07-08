# Slack Channel SDK

`github.com/TrueWatch/beak-agent-channel-slack` connects Slack bot accounts to the Beak Channel Gateway. It
implements the common `sdk.Connector` interface plus a slack-specific
inbound entry point.

Platform-specific logic — `Validate`, `SendText`, webhook verification, and
inbound event parsing — is fully implemented. Use `NewConnector()` to obtain a
`sdk.Connector` ready for registration.

## Module

```
github.com/TrueWatch/beak-agent-channel-slack
```

## Usage

```go
import (
    "fmt"

    beak "github.com/TrueWatch/beak-agent-channel-slack"
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

## Event delivery

Mode: **webhook**

The Beak host owns the HTTP endpoint and forwards the raw request to
`HandleWebhookRequest`, which verifies the signature and parses the event.


## Webhook security

Strategy: **hmac_sha256**



## Outbound

`Send` maps the common `OutboundMessage` (`Text`, `Format`, `Mentions`,
`MentionAll`) onto the Slack send endpoint (`/api/chat.postMessage`).

## State

Account-scoped state lives in `state/state.go`. Common collections plus the
platform fields are persisted via `sdk.AccountStore`.
