package slack

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/TrueWatchTech/truewatch-beak-agent-channel-slack/sdk"
)

func (c *Client) SendInteraction(ctx context.Context, chatID, threadID string, card sdk.InteractionCard) (string, error) {
	if err := card.Validate(); err != nil {
		return "", err
	}
	if len(card.Inputs) > 0 {
		return "", fmt.Errorf("Slack interaction input forms are unavailable")
	}
	// Keep the complete plan while respecting the limit of each text block.
	text := []rune(card.Title + "\n" + card.Summary)
	if len(text) > 49*3000 {
		return "", fmt.Errorf("Slack interaction exceeds the message block limit")
	}
	blocks := []any{}
	for len(text) > 0 {
		size := min(len(text), 3000)
		blocks = append(blocks, map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": string(text[:size])}})
		text = text[size:]
	}
	elements := []any{}
	for _, button := range card.Actions {
		token, err := card.ButtonToken(button)
		if err != nil {
			return "", err
		}
		elements = append(elements, map[string]any{"type": "button", "action_id": "beak_interaction_" + button.ID, "text": map[string]string{"type": "plain_text", "text": button.Label}, "value": token})
	}
	if len(elements) > 0 {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": elements})
	}
	payload := map[string]any{"channel": chatID, "text": card.Title + "\n" + card.Summary, "blocks": blocks}
	method := "chat.postMessage"
	if card.MessageID != "" {
		method = "chat.update"
		payload["ts"] = card.MessageID
	} else if threadID != "" {
		payload["thread_ts"] = threadID
	}
	var response postMessageResponse
	if err := c.doJSON(ctx, http.MethodPost, method, nil, payload, &response, withBearer(strings.TrimSpace(c.Credential["bot_token"]))); err != nil {
		return "", err
	}
	if !response.OK {
		return "", fmt.Errorf("slack %s: %s", method, response.Error)
	}
	if response.TS == "" {
		return "", fmt.Errorf("slack interaction response has no message id")
	}
	return response.TS, nil
}
