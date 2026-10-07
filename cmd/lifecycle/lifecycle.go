package lifecycle

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

func NewCmdDeactivate() *cobra.Command {
	return &cobra.Command{
		Use:   "deactivate [ref]",
		Short: "Deactivate a Hide My Email address",
		Long: `Deactivate a Hide My Email address. Mail to this address will be rejected.
Can be reactivated later with 'ihme reactivate'.

Without <ref> at a terminal, asks which active address, newest first;
Enter takes the one created most recently.

JSON output (--json):
  {"status":"deactivated","hme":"...","id":"...","hints":{"reactivate":"...","delete":"..."}}`,
		Example: "  ihme deactivate github.com\n  ihme deactivate          # pick, newest first",
		Args:    cmdutil.RefOrPick("ihme deactivate <ref>", "ihme deactivate github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "deactivate",
				Want: func(e api.HmeEmail) bool { return e.IsActive },
				None: "no active addresses to deactivate",
			})
			if err != nil {
				return err
			}

			hme, changed, err := app.New(client).Deactivate(ref)
			if err != nil {
				return err
			}
			if !changed {
				fmt.Printf("%s is already inactive\n", hme.Hme)
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return cmdutil.OutputResult(cmd, map[string]any{
					"status": "deactivated",
					"hme":    hme.Hme,
					"id":     hme.AnonymousID,
					"hints": map[string]string{
						"reactivate": fmt.Sprintf("ihme reactivate %s", hme.AnonymousID),
						"delete":     fmt.Sprintf("ihme delete %s --yes", hme.AnonymousID),
					},
				})
			}
			fmt.Printf("Deactivated %s\n", hme.Hme)
			return nil
		},
	}
}

func NewCmdReactivate() *cobra.Command {
	return &cobra.Command{
		Use:   "reactivate [ref]",
		Short: "Reactivate a deactivated Hide My Email address",
		Long: `Reactivate a previously deactivated Hide My Email address.

Without <ref> at a terminal, asks which inactive address, newest first.

JSON output (--json):
  {"status":"reactivated","hme":"...","id":"...","hint":"ihme view <id> --json"}`,
		Example: "  ihme reactivate github.com\n  ihme reactivate          # pick, newest first",
		Args:    cmdutil.RefOrPick("ihme reactivate <ref>", "ihme reactivate github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "reactivate",
				Want: func(e api.HmeEmail) bool { return !e.IsActive },
				None: "no inactive addresses to reactivate",
			})
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(ref, result.HmeEmails)
			if err != nil {
				return err
			}

			if hme.IsActive {
				fmt.Printf("%s is already active\n", hme.Hme)
				return nil
			}

			if err := client.ReactivateHme(hme.AnonymousID); err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return cmdutil.OutputResult(cmd, map[string]any{
					"status": "reactivated",
					"hme":    hme.Hme,
					"id":     hme.AnonymousID,
					"hint":   fmt.Sprintf("ihme view %s --json", hme.AnonymousID),
				})
			}
			fmt.Printf("Reactivated %s\n", hme.Hme)
			return nil
		},
	}
}

func NewCmdDelete() *cobra.Command {
	var force, yes bool

	cmd := &cobra.Command{
		Use:   "delete [ref]",
		Short: "Permanently delete a Hide My Email address",
		Long: `Permanently delete a Hide My Email address. Apple only deletes
inactive addresses: deactivate first, or pass --force to do both.

Without <ref> at a terminal, asks which inactive address (any address
with --force), newest first. A picked address is always confirmed,
even with --yes. Without a terminal, --yes is required.

JSON output (--json):
  {"status":"deleted","hme":"...","id":"..."}`,
		Example: "  ihme delete github.com\n  ihme delete github.com --force --yes --json\n  ihme delete              # pick, newest first",
		Args:    cmdutil.RefOrPick("ihme delete <ref>", "ihme delete github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}
			none := "no inactive addresses to delete — deactivate one first, or pass --force"
			ref, err := cmdutil.RefFromArgs(args, client, cmdutil.Pick{
				Verb: "delete",
				Want: func(e api.HmeEmail) bool { return force || !e.IsActive },
				None: none,
			})
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(ref, result.HmeEmails)
			if err != nil {
				return err
			}

			if hme.IsActive && !force {
				return fmt.Errorf("%s is still active — deactivate first or use --force", hme.Hme)
			}

			// A picked address is one Enter away from the default, so
			// --yes never answers for it. Where nobody can answer,
			// refuse instead of reading EOF as a quiet "no".
			if picked := len(args) == 0; !yes || picked {
				if !cmdutil.CanPrompt(cmd) {
					return fmt.Errorf("refusing to delete %s without confirmation — pass --yes", hme.Hme)
				}
				fmt.Fprintf(os.Stderr, "Delete %s (%s)? This cannot be undone. [y/N] ", hme.Hme, hme.Label)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(line)) != "y" {
					return cmdutil.ErrCancelled
				}
			}

			if hme.IsActive {
				if err := client.DeactivateHme(hme.AnonymousID); err != nil {
					return fmt.Errorf("deactivating before delete: %w", err)
				}
			}

			if err := client.DeleteHme(hme.AnonymousID); err != nil {
				return err
			}

			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				return cmdutil.OutputResult(cmd, map[string]any{
					"status": "deleted",
					"hme":    hme.Hme,
					"id":     hme.AnonymousID,
				})
			}
			fmt.Printf("Deleted %s\n", hme.Hme)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Deactivate and delete in one step")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
