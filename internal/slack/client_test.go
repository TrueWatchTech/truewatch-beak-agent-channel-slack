package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (f testRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResp(body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestClientValidate(t *testing.T) {
	var sawValidate bool
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/auth.test" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Fatalf("missing bearer token, got %q", got)
		}
		sawValidate = true
		return jsonResp(`{"ok":true,"team_id":"T1","user_id":"U1","bot_id":"B1","user":"mybot"}`)
	})}

	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient

	info, err := client.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sawValidate {
		t.Fatal("validate endpoint was not called")
	}
	if info.AccountID != "T1:U1" || info.TeamID != "T1" || info.BotID != "B1" || info.BotUserID != "U1" {
		t.Fatalf("unexpected bot info: %#v", info)
	}
}

func TestClientValidate_InvalidToken(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResp(`{"ok":false,"error":"invalid_auth"}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "bad"})
	client.HTTPClient = httpClient

	if _, err := client.Validate(context.Background()); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestClientSendText(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/chat.postMessage" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return jsonResp(`{"ok":true,"ts":"123.456","channel":"C1"}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient

	ts, err := client.SendText(context.Background(), "C1", "", "hi", "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if ts != "123.456" {
		t.Fatalf("ts=%q", ts)
	}
}

func TestClientSendText_PlatformError(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResp(`{"ok":false,"error":"channel_not_found"}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient

	if _, err := client.SendText(context.Background(), "C1", "", "hi", "", nil, false); err == nil {
		t.Fatal("expected error when ok=false")
	}
}

func TestClientSendText_ThreadReply(t *testing.T) {
	var body string
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return jsonResp(`{"ok":true,"ts":"123.456","channel":"C1"}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient

	if _, err := client.SendText(context.Background(), "C1", "111.222", "hi", "", nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"thread_ts":"111.222"`) {
		t.Fatalf("expected thread_ts in payload, got %s", body)
	}
}

func TestClientConversationInfo(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/conversations.info" || req.URL.Query().Get("channel") != "C1" {
			t.Fatalf("unexpected request: %s %s", req.URL.Path, req.URL.RawQuery)
		}
		return jsonResp(`{"ok":true,"channel":{"id":"C1","name":"general","name_normalized":"general","is_channel":true}}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient
	info, err := client.ConversationInfo(context.Background(), "C1")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "C1" || info.Name != "general" || !info.IsChannel {
		t.Fatalf("info=%+v", info)
	}
}

func TestClientUserInfo(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/users.info" || req.URL.Query().Get("user") != "U1" {
			t.Fatalf("unexpected request: %s %s", req.URL.Path, req.URL.RawQuery)
		}
		return jsonResp(`{"ok":true,"user":{"id":"U1","team_id":"T1","name":"alice","real_name":"Alice Zhang","profile":{"display_name":"Alice","image_72":"https://example.com/alice.png"}}}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient
	info, err := client.UserInfo(context.Background(), "U1")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "U1" || info.DisplayName != "Alice" || info.AvatarURL != "https://example.com/alice.png" {
		t.Fatalf("info=%+v", info)
	}
}

func TestClientAddReaction(t *testing.T) {
	var body string
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/reactions.add" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return jsonResp(`{"ok":true}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient
	if err := client.AddReaction(context.Background(), "C1", "123.456", ":eyes:"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"channel":"C1"`) || !strings.Contains(body, `"timestamp":"123.456"`) || !strings.Contains(body, `"name":"eyes"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestClientAddReaction_AlreadyReactedIsIdempotent(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResp(`{"ok":false,"error":"already_reacted"}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient
	if err := client.AddReaction(context.Background(), "C1", "123.456", "eyes"); err != nil {
		t.Fatalf("already_reacted must be idempotent, got %v", err)
	}
}

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	now := time.Now().UTC()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":"event_callback"}`)
	if err := VerifyWebhookSignature("secret", ts, sign("secret", ts, body), body, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifyWebhookSignature_Tampered(t *testing.T) {
	now := time.Now().UTC()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":"event_callback"}`)
	if err := VerifyWebhookSignature("secret", ts, sign("secret", ts, []byte("different")), body, now); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestVerifyWebhookSignature_Expired(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute)
	ts := strconv.FormatInt(old.Unix(), 10)
	body := []byte(`{"type":"event_callback"}`)
	if err := VerifyWebhookSignature("secret", ts, sign("secret", ts, body), body, now); err == nil {
		t.Fatal("expected expired error")
	}
}
