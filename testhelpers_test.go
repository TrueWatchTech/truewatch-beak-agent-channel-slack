package beakslack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TrueWatch/beak-agent-channel-slack/sdk"
)

const (
	testSigningSecret = "test-signing-secret"
	testBotToken      = "xoxb-test-token"
	testTeamID        = "T123"
	testBotUserID     = "U_BOT"
	testBotID         = "B_BOT"
)

// fakeSDKGateway records the EnsureChatSession / CreateMessage calls it receives.
type fakeSDKGateway struct {
	mu           sync.Mutex
	chatSessions []sdk.EnsureChatSessionRequest
	messages     []sdk.CreateMessageRequest
}

func (g *fakeSDKGateway) EnsureChannel(_ context.Context, _ sdk.EnsureChannelRequest) (string, error) {
	return "channel-1", nil
}

func (g *fakeSDKGateway) EnsureChannelLinkSession(_ context.Context, req sdk.EnsureChannelLinkSessionRequest) (string, error) {
	return "link-" + req.AccountUUID, nil
}

func (g *fakeSDKGateway) EnsureChatSession(_ context.Context, req sdk.EnsureChatSessionRequest) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.chatSessions = append(g.chatSessions, req)
	return "session-" + req.AccountUUID + "-" + req.ChatType + "-" + req.ChatID, nil
}

func (g *fakeSDKGateway) CreateMessage(_ context.Context, req sdk.CreateMessageRequest) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.messages = append(g.messages, req)
	return "message-" + strconv.Itoa(len(g.messages)), nil
}

func (g *fakeSDKGateway) StreamSession(_ context.Context, _ sdk.StreamSessionRequest, _ func(sdk.StreamEvent) error) error {
	return nil
}

func (g *fakeSDKGateway) AgentParticipantID() string { return "agent:agent-1" }

func (g *fakeSDKGateway) BridgeParticipantID(platform string) string {
	return sdk.BridgeParticipantID(platform)
}

func (g *fakeSDKGateway) messageCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.messages)
}

// fakeSDKAccountStore is an in-memory AccountStore.
type fakeSDKAccountStore struct {
	mu     sync.Mutex
	states map[string]map[string]any
}

func newFakeSDKAccountStore() *fakeSDKAccountStore {
	return &fakeSDKAccountStore{states: make(map[string]map[string]any)}
}

func (s *fakeSDKAccountStore) SaveChannelAccountState(_ context.Context, accountUUID string, state map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make(map[string]any, len(state))
	for key, value := range state {
		copied[key] = value
	}
	s.states[accountUUID] = copied
	return nil
}

func (s *fakeSDKAccountStore) LoadChannelAccountState(_ context.Context, accountUUID string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[accountUUID]
	copied := make(map[string]any, len(state))
	for key, value := range state {
		copied[key] = value
	}
	return copied, nil
}

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (f testRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// testJSONResponse builds a 200 response whose body is the JSON encoding of v.
func testJSONResponse(v any) (*http.Response, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}, nil
}

// httpClientReturning returns an *http.Client whose transport answers every
// request with the given JSON value.
func httpClientReturning(v any) *http.Client {
	return &http.Client{Transport: testRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testJSONResponse(v)
	})}
}

// sdkAccount builds an account whose credential + state carry the bot identity
// produced by ValidateCredential.
func sdkAccount(uuid string) sdk.ChannelAccount {
	return sdk.ChannelAccount{
		UUID:     uuid,
		Platform: Platform,
		Credential: map[string]any{
			"account_id":     testTeamID + ":" + testBotUserID,
			"bot_id":         testBotID,
			"bot_user_id":    testBotUserID,
			"bot_token":      testBotToken,
			"signing_secret": testSigningSecret,
		},
		State: map[string]any{
			"team_id":     testTeamID,
			"bot_id":      testBotID,
			"bot_user_id": testBotUserID,
		},
	}
}

func makeRuntime(gw sdk.Gateway, store sdk.AccountStore, accounts ...sdk.ChannelAccount) sdk.Runtime {
	rt := sdk.Runtime{
		WorkspaceUUID: "ws-1",
		Channel:       sdk.Channel{UUID: "channel-1", Platform: Platform},
		Gateway:       gw,
		AccountStore:  store,
	}
	if len(accounts) > 0 {
		rt.Account = accounts[0]
		rt.Accounts = accounts
	}
	return rt
}

// slackInnerEvent is a builder for the inner Slack event object.
type slackInnerEvent struct {
	Type        string
	Subtype     string
	ChannelType string
	Channel     string
	User        string
	Text        string
	TS          string
	ClientMsgID string
	BotID       string
	AppID       string
}

// slackEventBody marshals an event_callback envelope wrapping ev.
func slackEventBody(teamID string, ev slackInnerEvent) []byte {
	inner := map[string]any{"type": ev.Type}
	if ev.Subtype != "" {
		inner["subtype"] = ev.Subtype
	}
	if ev.ChannelType != "" {
		inner["channel_type"] = ev.ChannelType
	}
	if ev.Channel != "" {
		inner["channel"] = ev.Channel
	}
	if ev.User != "" {
		inner["user"] = ev.User
	}
	if ev.Text != "" {
		inner["text"] = ev.Text
	}
	if ev.TS != "" {
		inner["ts"] = ev.TS
	}
	if ev.ClientMsgID != "" {
		inner["client_msg_id"] = ev.ClientMsgID
	}
	if ev.BotID != "" {
		inner["bot_id"] = ev.BotID
	}
	if ev.AppID != "" {
		inner["app_id"] = ev.AppID
	}
	innerJSON, _ := json.Marshal(inner)
	env := map[string]any{
		"type":       "event_callback",
		"team_id":    teamID,
		"api_app_id": "A123",
		"event_id":   "Ev" + ev.TS,
		"event":      json.RawMessage(innerJSON),
	}
	out, _ := json.Marshal(env)
	return out
}

// signedSlackRequest builds an *http.Request with a valid Slack signature over
// body using secret and the given timestamp.
func signedSlackRequest(secret string, body []byte, ts time.Time) *http.Request {
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + tsStr + ":" + string(body)))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Slack-Request-Timestamp", tsStr)
	req.Header.Set("X-Slack-Signature", sig)
	return req
}
