package export

import (
	"os"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/pkg/filter"
	"github.com/lroolle/ihme-cli/pkg/output"
	"github.com/spf13/cobra"
)

func NewCmdExport() *cobra.Command {
	var (
		format  string
		outFile string
		opts    filter.Options
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export addresses to CSV or JSON",
		Example: `  ihme export
  ihme export --format json
  ihme export --active --tag dev -o dev.csv
  ihme export --format json -o all.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cmdutil.GetClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.ListHme()
			if err != nil {
				return err
			}

			emails := filter.Apply(result.HmeEmails, opts)

			w := os.Stdout
			if outFile != "" {
				f, err := os.Create(outFile)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}

			switch format {
			case "json":
				return output.PrintJSON(w, emails)
			default:
				return output.PrintCSV(w, emails)
			}
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "csv", "Output format: csv, json")
	cmd.Flags().StringVarP(&outFile, "output", "o", "", "Output file (default: stdout)")
	cmd.Flags().StringVar(&opts.Tag, "tag", "", "Filter by tag")
	cmd.Flags().StringVarP(&opts.Search, "search", "s", "", "Search label, address, or note")
	cmd.Flags().BoolVar(&opts.Active, "active", false, "Export only active")
	cmd.Flags().BoolVar(&opts.Inactive, "inactive", false, "Export only inactive")
	cmd.Flags().StringVar(&opts.Sort, "sort", "", "Sort by: date, label, date:asc, label:desc")
	return cmd
}
