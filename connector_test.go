package beakslack

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TrueWatch/beak-agent-channel-slack/sdk"
)

func TestConnectorImplementsInterface(t *testing.T) {
	var _ sdk.Connector = NewConnector()
}

func TestConnectorMetadata(t *testing.T) {
	meta := NewConnector().Metadata()
	if meta.ID != ID {
		t.Fatalf("id=%q want %q", meta.ID, ID)
	}
	if meta.Platform != Platform {
		t.Fatalf("platform=%q want %q", meta.Platform, Platform)
	}
	if meta.Label != "Slack" {
		t.Fatalf("label=%q want %q", meta.Label, "Slack")
	}
	if !meta.Capabilities.Text {
		t.Fatal("expected text capability")
	}
}

func TestConnectorCredentialSchema(t *testing.T) {
	schema := NewConnector().CredentialSchema(context.Background())
	if schema.Type != "object" {
		t.Fatalf("type=%q", schema.Type)
	}
	if schema.AdditionalProperties {
		t.Fatal("additionalProperties must be false")
	}
	for _, field := range []string{"bot_token", "signing_secret"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("missing credential field %q", field)
		}
		if !schema.Properties[field].Secret {
			t.Fatalf("%s must be marked secret", field)
		}
	}
	for _, banned := range []string{"base_url", "callback_url", "webhook_url", "offset", "bot_id"} {
		if _, ok := schema.Properties[banned]; ok {
			t.Fatalf("credential schema leaks backend field %q", banned)
		}
	}
}

func authTestOK() map[string]any {
	return map[string]any{"ok": true, "team_id": testTeamID, "user_id": testBotUserID, "bot_id": testBotID, "user": "mybot"}
}

func TestValidateCredential_Success(t *testing.T) {
	c := Connector{}
	res, err := c.ValidateCredential(context.Background(), sdk.CredentialValidationRequest{
		Credential: map[string]any{"bot_token": testBotToken, "signing_secret": testSigningSecret},
		Runtime:    sdk.Runtime{HTTPClient: httpClientReturning(authTestOK())},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected Valid=true, got error %q", res.Error)
	}
	if res.AccountKey != testTeamID+":"+testBotUserID {
		t.Fatalf("account key=%q", res.AccountKey)
	}
	if res.State["team_id"] != testTeamID || res.State["bot_id"] != testBotID || res.State["bot_user_id"] != testBotUserID {
		t.Fatalf("bot identity not persisted to state: %#v", res.State)
	}
}

func TestValidateCredential_InvalidToken(t *testing.T) {
	c := Connector{}
	res, err := c.ValidateCredential(context.Background(), sdk.CredentialValidationRequest{
		Credential: map[string]any{"bot_token": "bad", "signing_secret": testSigningSecret},
		Runtime:    sdk.Runtime{HTTPClient: httpClientReturning(map[string]any{"ok": false, "error": "invalid_auth"})},
	})
	if err != nil {
		t.Fatalf("invalid token must not return a Go error, got %v", err)
	}
	if res.Valid {
		t.Fatal("expected Valid=false")
	}
	if res.Error == "" {
		t.Fatal("expected Error to be populated")
	}
}

func TestValidateCredential_HTTPClientInjected(t *testing.T) {
	var sawPath, sawMethod string
	client := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawPath = req.URL.Path
		sawMethod = req.Method
		return testJSONResponse(authTestOK())
	})}
	c := Connector{}
	if _, err := c.ValidateCredential(context.Background(), sdk.CredentialValidationRequest{
		Credential: map[string]any{"bot_token": testBotToken},
		Runtime:    sdk.Runtime{HTTPClient: client},
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if sawPath != "/api/auth.test" {
		t.Fatalf("expected /api/auth.test, saw %q", sawPath)
	}
	if sawMethod != http.MethodPost {
		t.Fatalf("expected POST, saw %q", sawMethod)
	}
}

func TestStart_MissingGatewayReturnsError(t *testing.T) {
	c := Connector{}
	if err := c.Start(context.Background(), sdk.Runtime{}); err == nil {
		t.Fatal("expected error when Gateway is nil")
	}
}

func inbound(t *testing.T, ev slackInnerEvent) *EventResult {
	t.Helper()
	c := Connector{}
	res, err := c.HandleWebhook(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"), slackEventBody(testTeamID, ev))
	if err != nil {
		t.Fatalf("handle webhook: %v", err)
	}
	return res
}

func TestInbound_DirectText(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "message", ChannelType: "im", Channel: "D1", User: "U_HUMAN", Text: "hi", TS: "1.1", ClientMsgID: "a"})
	if res.Ignored {
		t.Fatalf("unexpected ignore: %q", res.Reason)
	}
	if res.Inbound == nil || res.Inbound.ChatType != sdk.ChatTypeDirect {
		t.Fatalf("expected direct chat, got %#v", res.Inbound)
	}
}

