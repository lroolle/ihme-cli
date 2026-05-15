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
  ~/Library/Application Support/ihme/session.json (macOS)
  ~/.config/ihme/session.json (Linux)
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
				return fmt.Errorf("Apple ID required")
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
				return err
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
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth status",
		Long: `Show current authentication status.

JSON output (--json):
  {"loggedIn":true,"appleId":"...","savedAt":"...","expired":false,"hint":"ihme list --json"}`,
		Example: `  ihme auth status
  ihme auth status --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessPath := api.DefaultSessionPath()
			sess, err := api.LoadSession(sessPath)
			if err != nil {
				return err
			}
			if sess == nil {
				fmt.Println("Not logged in.")
				os.Exit(2)
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				out := map[string]any{
					"loggedIn": true,
					"appleId":  sess.AppleID,
					"savedAt":  sess.SavedAt.Format("2006-01-02T15:04:05Z07:00"),
					"expired":  sess.IsExpired(),
					"hint":     "ihme list --json",
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Printf("Logged in as %s\n", sess.AppleID)
			fmt.Printf("Session saved: %s\n", sess.SavedAt.Format("2006-01-02 15:04"))
			if sess.IsExpired() {
				fmt.Println("Session may be expired — run 'ihme auth login' to refresh.")
			}
			return nil
		},
	}
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
