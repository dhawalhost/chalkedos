package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ptmTokenPayload is what a parent's PTM booking link carries: which
// school/student/guardian the token was issued for, and when it stops
// working. 7-day expiry per the API spec.
type ptmTokenPayload struct {
	SchoolID   string `json:"school_id"`
	StudentID  string `json:"student_id"`
	GuardianID string `json:"guardian_id"`
	ExpiresAt  int64  `json:"exp"`
}

const ptmTokenTTL = 7 * 24 * time.Hour

// signPTMToken produces "<base64url(payload)>.<base64url(hmac-sha256)>".
// Deliberately not a JWT: no algorithm agility to get wrong, nothing to
// configure, and the payload is ours alone.
func signPTMToken(secret string, payload ptmTokenPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling PTM token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// verifyPTMToken checks the signature (constant-time) and expiry, and
// returns the payload. All failures collapse into one generic error so
// the handler can't leak which part failed.
func verifyPTMToken(secret, token string) (*ptmTokenPayload, error) {
	errInvalid := fmt.Errorf("invalid or expired token")
	if secret == "" {
		return nil, errInvalid
	}
	encoded, sig, ok := strings.Cut(token, ".")
	if !ok {
		return nil, errInvalid
	}
	gotSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, errInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	if !hmac.Equal(gotSig, mac.Sum(nil)) {
		return nil, errInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errInvalid
	}
	var payload ptmTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errInvalid
	}
	if time.Now().Unix() > payload.ExpiresAt {
		return nil, errInvalid
	}
	return &payload, nil
}