func TestInbound_GroupText(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "1.2", ClientMsgID: "b"})
	if res.Ignored || res.Inbound.ChatType != sdk.ChatTypeGroup {
		t.Fatalf("expected group chat, got ignored=%v %#v", res.Ignored, res.Inbound)
	}
}

func TestInbound_MentionMe(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "app_mention", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "<@U_BOT> hello", TS: "1.3", ClientMsgID: "c"})
	if res.Ignored || res.Inbound == nil || !res.Inbound.MentionedMe {
		t.Fatalf("expected MentionedMe=true, got ignored=%v %#v", res.Ignored, res.Inbound)
	}
}

func TestInbound_SelfEchoIgnored(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: testBotUserID, Text: "echo", TS: "1.4", ClientMsgID: "d"})
	if !res.Ignored || res.Reason != "self_echo" {
		t.Fatalf("expected self_echo ignore, got ignored=%v reason=%q", res.Ignored, res.Reason)
	}
}

func TestInbound_BotMessageSubtypeIgnored(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "message", Subtype: "bot_message", ChannelType: "channel", Channel: "C1", User: "U_OTHER", Text: "x", TS: "1.5", BotID: "B_OTHER", ClientMsgID: "e"})
	if !res.Ignored || res.Reason != "self_echo" {
		t.Fatalf("expected self_echo for bot_message subtype, got ignored=%v reason=%q", res.Ignored, res.Reason)
	}
}

func TestInbound_NonTextIgnored(t *testing.T) {
	res := inbound(t, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "", TS: "1.6", ClientMsgID: "f"})
	if !res.Ignored || res.Reason != "unsupported_message_type" {
		t.Fatalf("expected non-text ignore, got ignored=%v reason=%q", res.Ignored, res.Reason)
	}
}

func TestInbound_DuplicateIgnored(t *testing.T) {
	c := Connector{}
	rt := makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore())
	account := sdkAccount("acct-1")
	body := slackEventBody(testTeamID, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "2.0", ClientMsgID: "dup"})
	if _, err := c.HandleWebhook(context.Background(), rt, account, body); err != nil {
		t.Fatalf("first: %v", err)
	}
	res, err := c.HandleWebhook(context.Background(), rt, account, body)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !res.Ignored || res.Reason != "duplicate" {
		t.Fatalf("expected duplicate ignore, got ignored=%v reason=%q", res.Ignored, res.Reason)
	}
}

func TestInbound_AccountMismatch(t *testing.T) {
	c := Connector{}
	res, err := c.HandleWebhook(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"),
		slackEventBody("T_OTHER", slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "1.7", ClientMsgID: "g"}))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !res.Ignored || res.Reason != "team_mismatch" {
		t.Fatalf("expected team_mismatch, got ignored=%v reason=%q", res.Ignored, res.Reason)
	}
}

func TestInbound_SavesState(t *testing.T) {
	c := Connector{}
	store := newFakeSDKAccountStore()
	if _, err := c.HandleWebhook(context.Background(), makeRuntime(&fakeSDKGateway{}, store), sdkAccount("acct-1"),
		slackEventBody(testTeamID, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "1.8", ClientMsgID: "h"})); err != nil {
		t.Fatalf("handle: %v", err)
	}
	saved, _ := store.LoadChannelAccountState(context.Background(), "acct-1")
	peer, _ := saved["peer_sessions"].(map[string]any)
	seen, _ := saved["inbound_seen"].(map[string]any)
	if len(peer) == 0 || len(seen) == 0 {
		t.Fatalf("expected peer_sessions and inbound_seen persisted, got %#v", saved)
	}
}

