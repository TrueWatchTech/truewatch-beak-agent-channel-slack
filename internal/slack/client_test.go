package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
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

func TestClientUploadFileAndTrustedDownloadURL(t *testing.T) {
	path := t.TempDir() + "/image.png"
	if err := os.WriteFile(path, []byte("image-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient("https://slack.test/api", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/files.uploadV2":
			if req.Header.Get("Authorization") != "Bearer xoxb-test" {
				t.Fatalf("authorization=%q", req.Header.Get("Authorization"))
			}
			if err := req.ParseMultipartForm(1024 * 1024); err != nil {
				t.Fatal(err)
			}
			file, header, err := req.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			if header.Filename != "report.png" || req.FormValue("filename") != "report.png" {
				t.Fatalf("uploaded filename=%q field=%q", header.Filename, req.FormValue("filename"))
			}
			return jsonResp(`{"ok":true,"files":[{"id":"F123"}]}`)
		case "/private/file":
			if req.Header.Get("Authorization") != "Bearer xoxb-test" {
				t.Fatalf("authorization=%q", req.Header.Get("Authorization"))
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("downloaded"))}, nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})}
	fileID, err := client.UploadFile(context.Background(), path, "C1", "", "report.png")
	if err != nil || fileID != "F123" {
		t.Fatalf("fileID=%q err=%v", fileID, err)
	}
	downloadPath, cleanup, err := client.DownloadFile(context.Background(), "https://files.slack.com/private/file")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(downloadPath)
	if err != nil || string(data) != "downloaded" {
		t.Fatalf("path=%q data=%q err=%v", downloadPath, data, err)
	}
	cleanup()
	if _, _, err := client.DownloadFile(context.Background(), "https://evil.example/file"); err == nil {
		t.Fatal("untrusted host should be rejected")
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

func TestClientThreadParent(t *testing.T) {
	httpClient := &http.Client{Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/conversations.replies" ||
			req.URL.Query().Get("channel") != "C1" ||
			req.URL.Query().Get("ts") != "111.222" ||
			req.URL.Query().Get("oldest") != "111.222" ||
			req.URL.Query().Get("inclusive") != "true" ||
			req.URL.Query().Get("limit") != "1" {
			t.Fatalf("unexpected request: %s %s", req.URL.Path, req.URL.RawQuery)
		}
		return jsonResp(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","thread_ts":"111.222","ts":"111.222"}]}`)
	})}
	client := NewClient("", map[string]string{"bot_token": "xoxb-test"})
	client.HTTPClient = httpClient
	msg, err := client.ThreadParent(context.Background(), "C1", "111.222")
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || msg.TS != "111.222" || msg.User != "U1" || msg.Text != "parent" {
		t.Fatalf("message=%+v", msg)
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
