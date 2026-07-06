// Package supabase is a minimal client for the Supabase Auth REST API —
// only the phone-OTP endpoints Chalked OS uses. Deliberately stdlib-only:
// Supabase's official Go client would add a dependency for two POSTs.
package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AuthClient talks to a Supabase project's /auth/v1 endpoints.
type AuthClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewAuthClient(projectURL, publishableKey string) *AuthClient {
	return &AuthClient{
		baseURL: projectURL + "/auth/v1",
		apiKey:  publishableKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Session is the subset of Supabase Auth's token response the login flow
// needs.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         struct {
		ID    string `json:"id"`
		Phone string `json:"phone"`
	} `json:"user"`
}

// apiError mirrors Supabase Auth's error body. Newer versions use
// error_code/msg; older ones error/error_description — accept both.
type apiError struct {
	Code             string `json:"error_code"`
	Msg              string `json:"msg"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (e *apiError) message() string {
	for _, s := range []string{e.Msg, e.ErrorDescription, e.Error, e.Code} {
		if s != "" {
			return s
		}
	}
	return "unknown Supabase Auth error"
}

// AuthError is returned for 4xx responses from Supabase Auth — the caller
// maps these to client-facing 400s rather than 502s.
type AuthError struct {
	Status  int
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("supabase auth: %d: %s", e.Status, e.Message)
}

// RequestOTP asks Supabase Auth to send an SMS OTP to phone (E.164).
func (c *AuthClient) RequestOTP(ctx context.Context, phone string) error {
	return c.post(ctx, "/otp", map[string]any{"phone": phone}, nil)
}

// VerifyOTP exchanges a phone + SMS code for a session.
func (c *AuthClient) VerifyOTP(ctx context.Context, phone, token string) (*Session, error) {
	var session Session
	err := c.post(ctx, "/verify", map[string]any{
		"type":  "sms",
		"phone": phone,
		"token": token,
	}, &session)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (c *AuthClient) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling supabase auth: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading supabase auth response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr apiError
		_ = json.Unmarshal(respBody, &apiErr)
		return &AuthError{Status: resp.StatusCode, Message: apiErr.message()}
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decoding supabase auth response: %w", err)
		}
	}
	return nil
}
