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

	"github.com/TrueWatch/beak-agent-channel-slack/sdk"
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
// ts (Slack's per-channel message id). markdown is delivered as mrkdwn;
// mentionAll prepends <!channel>.
func (c *Client) SendText(ctx context.Context, chatID, text, format string, mentions []sdk.MentionIdentity, mentionAll bool) (string, error) {
	token := strings.TrimSpace(c.Credential["bot_token"])
	if token == "" {
		return "", fmt.Errorf("slack bot_token is required")
	}
	if strings.TrimSpace(chatID) == "" {
		return "", fmt.Errorf("slack chat_id is required")
	}
	body := text
	if mentionAll {
		body = "<!channel> " + body
	}
	payload := map[string]any{
		"channel": chatID,
		"text":    body,
		"mrkdwn":  true,
	}
	_ = format
	_ = mentions
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

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
