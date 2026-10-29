package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/securitykey"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// securityKeyWait bounds how long login waits for a security key to
// be plugged in and touched before giving up.
const securityKeyWait = 2 * time.Minute

func NewCmdAuth() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with iCloud",
		Example: `  ihme auth login
  ihme auth login --apple-id user@icloud.com
  ihme auth status --json
  ihme auth logout`,
	}
	cmd.AddCommand(newCmdLogin())
	cmd.AddCommand(newCmdStatus())
	cmd.AddCommand(newCmdLogout())
	return cmd
}

func newCmdLogin() *cobra.Command {
	var appleID string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with your Apple ID",
		Long: `Sign in with your Apple ID using SRP authentication + 2FA.

2FA is a six-digit code, or, if your Apple Account uses security
keys, a touch on a FIDO2 key plugged in over USB.

Credentials are never stored. Session tokens are saved to:
  ~/.config/ihme/session.json (respects $XDG_CONFIG_HOME)
Override with IHME_SESSION_PATH.

Trust token (~30 days) allows subsequent logins without 2FA.`,
		Example: `  ihme auth login
  ihme auth login --apple-id user@icloud.com
  IHME_APPLE_ID=user@icloud.com ihme auth login`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessPath := api.DefaultSessionPath()
			sess, err := api.LoadSession(sessPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load existing session: %s\n", err)
			}

			if appleID == "" {
				appleID = os.Getenv("IHME_APPLE_ID")
			}
			if appleID == "" {
				// Signing in again is the common case (the session
				// expired), so the last Apple ID is one Enter away.
				last := ""
				if sess != nil {
					last = sess.AppleID
				}
				if last != "" {
					fmt.Printf("Apple ID [%s]: ", last)
				} else {
					fmt.Print("Apple ID: ")
				}
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil && strings.TrimSpace(line) == "" {
					// EOF is not "use the last one".
					return fmt.Errorf("reading Apple ID: %w", err)
				}
				appleID = strings.TrimSpace(line)
				if appleID == "" {
					appleID = last
				}
			}
			if appleID == "" {
				return fmt.Errorf("missing Apple ID")
			}

			fmt.Print("Password: ")
			passBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("reading password: %w", err)
			}
			password := string(passBytes)

			client, err := api.NewClient()
			if err != nil {
				return err
			}
			client.Verbose, _ = cmd.Flags().GetBool("verbose")

			// A trust token vouches for one account only; it skips
			// 2FA for that account and means nothing for another.
			if sess != nil && sess.TrustToken != "" && strings.EqualFold(sess.AppleID, appleID) {
				client.Session().TrustToken = sess.TrustToken
			}

			otpCallback := func() (string, error) {
				fmt.Print("Two-factor code: ")
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil {
					return "", fmt.Errorf("reading 2FA code: %w", err)
				}
				code := strings.TrimSpace(line)
				if code == "" {
					return "", fmt.Errorf("verification code required")
				}
				return code, nil
			}

			keyCallback := func(challenge api.SecurityKeyChallenge) (*api.SecurityKeyAssertion, error) {
				if len(challenge.KeyNames) > 0 {
					fmt.Printf("This Apple Account uses security keys (%s).\n", strings.Join(challenge.KeyNames, ", "))
				}
				fmt.Println("Insert your security key and touch it when it flashes...")
				ctx, cancel := context.WithTimeout(cmd.Context(), securityKeyWait)
				defer cancel()
				return securitykey.Assert(ctx, challenge)
			}

			fmt.Println("Authenticating...")
			if err := client.Login(appleID, password, otpCallback, keyCallback); err != nil {
				// %v, not %w, on purpose: the final accountLogin can
				// fail as an auth rejection, and cmdutil.Explain would
				// answer it with "Fix: ihme auth login" — the command
				// that is running. A failed sign-in gets the cause and
				// no circular advice.
				return fmt.Errorf("sign-in failed: %v", err)
			}

			return confirmHme(client, sessPath, "Logged in successfully")
		},
	}

	cmd.Flags().StringVar(&appleID, "apple-id", "", "Apple ID email (or set IHME_APPLE_ID)")
	return cmd
}

