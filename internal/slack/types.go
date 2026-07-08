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

type ChannelInfo struct {
	ID        string
	Name      string
	IsChannel bool
	IsGroup   bool
	IsIM      bool
	IsMPIM    bool
	User      string
}

type UserInfo struct {
	ID          string
	TeamID      string
	Name        string
	RealName    string
	DisplayName string
	AvatarURL   string
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

type conversationInfoResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Channel struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		NameNormalized string `json:"name_normalized"`
		IsChannel      bool   `json:"is_channel"`
		IsGroup        bool   `json:"is_group"`
		IsIM           bool   `json:"is_im"`
		IsMPIM         bool   `json:"is_mpim"`
		User           string `json:"user"`
	} `json:"channel"`
}

type userInfoResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	User  struct {
		ID       string `json:"id"`
		TeamID   string `json:"team_id"`
		Name     string `json:"name"`
		RealName string `json:"real_name"`
		Profile  struct {
			RealName              string `json:"real_name"`
			DisplayName           string `json:"display_name"`
			RealNameNormalized    string `json:"real_name_normalized"`
			DisplayNameNormalized string `json:"display_name_normalized"`
			ImageOriginal         string `json:"image_original"`
			Image512              string `json:"image_512"`
			Image192              string `json:"image_192"`
			Image72               string `json:"image_72"`
			Image48               string `json:"image_48"`
		} `json:"profile"`
	} `json:"user"`
}

type addReactionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
	EventTS     string `json:"event_ts,omitempty"`
	ThreadTS    string `json:"thread_ts,omitempty"`
	ClientMsgID string `json:"client_msg_id,omitempty"`
	BotID       string `json:"bot_id,omitempty"`
	AppID       string `json:"app_id,omitempty"`
}
