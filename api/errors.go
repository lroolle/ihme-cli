package api

import (
	"errors"
	"fmt"
	"strings"
)

// HTTPError is a non-2xx response from an Apple endpoint, carrying
// the status so callers can distinguish auth rejections from
// transient trouble without parsing message text.
type HTTPError struct {
	Status int
	URL    string
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d from %s", e.Status, redactURL(e.URL))
	}
	return fmt.Sprintf("HTTP %d from %s: %s", e.Status, redactURL(e.URL), e.Body)
}

// redactURL keeps host and path, drops the query. Apple's service
// URLs carry dsid and clientId — account identifiers that add
// nothing to a diagnosis and should not land in a pasted error,
// a log, or an issue report.
func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// TransientError marks a failure that does NOT mean the session is
// invalid: timeouts, connection resets, 5xx, or Apple's 421 routing
// hiccups. Callers should suggest retrying, never re-login.
type TransientError struct {
	Err error
}

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

// IsTransient reports whether err is marked transient.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}

// ErrSessionInvalid marks a definitive rejection: Apple examined
// the session and said no. Only this class justifies re-login or
// the accountLogin fallback.
var ErrSessionInvalid = errors.New("session rejected by iCloud")

// IsAuthRejection reports whether err is a definitive auth
// rejection rather than transport/server trouble. Notably 421
// ("misdirected request") is NOT a rejection: Apple returns it for
// routing hiccups and rate pressure on sessions that are still
// valid — the CN-region case is already retried inside
// ValidateSessionInfo, and everything else deserves "try again",
// never "re-login".
func IsAuthRejection(err error) bool {
	if errors.Is(err, ErrSessionInvalid) {
		return true
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case 401, 403, 450:
			return true
		}
	}
	return false
}
