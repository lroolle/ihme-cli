// Package memorycmd exposes the embedded agent's memory graph:
// `ihme memory` shows where it lives and what it holds, with
// subcommands to search it and view its link graph. The files are
// plain Logseq-style markdown — open the directory in Logseq,
// Obsidian, or any editor for the full experience.
package memorycmd

import (
	"fmt"
	"os"

	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/spf13/cobra"
)

func NewCmdMemory() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect the agent's memory (journals, pages, flashcards)",
		Long: `The embedded agent keeps a memory across runs: a plain markdown
graph it finds on its own (override with $IHME_MEMORY_PATH).

  journals/    one dated file per day — what the agent did
  pages/       one file per topic — a service's history, or
               flashcards.md, the notes loaded into every run

The layout is Logseq's, so the directory opens directly in Logseq
or Obsidian. This command reports and searches it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st := memory.Open().Stats()
			if jsonOut(cmd) {
				return cmdutil.OutputResult(cmd, st)
			}
			fmt.Printf("memory: %s\n", st.Root)
			fmt.Printf("  %d journal day(s), %d page(s), %d flashcard(s)\n", st.Journals, st.Pages, st.Flashcards)
			if st.Journals == 0 && st.Pages == 0 {
				fmt.Println("  (empty — it fills as the agent reserves and learns)")
			}
			return nil
		},
	}
	cmd.AddCommand(cmdPath(), cmdSearch(), cmdGraph(), cmdCard())
	return cmd
}

func cmdPath() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the memory directory (for cd, backup, or opening in Logseq)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(memory.Open().Root())
			return nil
		},
	}
}

func cmdSearch() *cobra.Command {
	return &cobra.Command{
		Use:     "search <query>",
		Short:   "Search journals and pages for a service, address, or term",
		Args:    cobra.MinimumNArgs(1),
		Example: "  ihme memory search github\n  ihme memory search calm_mule",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			hits := memory.Open().Search(query, 50)
			if jsonOut(cmd) {
				return cmdutil.OutputResult(cmd, map[string]any{"hits": hits, "count": len(hits)})
			}
			if len(hits) == 0 {
				fmt.Fprintf(os.Stderr, "no memory of %q\n", query)
				return nil
			}
			for _, h := range hits {
				fmt.Printf("%-28s  %s\n", h.Source, h.Text)
			}
			return nil
		},
	}
}

func cmdGraph() *cobra.Command {
	return &cobra.Command{
		Use:   "graph",
		Short: "Show the link graph: topic pages and how often each is referenced",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes := memory.Open().Graph()
			if jsonOut(cmd) {
				return cmdutil.OutputResult(cmd, map[string]any{"nodes": nodes, "count": len(nodes)})
			}
			if len(nodes) == 0 {
				fmt.Fprintln(os.Stderr, "graph is empty")
				return nil
			}
			for _, n := range nodes {
				fmt.Printf("%-24s  %d backlink(s)", n.Title, n.Backlinks)
				if len(n.Links) > 0 {
					fmt.Printf("  →  %v", n.Links)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

func cmdCard() *cobra.Command {
	return &cobra.Command{
		Use:     "card <note>",
		Short:   "Pin a note into the flashcards page (loaded into every agent run)",
		Args:    cobra.MinimumNArgs(1),
		Example: "  ihme memory card \"prefer short, image-forward addresses\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			note := args[0]
			if err := memory.Open().PageAppend(memory.FlashcardsPage, note); err != nil {
				return err
			}
			fmt.Printf("pinned to flashcards: %s\n", note)
			return nil
		},
	}
}

func jsonOut(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}
