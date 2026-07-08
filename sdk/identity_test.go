package sdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityHelpers(t *testing.T) {
	if got := ChatSourceID("slack", "account-1", "group", "chat-1"); got != "slack:account-1:group:chat-1" {
		t.Fatalf("source id=%q", got)
	}
	participant := IMPersonParticipantID("slack", "direct", "chat-1", "user-1")
	if participant != "im:slack:direct:chat-1:user:user-1" {
		t.Fatalf("participant=%q", participant)
	}
	if !strings.Contains(participant, "slack") {
		t.Fatalf("participant missing platform key: %q", participant)
	}
	bridge := BridgeParticipantID("slack")
	if bridge != "bridge:slack" {
		t.Fatalf("bridge=%q", bridge)
	}
	if !strings.Contains(bridge, "slack") {
		t.Fatalf("bridge missing platform key: %q", bridge)
	}
}

func TestOutboundMessageCommonFormatContract(t *testing.T) {
	data, err := json.Marshal(OutboundMessage{
		Text:     "# title\n- item",
		Format:   "markdown",
		Title:    "title",
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "# title\n- item" || decoded["format"] != "markdown" || decoded["title"] != "title" || decoded["thread_id"] != "thread-1" {
		t.Fatalf("common outbound json=%+v", decoded)
	}
}

func TestOutboundAckCommonContract(t *testing.T) {
	data, err := json.Marshal(OutboundAck{
		Platform:        "slack",
		AccountUUID:     "acct-1",
		ChatType:        ChatTypeGroup,
		ChatID:          "C1",
		TargetMessageID: "123.456",
		Intent:          "processing",
		Action:          "start",
		Mode:            "reaction",
		Emoji:           "eyes",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["target_message_id"] != "123.456" || decoded["mode"] != "reaction" || decoded["emoji"] != "eyes" {
		t.Fatalf("common ack json=%+v", decoded)
	}
}
