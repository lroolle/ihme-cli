package lifecycle

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/resolver"
	"github.com/spf13/cobra"
)

func NewCmdDeactivate() *cobra.Command {
	return &cobra.Command{
		Use:     "deactivate <ref>",
		Short:   "Deactivate a Hide My Email address",
		Example: "  ihme deactivate github.com",
		Args:    cmdutil.ExactRefArg("ihme deactivate <ref>", "ihme deactivate github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(args[0], result.HmeEmails)
			if err != nil {
				return err
			}

			if !hme.IsActive {
				fmt.Printf("%s is already inactive\n", hme.Hme)
				return nil
			}

			if err := client.DeactivateHme(hme.AnonymousID); err != nil {
				return err
			}

			fmt.Printf("Deactivated %s\n", hme.Hme)
			return nil
		},
	}
}

func NewCmdReactivate() *cobra.Command {
	return &cobra.Command{
		Use:     "reactivate <ref>",
		Short:   "Reactivate a deactivated Hide My Email address",
		Example: "  ihme reactivate github.com",
		Args:    cmdutil.ExactRefArg("ihme reactivate <ref>", "ihme reactivate github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(args[0], result.HmeEmails)
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

			fmt.Printf("Reactivated %s\n", hme.Hme)
			return nil
		},
	}
}

func NewCmdDelete() *cobra.Command {
	var force, yes bool

	cmd := &cobra.Command{
		Use:     "delete <ref>",
		Short:   "Permanently delete a Hide My Email address",
		Example: "  ihme delete github.com\n  ihme delete github.com --force --yes",
		Args:    cmdutil.ExactRefArg("ihme delete <ref>", "ihme delete github.com"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			hme, err := resolver.Resolve(args[0], result.HmeEmails)
			if err != nil {
				return err
			}

			if hme.IsActive && !force {
				return fmt.Errorf("%s is still active — deactivate first or use --force", hme.Hme)
			}

			if !yes {
				fmt.Fprintf(os.Stderr, "Delete %s (%s)? This cannot be undone. [y/N] ", hme.Hme, hme.Label)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(line)) != "y" {
					fmt.Println("Cancelled.")
					return nil
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

			fmt.Printf("Deleted %s\n", hme.Hme)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Deactivate and delete in one step")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
