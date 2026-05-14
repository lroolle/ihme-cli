package cmdutil

import (
	"fmt"
	"os"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/output"
	"github.com/spf13/cobra"
)

func GetClient(cmd *cobra.Command) (*api.Client, error) {
	sessPath := api.DefaultSessionPath()
	sess, err := api.LoadSession(sessPath)
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}
	if sess == nil {
		return nil, fmt.Errorf("not logged in — run 'ihme auth login' first")
	}

	client, err := api.NewClientWithSession(sess)
	if err != nil {
		return nil, err
	}

	if err := client.ValidateSession(); err != nil {
		return nil, fmt.Errorf("session expired — run 'ihme auth login' to re-authenticate: %w", err)
	}

	return client, nil
}

func OutputResult(cmd *cobra.Command, v any) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	jqExpr, _ := cmd.Flags().GetString("jq")

	if jqExpr != "" {
		return output.PrintJQ(os.Stdout, v, jqExpr)
	}
	if jsonFlag {
		return output.PrintJSON(os.Stdout, v)
	}

	switch data := v.(type) {
	case []api.HmeEmail:
		output.PrintTable(os.Stdout, data)
	case *api.HmeEmail:
		output.PrintDetail(os.Stdout, data)
	default:
		return output.PrintJSON(os.Stdout, v)
	}
	return nil
}

func CheckErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
