package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

var (
	ErrCredentialRejected = errors.New("slack credential rejected")
	ErrTransientFailure   = errors.New("slack transient platform failure")
)

type HTTPError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s failed: status=%d body=%s", e.Method, e.Path, e.StatusCode, e.Body)
}

func credentialRejected(message string) error {
	return fmt.Errorf("%w: %s", ErrCredentialRejected, message)
}

func transientFailure(message string) error {
	return fmt.Errorf("%w: %s", ErrTransientFailure, message)
}

func credentialResponseRejected(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_auth", "not_authed", "account_inactive", "token_expired", "token_revoked",
		"not_allowed_token_type", "access_denied", "no_permission", "missing_scope",
		"ekm_access_denied", "enterprise_is_restricted":
		return true
	default:
		return false
	}
}

func IsCredentialRejected(err error) bool {
	if errors.Is(err, ErrCredentialRejected) {
		return true
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
		return true
	}
	if httpErr.StatusCode != http.StatusBadRequest {
		return false
	}
	var payload struct {
		Error string `json:"error"`
	}
	return json.Unmarshal([]byte(httpErr.Body), &payload) == nil && credentialResponseRejected(payload.Error)
}

func IsRetryableError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, ErrTransientFailure) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusRequestTimeout || httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= http.StatusInternalServerError)
}