func TestSend_Text(t *testing.T) {
	c := Connector{}
	account := sdkAccount("acct-1")
	rt := makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore(), account)
	rt.HTTPClient = httpClientReturning(map[string]any{"ok": true, "ts": "111.222"})
	res, err := c.Send(context.Background(), rt, sdk.OutboundMessage{AccountUUID: "acct-1", ChatID: "C1", Text: "hi"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.MessageID != "111.222" {
		t.Fatalf("message id=%q", res.MessageID)
	}
}

func TestSend_MentionAll(t *testing.T) {
	var sentText string
	client := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		sentText = string(data)
		return testJSONResponse(map[string]any{"ok": true, "ts": "1.1"})
	})}
	c := Connector{}
	account := sdkAccount("acct-1")
	rt := makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore(), account)
	rt.HTTPClient = client
	if _, err := c.Send(context.Background(), rt, sdk.OutboundMessage{AccountUUID: "acct-1", ChatID: "C1", Text: "ping", MentionAll: true}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// json.Marshal HTML-escapes "<" to <, so assert on the stable substring.
	if !strings.Contains(sentText, "!channel") {
		t.Fatalf("expected !channel mention in payload, got %q", sentText)
	}
}

func TestSend_MissingChatID(t *testing.T) {
	c := Connector{}
	account := sdkAccount("acct-1")
	rt := makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore(), account)
	if _, err := c.Send(context.Background(), rt, sdk.OutboundMessage{AccountUUID: "acct-1", Text: "hi"}); err == nil {
		t.Fatal("expected error for missing chat_id")
	}
}

func TestSend_MissingAccount(t *testing.T) {
	c := Connector{}
	rt := makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore())
	if _, err := c.Send(context.Background(), rt, sdk.OutboundMessage{AccountUUID: "ghost", ChatID: "C1", Text: "hi"}); err == nil {
		t.Fatal("expected error for unknown account")
	}
}

func TestWebhookSecurity_ValidSignature(t *testing.T) {
	c := Connector{}
	body := slackEventBody(testTeamID, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "9.1", ClientMsgID: "w1"})
	req := signedSlackRequest(testSigningSecret, body, time.Now().UTC())
	resp, err := c.HandleWebhookRequest(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"), req)
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestWebhookSecurity_TamperedSignature(t *testing.T) {
	c := Connector{}
	body := slackEventBody(testTeamID, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "9.2", ClientMsgID: "w2"})
	req := signedSlackRequest(testSigningSecret, body, time.Now().UTC())
	req.Header.Set("X-Slack-Signature", "v0=deadbeef")
	if _, err := c.HandleWebhookRequest(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"), req); err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestWebhookSecurity_ExpiredTimestamp(t *testing.T) {
	c := Connector{}
	body := slackEventBody(testTeamID, slackInnerEvent{Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "9.3", ClientMsgID: "w3"})
	req := signedSlackRequest(testSigningSecret, body, time.Now().UTC().Add(-10*time.Minute))
	if _, err := c.HandleWebhookRequest(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"), req); err == nil {
		t.Fatal("expected error for expired timestamp")
	}
}

func TestWebhookSecurity_URLVerificationChallenge(t *testing.T) {
	c := Connector{}
	req := signedSlackRequest("wrong-secret", []byte(`{"type":"url_verification","challenge":"abc123"}`), time.Now().UTC())
	resp, err := c.HandleWebhookRequest(context.Background(), makeRuntime(&fakeSDKGateway{}, newFakeSDKAccountStore()), sdkAccount("acct-1"), req)
	if err != nil {
		t.Fatalf("url_verification must bypass signature: %v", err)
	}
	if !strings.Contains(string(resp.Body), "abc123") {
		t.Fatalf("expected challenge echoed, got %q", string(resp.Body))
	}
}
