package api

import (
	"errors"
	"fmt"
	"testing"
)

func TestAuthRejectionClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"401", &HTTPError{Status: 401, URL: "u"}, true},
		{"403", &HTTPError{Status: 403, URL: "u"}, true},
		{"450", &HTTPError{Status: 450, URL: "u"}, true},
		{"421 is transient, not expiry", &HTTPError{Status: 421, URL: "u", Body: `{"success":false,"error":1}`}, false},
		{"502 proxy flap", &HTTPError{Status: 502, URL: "u"}, false},
		{"500", &HTTPError{Status: 500, URL: "u"}, false},
		{"timeout", fmt.Errorf("request to u: context deadline exceeded"), false},
		{"success=false sentinel", fmt.Errorf("validate session: %w", ErrSessionInvalid), true},
		{"wrapped 401", fmt.Errorf("outer: %w", &HTTPError{Status: 401}), true},
	}
	for _, tc := range cases {
		if got := isAuthRejection(tc.err); got != tc.want {
			t.Fatalf("%s: isAuthRejection = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTransientErrorUnwraps(t *testing.T) {
	inner := &HTTPError{Status: 502, URL: "u"}
	err := &TransientError{Err: fmt.Errorf("validating session: %w", inner)}
	if !IsTransient(err) {
		t.Fatal("IsTransient failed")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 502 {
		t.Fatal("inner HTTPError lost")
	}
}