// confirmHme saves the session, then lists the addresses once, so a
// login only reports success when Hide My Email answers, not just
// iCloud. A refusal is returned as the login's error; the session is
// still kept, since it is valid and its trust token spares the second
// factor on the sign-in the fix asks for. Anything else (no network,
// no iCloud+) is a warning on a good login.
func confirmHme(client *api.Client, sessPath, success string) error {
	if err := api.SaveSession(sessPath, client.Session()); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	list, err := client.ListHme()
	if errors.As(err, new(*api.ServiceRefusedError)) {
		return err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check Hide My Email: %v\n", err)
		fmt.Println(success + ".")
		return nil
	}
	fmt.Printf("%s (%d addresses).\n", success, len(list.HmeEmails))
	return nil
}

func newCmdStatus() *cobra.Command {
	var localOnly bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current auth status",
		Long: `Show current authentication status.

JSON output (--json):
  {"loggedIn":true,"appleId":"...","savedAt":"...","expired":false,"canAccessICloud":true,"hint":"ihme list --json"}`,
		Example: `  ihme auth status
  ihme auth status --json
  ihme auth status --local --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessPath := api.DefaultSessionPath()
			sess, err := api.LoadSession(sessPath)
			if err != nil {
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if sess == nil {
				if jsonFlag {
					out := map[string]any{
						"loggedIn":        false,
						"expired":         true,
						"canAccessICloud": false,
						"check":           "none",
						"hint":            "ihme auth login",
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					enc.SetEscapeHTML(false)
					_ = enc.Encode(out)
				} else {
					fmt.Println("Not logged in.")
				}
				os.Exit(2)
			}

			localExpired := sess.IsExpired()
			out := map[string]any{
				"loggedIn":        true,
				"appleId":         sess.AppleID,
				"expired":         localExpired,
				"localExpired":    localExpired,
				"canAccessICloud": false,
				"check":           "local",
				"hint":            "ihme auth status --json",
			}
			if !sess.SavedAt.IsZero() {
				out["savedAt"] = sess.SavedAt.Format("2006-01-02T15:04:05Z07:00")
			}

			if !localOnly {
				client, err := api.NewClientWithSession(sess)
				if err != nil {
					return err
				}
				client.Verbose, _ = cmd.Flags().GetBool("verbose")

				_, rawResponse, err := client.ValidateSessionInfo()
				if err != nil {
					out["loggedIn"] = false
					out["expired"] = true
					out["canAccessICloud"] = false
					out["check"] = "icloud"
					out["error"] = err.Error()
					out["hint"] = "ihme auth login"

					if jsonFlag {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						enc.SetEscapeHTML(false)
						_ = enc.Encode(out)
					} else {
						fmt.Printf("Stored session for %s\n", sess.AppleID)
						fmt.Printf("Session saved: %s\n", sess.SavedAt.Format("2006-01-02 15:04"))
						if localExpired {
							fmt.Println("Local session may be expired.")
						}
						fmt.Printf("iCloud access check failed: %s\n", err)
						fmt.Println("Run 'ihme auth login' to refresh.")
					}
					os.Exit(2)
				}

				if err := api.SaveSession(sessPath, client.Session()); err != nil {
					return fmt.Errorf("saving session: %w", err)
				}
				sess = client.Session()
				out["loggedIn"] = true
				out["expired"] = false
				out["canAccessICloud"] = true
				out["check"] = "icloud"
				out["hint"] = "ihme list --json"
				out["savedAt"] = sess.SavedAt.Format("2006-01-02T15:04:05Z07:00")
				if jsonFlag && len(rawResponse) > 0 {
					out["rawResponse"] = rawResponse
				}
			}

			if jsonFlag {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				return enc.Encode(out)
			}

			if !localOnly {
				fmt.Printf("Authenticated as %s\n", sess.AppleID)
			} else {
				fmt.Printf("Stored session for %s\n", sess.AppleID)
			}
			fmt.Printf("Session saved: %s\n", sess.SavedAt.Format("2006-01-02 15:04"))
			if localExpired && localOnly {
				fmt.Println("Session may be expired — run 'ihme auth login' to refresh.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&localOnly, "local", false, "Only inspect the local session file; skip live validation")
	return cmd
}

func newCmdLogout() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessPath := api.DefaultSessionPath()
			if err := api.DeleteSession(sessPath); err != nil {
				return err
			}
			fmt.Println("Logged out.")
			return nil
		},
	}
}
