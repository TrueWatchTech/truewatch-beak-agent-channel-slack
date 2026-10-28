package beakslack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	platform "github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/internal/slack"
	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/sdk"
)

func sendSlackInteraction(ctx context.Context, runtime sdk.Runtime, req sdk.OutboundMessage) (*sdk.SendResult, error) {
	account, err := selectRuntimeAccount(runtime, req.AccountUUID)
	if err != nil {
		return nil, err
	}
	client := platform.NewClient("", credentialStrings(account.Credential))
	client.HTTPClient = runtime.HTTPClient
	id, err := client.SendInteraction(ctx, req.ChatID, slackOutboundThreadTS(req), *req.Interaction)
	if err != nil {
		return nil, err
	}
	return &sdk.SendResult{Platform: Platform, AccountUUID: accountKey(account), MessageID: id}, nil
}

func handleSlackInteraction(ctx context.Context, runtime sdk.Runtime, account sdk.ChannelAccount, req *http.Request, body []byte) (*sdk.WebhookResponse, error) {
	if err := platform.VerifyWebhookSignature(stringValue(account.Credential["signing_secret"]), req.Header.Get("X-Slack-Request-Timestamp"), req.Header.Get("X-Slack-Signature"), body, time.Now().UTC()); err != nil {
		return nil, err
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Type string `json:"type"`
		Team struct {
			ID string `json:"id"`
		} `json:"team"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
		Message struct {
			TS string `json:"ts"`
		} `json:"message"`
		Actions []struct {
			ID    string `json:"action_id"`
			Value string `json:"value"`
			TS    string `json:"action_ts"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil {
		return nil, err
	}
	expectedTeam := stringValue(account.State["team_id"])
	if expectedTeam == "" || expectedTeam != payload.Team.ID {
		return nil, fmt.Errorf("slack callback team mismatch")
	}
	if payload.Type != "block_actions" || len(payload.Actions) != 1 {
		return nil, fmt.Errorf("unsupported Slack interaction")
	}
	button := payload.Actions[0]
	action, err := sdk.ParseInteractionToken(button.Value)
	if err != nil {
		return nil, err
	}
	if button.ID != "beak_interaction_"+action.ActionID {
		return nil, fmt.Errorf("slack callback action mismatch")
	}
	action.ActorID = payload.User.ID
	action.ChatID = payload.Channel.ID
	action.MessageID = payload.Message.TS
	action.EventID = button.TS
	actionCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	if _, err := sdk.DispatchInteraction(actionCtx, runtime, action); err != nil {
		return nil, err
	}
	return &sdk.WebhookResponse{StatusCode: http.StatusOK}, nil
}
