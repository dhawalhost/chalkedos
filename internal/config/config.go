// Package config loads Chalked OS's runtime configuration from environment
// variables. Struct tags are read by github.com/caarlos0/env, avoiding the
// need for a separate config-parsing framework.
package config

import (
	"fmt"

	"github.com/caarlos0/env/v10"
)

// Config holds every environment-driven setting the API service needs.
type Config struct {
	// Environment is one of "local", "staging", "production".
	Environment string `env:"ENVIRONMENT" envDefault:"local"`

	// Port is the HTTP port the server listens on.
	Port string `env:"PORT" envDefault:"8080"`

	// DatabaseURL is the Postgres connection string (Supabase-managed).
	DatabaseURL string `env:"DATABASE_URL,required"`

	// SupabaseJWKSURL is the JWKS endpoint used to verify JWTs issued by
	// Supabase Auth (asymmetric signing keys, e.g. ES256) — current
	// Supabase projects publish this instead of a static HS256 secret.
	SupabaseJWKSURL string `env:"SUPABASE_JWKS_URL,required"`

	// SupabaseURL is the project's base URL (https://<ref>.supabase.co),
	// used to reach Supabase Auth's REST endpoints for phone OTP.
	SupabaseURL string `env:"SUPABASE_URL,required"`

	// SupabasePublishableKey is the client-safe API key sent as the
	// `apikey` header on Supabase Auth requests. Publishable by design —
	// RLS and Auth are the security boundary, not this key.
	SupabasePublishableKey string `env:"SUPABASE_PUBLISHABLE_KEY,required"`

	// AnthropicAPIKey authenticates calls to the Claude API. Optional
	// until the AI endpoints ship: internal/http/ai.go must refuse (503,
	// not crash) when unset — check AIEnabled before calling Generate.
	AnthropicAPIKey string `env:"ANTHROPIC_API_KEY"`

	// WATIAPIKey and WATIBaseURL authenticate outbound WhatsApp sends.
	// Optional: WhatsApp integration is disabled entirely when unset —
	// see WATIEnabled.
	WATIAPIKey  string `env:"WATI_API_KEY"`
	WATIBaseURL string `env:"WATI_BASE_URL"`

	// PTMTokenSecret signs the public PTM booking tokens parents receive
	// over WhatsApp (F-07: signed token, not JWT — parents never log in).
	// Optional: without it, the public PTM endpoints return 503.
	PTMTokenSecret string `env:"PTM_TOKEN_SECRET"`

	// WATIWebhookSecret verifies inbound WATI webhook signatures (HMAC),
	// per the API Specification's webhook auth model. Optional: without
	// it the webhook endpoint rejects (and logs) everything, since no
	// signature can ever verify against an empty secret.
	WATIWebhookSecret string `env:"WATI_WEBHOOK_SECRET"`
}

// AIEnabled reports whether the Claude API integration is configured.
func (c *Config) AIEnabled() bool {
	return c.AnthropicAPIKey != ""
}

// WATIEnabled reports whether the WhatsApp (WATI) integration is
// configured. Handlers that would send messages must no-op (and say so in
// their response where the API contract allows) when this is false.
func (c *Config) WATIEnabled() bool {
	return c.WATIAPIKey != "" && c.WATIBaseURL != ""
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
