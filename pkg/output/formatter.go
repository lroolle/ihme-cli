package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/tags"
)

func PrintTable(w io.Writer, emails []api.HmeEmail) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tLABEL\tADDRESS\tCREATED\tSTATUS\n")
	for _, e := range emails {
		status := "active"
		if !e.IsActive {
			status = "inactive"
		}
		created := time.UnixMilli(e.CreateTimestamp).Format("Jan 02")
		id := e.AnonymousID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", id, e.Label, e.Hme, created, status)
	}
	tw.Flush()
}

func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func PrintJQ(w io.Writer, v any, expr string) error {
	data, err := marshalRaw(v)
	if err != nil {
		return err
	}

	cmd := exec.Command("jq", expr)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func PrintCSV(w io.Writer, emails []api.HmeEmail) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	cw.Write([]string{"anonymousId", "label", "hme", "forwardTo", "isActive", "created", "tags", "note"})
	for _, e := range emails {
		parsed := tags.Parse(e.Note)
		cw.Write([]string{
			e.AnonymousID,
			e.Label,
			e.Hme,
			e.ForwardToEmail,
			fmt.Sprintf("%t", e.IsActive),
			time.UnixMilli(e.CreateTimestamp).Format(time.RFC3339),
			strings.Join(parsed.Tags, ","),
			parsed.Note,
		})
	}
	return cw.Error()
}

func marshalRaw(v any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

func PrintDetail(w io.Writer, e *api.HmeEmail) {
	parsed := tags.Parse(e.Note)
	status := "active"
	if !e.IsActive {
		status = "inactive"
	}

	fmt.Fprintf(w, "ID:         %s\n", e.AnonymousID)
	fmt.Fprintf(w, "Label:      %s\n", e.Label)
	fmt.Fprintf(w, "Address:    %s\n", e.Hme)
	fmt.Fprintf(w, "Forward to: %s\n", e.ForwardToEmail)
	fmt.Fprintf(w, "Status:     %s\n", status)
	fmt.Fprintf(w, "Created:    %s\n", e.CreatedAt().Format("2006-01-02 15:04"))
	if len(parsed.Tags) > 0 {
		fmt.Fprintf(w, "Tags:       %s\n", strings.Join(parsed.Tags, ", "))
	}
	if parsed.Note != "" {
		fmt.Fprintf(w, "Note:       %s\n", parsed.Note)
	}
}
