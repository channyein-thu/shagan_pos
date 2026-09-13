package common

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgUniqueViolation is Postgres's error code for a unique constraint violation.
const pgUniqueViolation = "23505"

// IsDuplicateError reports whether err is a Postgres unique constraint
// violation (e.g. inserting a barcode/phone/po_number that already exists).
func IsDuplicateError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}
	return false
}
