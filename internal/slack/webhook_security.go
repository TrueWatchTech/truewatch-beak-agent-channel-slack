package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const webhookFreshnessWindow = 5 * time.Minute

// VerifyWebhookSignature validates a Slack Events API request signature.
// Slack signs each request as:
//
//	X-Slack-Signature = "v0=" + hex(HMAC-SHA256(signing_secret, "v0:{timestamp}:{raw_body}"))
//
// timestamp is X-Slack-Request-Timestamp (Unix seconds). The base string is
// built over the raw request body bytes — read them before JSON parsing.
// Requests older than the freshness window are rejected (replay protection),
// and the comparison is constant-time.
func VerifyWebhookSignature(secret, timestamp, signature string, body []byte, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("slack signing secret is required")
	}
	if timestamp == "" || signature == "" {
		return fmt.Errorf("slack signature headers are required")
	}
	sentAt, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return fmt.Errorf("slack timestamp is invalid")
	}
	age := now.Sub(time.Unix(sentAt, 0))
	if age < 0 {
		age = -age
	}
	if age > webhookFreshnessWindow {
		return fmt.Errorf("slack timestamp is expired")
	}
	base := make([]byte, 0, len("v0:")+len(timestamp)+1+len(body))
	base = append(base, "v0:"...)
	base = append(base, timestamp...)
	base = append(base, ':')
	base = append(base, body...)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(base)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature))) {
		return fmt.Errorf("slack signature mismatch")
	}
	return nil
}
