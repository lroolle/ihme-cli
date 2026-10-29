package auth

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
)

func TestReadHiddenLine(t *testing.T) {
	long := strings.Repeat("x", 5000)
	cases := []struct {
		name, in, want string
		wantErr        error
	}{
		{"Enter ends the line", "a=b; c=d\rignored", "a=b; c=d", nil},
		{"longer than a terminal line", long + "\r", long, nil},
		{"bracketed paste", "\x1b[200~a=b\x1b[201~\r", "a=b", nil},
		{"backspace", "a=bx\x7f\r", "a=b", nil},
		{"multi-line JSON export is read whole", "[\r\n  {\"name\": \"a\",\r\n   \"value\": \"b\"}\r\n]\rnext", `[    {"name": "a",     "value": "b"}  ]`, nil},
		{"EOF after input", "a=b", "a=b", nil},
		{"Ctrl-C cancels", "a=\x03", "", cmdutil.ErrCancelled},
		{"Ctrl-D on empty line", "\x04", "", io.EOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readHiddenLine(strings.NewReader(tc.in))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
