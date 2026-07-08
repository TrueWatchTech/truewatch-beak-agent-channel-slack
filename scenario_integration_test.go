package beakslack

import (
	"context"
	"testing"

	"github.com/TrueWatch/beak-agent-channel-slack/sdk"
)

// TestScenario_DirectAndGroupDoNotShareSession: the same account receiving a
// direct message and a group message must get distinct sessions.
func TestScenario_DirectAndGroupDoNotShareSession(t *testing.T) {
	c := Connector{}
	gw := &fakeSDKGateway{}
	store := newFakeSDKAccountStore()
	account := sdkAccount("acct-1")
	rt := makeRuntime(gw, store)

	direct, err := c.HandleWebhook(context.Background(), rt, account, slackEventBody(testTeamID, slackInnerEvent{
		Type: "message", ChannelType: "im", Channel: "D1", User: "U_HUMAN", Text: "hi", TS: "1.1", ClientMsgID: "m1",
	}))
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	group, err := c.HandleWebhook(context.Background(), rt, account, slackEventBody(testTeamID, slackInnerEvent{
		Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "hi", TS: "2.2", ClientMsgID: "m2",
	}))
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	if direct.SessionUUID == "" || group.SessionUUID == "" {
		t.Fatalf("missing session uuids: %q %q", direct.SessionUUID, group.SessionUUID)
	}
	if direct.SessionUUID == group.SessionUUID {
		t.Fatalf("direct and group must not share session: %q", direct.SessionUUID)
	}
}

// TestScenario_TwoAccountsSameGroupDoNotShareSession guards the core contract:
// session identity includes the account uuid, so two bots in the same group
// must not share a Beak session.
func TestScenario_TwoAccountsSameGroupDoNotShareSession(t *testing.T) {
	c := Connector{}
	gw := &fakeSDKGateway{}
	store := newFakeSDKAccountStore()

	body := slackEventBody(testTeamID, slackInnerEvent{
		Type: "message", ChannelType: "channel", Channel: "C_SHARED", User: "U_HUMAN", Text: "hello team", TS: "3.3", ClientMsgID: "m3",
	})

	resA, err := c.HandleWebhook(context.Background(), makeRuntime(gw, store), sdkAccount("acct-A"), body)
	if err != nil {
		t.Fatalf("account A: %v", err)
	}
	resB, err := c.HandleWebhook(context.Background(), makeRuntime(gw, store), sdkAccount("acct-B"), body)
	if err != nil {
		t.Fatalf("account B: %v", err)
	}
	if resA.SessionUUID == "" || resB.SessionUUID == "" {
		t.Fatalf("missing session uuids: %q %q", resA.SessionUUID, resB.SessionUUID)
	}
	if resA.SessionUUID == resB.SessionUUID {
		t.Fatalf("two accounts in the same group must not share session: %q", resA.SessionUUID)
	}
}

// TestScenario_DuplicateEventNotDuplicated: replaying the same event must be
// ignored and must not create a second Beak message.
func TestScenario_DuplicateEventNotDuplicated(t *testing.T) {
	c := Connector{}
	gw := &fakeSDKGateway{}
	store := newFakeSDKAccountStore()
	account := sdkAccount("acct-1")
	rt := makeRuntime(gw, store)

	body := slackEventBody(testTeamID, slackInnerEvent{
		Type: "message", ChannelType: "channel", Channel: "C1", User: "U_HUMAN", Text: "once", TS: "4.4", ClientMsgID: "dup-1",
	})

	first, err := c.HandleWebhook(context.Background(), rt, account, body)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Ignored {
		t.Fatalf("first delivery must not be ignored")
	}
	second, err := c.HandleWebhook(context.Background(), rt, account, body)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !second.Ignored || second.Reason != "duplicate" {
		t.Fatalf("second delivery must be ignored as duplicate, got ignored=%v reason=%q", second.Ignored, second.Reason)
	}
	if got := gw.messageCount(); got != 1 {
		t.Fatalf("expected exactly 1 message created, got %d", got)
	}
}

// TestScenario_MarkdownOutboundSentOrDegraded: a markdown outbound message is
// delivered (Slack mrkdwn) and returns the platform message id.
func TestScenario_MarkdownOutboundSentOrDegraded(t *testing.T) {
	c := Connector{}
	gw := &fakeSDKGateway{}
	store := newFakeSDKAccountStore()
	account := sdkAccount("acct-1")
	rt := makeRuntime(gw, store, account)
	rt.HTTPClient = httpClientReturning(map[string]any{"ok": true, "ts": "1700000000.000100", "channel": "C1"})

	res, err := c.Send(context.Background(), rt, sdk.OutboundMessage{
		AccountUUID: "acct-1",
		ChatID:      "C1",
		Text:        "*bold* message",
		Format:      "markdown",
	})
	if err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	if res.MessageID == "" {
		t.Fatalf("expected a message id from chat.postMessage")
	}
}
