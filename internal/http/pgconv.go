package http

import (
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// uuidString renders a pgtype.UUID in canonical 8-4-4-4-12 form.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	s := hex.EncodeToString(u.Bytes[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
