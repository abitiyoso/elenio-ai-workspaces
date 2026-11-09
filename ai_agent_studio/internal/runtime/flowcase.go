package runtime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// FlowcaseDispatcher delivers run results to Flowcase's signed callback with
// X-Flowcase-Timestamp and X-Flowcase-Signature: v1=HMAC-SHA256(secret, ts + "." + body).
// CallbackURL (AGENT_STUDIO_FLOWCASE_CALLBACK_URL) is POSTed to verbatim; no
// path is appended, so it must be the full Flowcase endpoint including
// /api/v1/agent-callbacks/{provider}, for example
// http://127.0.0.1:8180/api/v1/agent-callbacks/agent-studio.
// The destination is pre-registered configuration, never caller-supplied.
type FlowcaseDispatcher struct {
	CallbackURL string
	Secret      []byte
	Client      *http.Client
	Now         func() time.Time
}

func (d *FlowcaseDispatcher) Configured() bool {
	return d != nil && d.CallbackURL != "" && len(d.Secret) >= 32
}

// SignFlowcase returns the signature header value for a body.
func SignFlowcase(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Deliver returns "" on acknowledgement or a stable error code otherwise.
// Flowcase dedupes by message_id, so redelivery after a lost ack is safe.
func (d *FlowcaseDispatcher) Deliver(ctx context.Context, body []byte) string {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	ts := strconv.FormatInt(now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return "flowcase_request_invalid"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Flowcase-Timestamp", ts)
	req.Header.Set("X-Flowcase-Signature", SignFlowcase(d.Secret, ts, body))
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "flowcase_unreachable"
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return ""
	case resp.StatusCode == http.StatusUnauthorized:
		return "flowcase_signature_rejected"
	case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
		return "flowcase_rejected"
	default:
		return "flowcase_unavailable"
	}
}
