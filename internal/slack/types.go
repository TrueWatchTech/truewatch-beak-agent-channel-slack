package slack

import "encoding/json"

// BotInfo is the normalized identity returned by Validate. Populated from the
// Slack auth.test response inside Client.Validate.
type BotInfo struct {
	AccountID   string
	TeamID      string
	BotID       string
	BotUserID   string
	DisplayName string
	BotName     string
}

// authTestResponse models the Slack auth.test response. On an invalid token
// Slack returns HTTP 200 with OK=false and a populated Error field.
type authTestResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
	User   string `json:"user"`
	Team   string `json:"team"`
}

// postMessageResponse models the Slack chat.postMessage response.
type postMessageResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// EventEnvelope is the outer Slack Events API payload. Event carries the inner
// event object, decoded separately so url_verification can be handled before
// signature verification.
type EventEnvelope struct {
	Type      string          `json:"type"`
	Token     string          `json:"token,omitempty"`
	TeamID    string          `json:"team_id,omitempty"`
	APIAppID  string          `json:"api_app_id,omitempty"`
	EventID   string          `json:"event_id,omitempty"`
	EventTime int64           `json:"event_time,omitempty"`
	Challenge string          `json:"challenge,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
}

// InnerEvent is the Slack inner event (message / app_mention).
type InnerEvent struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	Channel     string `json:"channel,omitempty"`
	User        string `json:"user,omitempty"`
	Text        string `json:"text,omitempty"`
	TS          string `json:"ts,omitempty"`
	ThreadTS    string `json:"thread_ts,omitempty"`
	ClientMsgID string `json:"client_msg_id,omitempty"`
	BotID       string `json:"bot_id,omitempty"`
	AppID       string `json:"app_id,omitempty"`
}
