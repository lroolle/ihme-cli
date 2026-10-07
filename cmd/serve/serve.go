// Package serve runs the read-only web inbox for shared addresses.
package serve

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/inbox"
	"github.com/lroolle/ihme-cli/internal/shares"
	"github.com/spf13/cobra"
)

const defaultIMAPServer = "imap.mail.me.com:993"

func NewCmdServe() *cobra.Command {
	var (
		listen  string
		days    int
		limit   int
		account string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve a plain web inbox for the addresses you share",
		Long: `Serve a read-only web page per shared address: the mail it received,
as plain text. Visitors open an address with the key 'ihme share'
printed for it; nothing else in the mailbox is reachable.

Mail is read over IMAP with an app-specific password, never with
your iCloud session, so this server can read mail and do nothing
else. Create one at account.apple.com > Sign-In and Security >
App-Specific Passwords.

The login comes from, in order:

  --account <email>    that account in em's accounts file
                       (~/.config/em/accounts.json)
  IHME_IMAP_USER       your @icloud.com address (not an Apple ID
                       that is a non-iCloud email)
  IHME_IMAP_PASSWORD   the app-specific password
  IHME_IMAP_SERVER     host:port, implicit TLS (default ` + defaultIMAPServer + `)
                       point it at the forward-to mailbox if your
                       addresses forward somewhere other than iCloud
  em's accounts file   when it holds exactly one account; with
                       several, name one with --account

Pages load nothing remote: no scripts, no images, a CSP of
default-src 'none'. Serve it publicly only behind HTTPS (a reverse
proxy such as Caddy); then set IHME_SERVE_URL=https://... for both
'ihme share' (working links) and 'ihme serve' (the key cookie is
then always Secure, even if the proxy omits X-Forwarded-Proto).`,
		Example: `  ihme serve                      # the one account em has
  ihme serve --account me@icloud.com
  IHME_IMAP_USER=me@icloud.com IHME_IMAP_PASSWORD=abcd-efgh-ijkl-mnop ihme serve
  ihme serve --listen 0.0.0.0:8025 --days 14`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if days < 1 || limit < 1 {
				return fmt.Errorf("--days and --limit must be at least 1")
			}

			sharesPath := shares.DefaultPath(api.DefaultSessionPath())
			list, err := shares.Load(sharesPath)
			if err != nil {
				return err
			}

			box, err := resolveMailbox(account)
			if err != nil {
				return err
			}
			mail := &inbox.IMAP{Dial: inbox.DialTLS(box.Server, box.User, box.Password)}
			if err := mail.Check(); err != nil {
				// Only a server that answered "no" is a credential
				// problem; DNS, TLS, and timeouts say so themselves.
				var refused *imap.Error
				if !errors.As(err, &refused) {
					return err
				}
				if box.From == envSource {
					return fmt.Errorf("%w\n\n  Fix: IHME_IMAP_USER must be the @icloud.com address and\n"+
						"  IHME_IMAP_PASSWORD an app-specific password, not your Apple ID password", err)
				}
				return fmt.Errorf("%w\n\n  The password em saved for %s was refused.\n"+
					"  If you revoked that app-specific password, save a new one: em login icloud --email %s", err, box.User, box.User)
			}

			logger := log.New(os.Stderr, "", log.LstdFlags)
			site := &inbox.Server{
				SharesPath: sharesPath, Mail: mail, Days: days, Limit: limit, Logf: logger.Printf,
				Secure: strings.HasPrefix(strings.ToLower(os.Getenv("IHME_SERVE_URL")), "https://"),
			}
			srv := &http.Server{
				Addr:              listen,
				Handler:           site.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      90 * time.Second,
				IdleTimeout:       2 * time.Minute,
			}
			ln, err := net.Listen("tcp", listen)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Serving at http://%s\n", ln.Addr())
			fmt.Fprintf(os.Stderr, "Mail: %s on %s (login from %s), INBOX and Junk, last %d days\n", box.User, box.Server, box.From, days)
			switch n := len(list); n {
			case 0:
				fmt.Fprintln(os.Stderr, "No addresses shared yet. Share one with: ihme share <ref>")
			case 1:
				fmt.Fprintln(os.Stderr, "1 address shared. Manage with: ihme share list | ihme share revoke <ref>")
			default:
				fmt.Fprintf(os.Stderr, "%d addresses shared. Manage with: ihme share list | ihme share revoke <ref>\n", n)
			}
			if host, _, _ := net.SplitHostPort(listen); !isLoopback(host) {
				fmt.Fprintln(os.Stderr, "Warning: listening beyond this machine over plain HTTP. Put HTTPS in front before sharing links.")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			errc := make(chan error, 1)
			go func() { errc <- srv.Serve(ln) }()
			select {
			case err := <-errc:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				fmt.Fprintln(os.Stderr, "Stopping.")
				if err := srv.Shutdown(shutdown); errors.Is(err, context.DeadlineExceeded) {
					return srv.Close() // a page still waiting on the mailbox; drop it
				} else if err != nil {
					return err
				}
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8025", "Address to listen on")
	cmd.Flags().IntVar(&days, "days", 30, "Show mail received in the last N days")
	cmd.Flags().IntVar(&limit, "limit", 50, "Show at most N messages per address")
	cmd.Flags().StringVar(&account, "account", "", "Read mail as this account from em's accounts file")
	return cmd
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
