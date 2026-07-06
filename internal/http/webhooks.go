package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/config"
)

// watiSignatureHeader is the header WATI is expected to send the
// HMAC-SHA256 signature (hex-encoded, over the raw request body) in.
//
// FLAG: this header name/format is an assumption, not a confirmed value —
// neither docs/api-reference.md nor docs/architecture.md name it, and
// WATI's own webhook docs should be checked before trusting this in
// production. If WATI signs differently (different header, base64 instead
// of hex, a timestamp included in the signed payload), this needs updating
// to match.
const watiSignatureHeader = "X-WATI-Signature"

type watiWebhookPayload struct {
	WatiMessageID string `json:"wati_message_id"`
	Status        string `json:"status"`
	Timestamp     string `json:"timestamp"`
	FailureReason string `json:"failure_reason,omitempty"`
}

// watiWebhook implements POST /api/webhooks/wati — see the API
// Specification, Section 09. Authenticated via an HMAC signature header
// (WATI-specific), not a JWT, since WATI is not a logged-in user.
//
// Per the API Specification's note: this always returns 200, even for an
// invalid signature or unrecognised message ID, to avoid WATI retrying
// indefinitely — both cases are logged as warnings, not surfaced as
// errors, and neither one applies any DB write.
func watiWebhook(cfg *config.Config, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Warn("wati webhook: could not read body", "error", err)
			writeJSON(w, http.StatusOK, map[string]bool{"acknowledged": true})
			return
		}

		if !verifyWATISignature(cfg.WATIWebhookSecret, body, r.Header.Get(watiSignatureHeader)) {
			slog.Warn("wati webhook: signature verification failed")
			writeJSON(w, http.StatusOK, map[string]bool{"acknowledged": true})
			return
		}

		var payload watiWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil || payload.WatiMessageID == "" {
			slog.Warn("wati webhook: could not parse payload", "error", err)
			writeJSON(w, http.StatusOK, map[string]bool{"acknowledged": true})
			return
		}

		var failureReason *string
		if payload.FailureReason != "" {
			failureReason = &payload.FailureReason
		}

		var matched bool
		err = pool.QueryRow(r.Context(),
			`SELECT update_whatsapp_message_status($1, $2, $3)`,
			payload.WatiMessageID, payload.Status, failureReason,
		).Scan(&matched)
		if err != nil {
			slog.Error("wati webhook: status update failed", "error", err, "wati_message_id", payload.WatiMessageID)
		} else if !matched {
			slog.Warn("wati webhook: no matching message", "wati_message_id", payload.WatiMessageID)
		}

		writeJSON(w, http.StatusOK, map[string]bool{"acknowledged": true})
	}
}

// verifyWATISignature checks an HMAC-SHA256 signature (hex-encoded) over
// the raw request body. Uses hmac.Equal for a constant-time comparison —
// a naive == would leak timing information about how many leading bytes
// matched, letting an attacker forge a valid signature byte-by-byte.
func verifyWATISignature(secret string, body []byte, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" {
		return false
	}
	sig, err := hex.DecodeString(signatureHeader)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(sig, expected)
}
