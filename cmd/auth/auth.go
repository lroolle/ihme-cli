package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/lroolle/ihme-cli/api"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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

Credentials are never stored. Session tokens are saved to:
  ~/.config/ihme/session.json (respects $XDG_CONFIG_HOME)
Override with IHME_SESSION_PATH.

Trust token (~30 days) allows subsequent logins without 2FA.`,
		Example: `  ihme auth login
  ihme auth login --apple-id user@icloud.com
  IHME_APPLE_ID=user@icloud.com ihme auth login`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessPath := api.DefaultSessionPath()

			if appleID == "" {
				appleID = os.Getenv("IHME_APPLE_ID")
			}
			if appleID == "" {
				fmt.Print("Apple ID: ")
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading Apple ID: %w", err)
				}
				appleID = strings.TrimSpace(line)
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

			sess, err := api.LoadSession(sessPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load existing session: %s\n", err)
			}
			if sess != nil && sess.TrustToken != "" {
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

			fmt.Println("Authenticating...")
			if err := client.Login(appleID, password, otpCallback); err != nil {
				// %v, not %w, on purpose: the final accountLogin can
				// fail as an auth rejection, and cmdutil.Explain would
				// answer it with "Fix: ihme auth login" — the command
				// that is running. A failed sign-in gets the cause and
				// no circular advice.
				return fmt.Errorf("sign-in failed: %v", err)
			}

			if err := api.SaveSession(sessPath, client.Session()); err != nil {
				return fmt.Errorf("saving session: %w", err)
			}

			fmt.Println("Logged in successfully.")
			return nil
		},
	}

	cmd.Flags().StringVar(&appleID, "apple-id", "", "Apple ID email (or set IHME_APPLE_ID)")
	return cmd
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
