package beakslack

import (
	"context"
	"encoding/json"
	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/sdk"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type interactionTestGateway struct {
	*fakeSDKGateway
	action sdk.InteractionAction
}

func (g *interactionTestGateway) HandleInteraction(_ context.Context, a sdk.InteractionAction) (*sdk.InteractionResult, error) {
	g.action = a
	return &sdk.InteractionResult{State: "approved"}, nil
}
func TestSlackInteractionSignedForm(t *testing.T) {
	account := sdkAccount("acct-1")
	gateway := &interactionTestGateway{fakeSDKGateway: &fakeSDKGateway{}}
	runtime := makeRuntime(gateway, newFakeSDKAccountStore(), account)
	var payload map[string]any
	runtime.HTTPClient = &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return testJSONResponse(map[string]any{"ok": true, "ts": "12.3"})
	})}
	card := &sdk.InteractionCard{RequestID: "approval_123", Revision: 3, Kind: "tool_approval", State: "pending", Title: "Approval", Summary: "Run operation", Actions: []sdk.InteractionButton{{ID: "approve", Label: "Approve"}}}
	result, err := NewConnector().Send(context.Background(), runtime, sdk.OutboundMessage{AccountUUID: account.UUID, ChatID: "C123", Interaction: card})
	if err != nil || result.MessageID != "12.3" {
		t.Fatalf("send: %#v %v", result, err)
	}
	if payload["blocks"] == nil {
		t.Fatal("missing Block Kit actions")
	}
	token, _ := card.ButtonToken(card.Actions[0])
	raw, _ := json.Marshal(map[string]any{"type": "block_actions", "team": map[string]string{"id": testTeamID}, "user": map[string]string{"id": "U123"}, "channel": map[string]string{"id": "C123"}, "message": map[string]string{"ts": "12.3"}, "actions": []map[string]string{{"action_id": "beak_interaction_approve", "value": token, "action_ts": "13.4"}}})
	body := []byte(url.Values{"payload": {string(raw)}}.Encode())
	request := signedSlackRequest(testSigningSecret, body, time.Now())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := (Connector{}).HandleWebhookRequest(context.Background(), runtime, account, request); err != nil {
		t.Fatal(err)
	}
	if gateway.action.ActorID != "U123" || gateway.action.ChatID != "C123" || gateway.action.RequestID != "approval_123" {
		t.Fatalf("identity: %#v", gateway.action)
	}
	request = signedSlackRequest("wrong-secret", body, time.Now())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := (Connector{}).HandleWebhookRequest(context.Background(), runtime, account, request); err == nil {
		t.Fatal("forged signature accepted")
	}
}

func TestSlackLongPlanPreservesAllTextWithinBlockLimits(t *testing.T) {
	account := sdkAccount("acct-1")
	runtime := makeRuntime(&interactionTestGateway{fakeSDKGateway: &fakeSDKGateway{}}, newFakeSDKAccountStore(), account)
	card := &sdk.InteractionCard{RequestID: "plan_123", Revision: 1, Kind: "plan_confirmation", State: "pending", Title: "计划", Summary: strings.Repeat("中文😀", 2500), Actions: []sdk.InteractionButton{{ID: "approve", Label: "批准"}}}
	runtime.HTTPClient = &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		blocks := payload["blocks"].([]any)
		if len(blocks) > 50 {
			t.Fatal("too many blocks")
		}
		for _, raw := range blocks {
			block := raw.(map[string]any)
			if block["type"] != "section" {
				continue
			}
			part := block["text"].(map[string]any)["text"].(string)
			if utf8.RuneCountInString(part) > 3000 {
				t.Fatal("section exceeds platform limit")
			}
			text.WriteString(part)
		}
		if text.String() != card.Title+"\n"+card.Summary {
			t.Fatal("plan text was lost or reordered")
		}
		return testJSONResponse(map[string]any{"ok": true, "ts": "12.3"})
	})}
	if _, err := NewConnector().Send(context.Background(), runtime, sdk.OutboundMessage{AccountUUID: account.UUID, ChatID: "C123", Interaction: card}); err != nil {
		t.Fatal(err)
	}
}
