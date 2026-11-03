package domain

import "errors"

var (
	ErrNotFound       = errors.New("document not found")
	ErrUnavailable    = errors.New("search service unavailable")
	ErrInvalidQuery   = errors.New("search query invalid")
	ErrReasonRequired = errors.New("admin search reason required")
	ErrOrgSuspended   = errors.New("organization suspended")
	ErrVersionOlder   = errors.New("version is older than indexed document")
)
