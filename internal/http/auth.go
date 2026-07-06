package http

import (
	"net/http"

	"github.com/dhawalhost/chalkedos/internal/config"
)

// requestOTP implements POST /api/auth/otp/request — see API
// Specification, Section 02. This is a thin proxy to Supabase Auth's own
// OTP endpoint in production; stubbed here pending that wiring.
func requestOTP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO(chalked): forward to Supabase Auth's phone-OTP endpoint.
		// Rate limit: 5 requests / 15 min per phone + per IP, per the
		// API Specification's rate-limiting table.
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "OTP request not yet wired to Supabase Auth.")
	}
}

// verifyOTP implements POST /api/auth/otp/verify.
func verifyOTP(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO(chalked): verify the OTP via Supabase Auth, then look up
		// the matching user_profiles row to return school + role.
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "OTP verification not yet wired to Supabase Auth.")
	}
}
