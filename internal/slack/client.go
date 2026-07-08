package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/sdk"
)

// DefaultBaseURL is the Slack API base. Override via NewClient for private
// deployments or tests; do not surface it in CredentialSchema.
const DefaultBaseURL = "https://slack.com/api"

const defaultRequestTimeout = 15 * time.Second

// Client is the Slack HTTP client. Credentials are kept in a map so the
// scaffold stays platform-agnostic; read them with stringValue helpers in the
// methods you implement.
//
// Credential fields supplied by CredentialSchema:
//   - bot_token: Bot Token
//   - signing_secret: Signing Secret
type Client struct {
	BaseURL        string
	Credential     map[string]string
	RequestTimeout time.Duration
	HTTPClient     *http.Client
}

func NewClient(baseURL string, credential map[string]string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if credential == nil {
		credential = make(map[string]string)
	}
	return &Client{
		BaseURL:        baseURL,
		Credential:     credential,
		RequestTimeout: defaultRequestTimeout,
		HTTPClient:     http.DefaultClient,
	}
}

// Validate calls Slack auth.test and returns normalized bot identity. HTTP and
// parsing failures are returned as Go errors; an invalid token (Slack returns
// ok=false on HTTP 200) is also returned as a Go error and mapped to
// Valid=false by the connector.
func (c *Client) Validate(ctx context.Context) (*BotInfo, error) {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	var resp authTestResponse
	// BaseURL already includes /api, so the path is the bare method name.
	if err := c.doJSON(ctx, http.MethodPost, "auth.test", nil, nil, &resp, withBearer(token)); err != nil {
		return nil, err
	}
	if !resp.OK {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "invalid_auth"
		}
		return nil, fmt.Errorf("slack auth.test: %s", errMsg)
	}
	accountID := resp.TeamID
	if resp.UserID != "" {
		accountID = resp.TeamID + ":" + resp.UserID
	}
	return &BotInfo{
		AccountID:   accountID,
		TeamID:      resp.TeamID,
		BotID:       resp.BotID,
		BotUserID:   resp.UserID,
		DisplayName: firstNonEmpty(resp.User, resp.Team),
		BotName:     resp.User,
	}, nil
}

// SendText sends a message via Slack chat.postMessage and returns the message
// ts (Slack's per-channel message id). Markdown is delivered as Slack mrkdwn.
func (c *Client) SendText(ctx context.Context, chatID, threadTS, text, format string, mentions []sdk.MentionIdentity, mentionAll bool) (string, error) {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return "", fmt.Errorf("slack bot_token is required")
	}
	if strings.TrimSpace(chatID) == "" {
		return "", fmt.Errorf("slack chat_id is required")
	}
	body := slackMentionPrefix(mentions, text)
	if mentionAll {
		body = "<!channel> " + body
	}
	payload := map[string]any{
		"channel": chatID,
		"text":    body,
		"mrkdwn":  true,
	}
	if threadTS = strings.TrimSpace(threadTS); threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	_ = format
	var resp postMessageResponse
	if err := c.doJSON(ctx, http.MethodPost, "chat.postMessage", nil, payload, &resp, withBearer(token)); err != nil {
		return "", err
	}
	if !resp.OK {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "post_message_failed"
		}
		return "", fmt.Errorf("slack chat.postMessage: %s", errMsg)
	}
	return resp.TS, nil
}

func (c *Client) ConversationInfo(ctx context.Context, channelID string) (*ChannelInfo, error) {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, fmt.Errorf("slack channel_id is required")
	}
	var resp conversationInfoResponse
	if err := c.doJSON(ctx, http.MethodGet, "conversations.info", map[string]string{"channel": channelID}, nil, &resp, withBearer(token)); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, slackAPIError("conversations.info", resp.Error)
	}
	info := &ChannelInfo{
		ID:        firstNonEmpty(resp.Channel.ID, channelID),
		Name:      firstNonEmpty(resp.Channel.NameNormalized, resp.Channel.Name),
		IsChannel: resp.Channel.IsChannel,
		IsGroup:   resp.Channel.IsGroup,
		IsIM:      resp.Channel.IsIM,
		IsMPIM:    resp.Channel.IsMPIM,
		User:      resp.Channel.User,
	}
	return info, nil
}

func (c *Client) UserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("slack user_id is required")
	}
	var resp userInfoResponse
	if err := c.doJSON(ctx, http.MethodGet, "users.info", map[string]string{"user": userID}, nil, &resp, withBearer(token)); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, slackAPIError("users.info", resp.Error)
	}
	return &UserInfo{
		ID:          firstNonEmpty(resp.User.ID, userID),
		TeamID:      resp.User.TeamID,
		Name:        resp.User.Name,
		RealName:    firstNonEmpty(resp.User.Profile.RealNameNormalized, resp.User.Profile.RealName, resp.User.RealName),
		DisplayName: firstNonEmpty(resp.User.Profile.DisplayNameNormalized, resp.User.Profile.DisplayName, resp.User.Name, resp.User.RealName),
		AvatarURL: firstNonEmpty(
			resp.User.Profile.Image512,
			resp.User.Profile.Image192,
			resp.User.Profile.Image72,
			resp.User.Profile.Image48,
			resp.User.Profile.ImageOriginal,
		),
	}, nil
}

func (c *Client) AddReaction(ctx context.Context, channelID, timestamp, name string) error {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return fmt.Errorf("slack bot_token is required")
	}
	channelID = strings.TrimSpace(channelID)
	timestamp = strings.TrimSpace(timestamp)
	name = strings.Trim(strings.TrimSpace(name), ":")
	if channelID == "" {
		return fmt.Errorf("slack channel_id is required")
	}
	if timestamp == "" {
		return fmt.Errorf("slack message timestamp is required")
	}
	if name == "" {
		return fmt.Errorf("slack reaction name is required")
	}
	payload := map[string]any{
		"channel":   channelID,
		"timestamp": timestamp,
		"name":      name,
	}
	var resp addReactionResponse
	if err := c.doJSON(ctx, http.MethodPost, "reactions.add", nil, payload, &resp, withBearer(token)); err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "already_reacted" {
			return nil
		}
		return slackAPIError("reactions.add", resp.Error)
	}
	return nil
}

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func slackMentionPrefix(mentions []sdk.MentionIdentity, text string) string {
	var parts []string
	seen := make(map[string]bool)
	for _, mention := range mentions {
		id := strings.TrimSpace(mention.ID)
		if id == "" || seen[id] || strings.Contains(text, "<@"+id+">") || strings.Contains(text, "<@"+id+"|") {
			continue
		}
		seen[id] = true
		parts = append(parts, "<@"+id+">")
	}
	if len(parts) == 0 {
		return text
	}
	return strings.Join(parts, " ") + " " + text
}

func slackAPIError(method, errMsg string) error {
	if errMsg == "" {
		errMsg = "platform_error"
	}
	return fmt.Errorf("slack %s: %s", method, errMsg)
}

type requestOption func(*http.Request)

func withBearer(token string) requestOption {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (c *Client) doJSON(ctx context.Context, method, path string, query map[string]string, body any, out any, opts ...requestOption) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	timeout := c.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, c.url(path, query), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BeakAgentSlack/0.1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for _, opt := range opts {
		opt(req)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s failed: status=%d body=%s", method, path, resp.StatusCode, string(data))
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) url(path string, query map[string]string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	values := url.Values{}
	for key, value := range query {
		if value != "" {
			values.Set(key, value)
		}
	}
	out := base + "/" + strings.TrimLeft(path, "/")
	if encoded := values.Encode(); encoded != "" {
		out += "?" + encoded
	}
	return out
}
