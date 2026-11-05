package domain

import "errors"

var (
	ErrNotFound       = errors.New("document not found")
	ErrUnavailable    = errors.New("search service unavailable")
	ErrInvalidQuery   = errors.New("search query invalid")
	ErrReasonRequired = errors.New("admin search reason required")
	ErrOrgSuspended        = errors.New("organization suspended")
	ErrVersionOlder        = errors.New("version is older than indexed document")
	ErrCohortTooLarge      = errors.New("cohort too large (limit is 1000 rows)")
	ErrCampaignRateLimited = errors.New("campaign rate limited (1 campaign per cohort every 7 days)")
)
