// Package memory is the embedded agent's persistent memory: a plain
// file-based Logseq graph — journals/ for dated run records, pages/
// for topics, [[wikilinks]] between them — that the user can open in
// Logseq, Obsidian, or any editor. Two rules keep it honest (both
// borrowed from pi.dev's context-engineering doctrine): the files
// ARE the memory (no database, no embeddings, nothing to migrate),
// and code writes the facts while the model records only what code
// cannot know. The graph view is derived from links at read time,
// never stored — the Logseq way.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FlashcardsPage is the one special page: its bullets are injected
// into every run — the always-loaded keeper layer — so writes there
// must earn their permanent context cost.
const FlashcardsPage = "flashcards"

// Store reads and appends one memory graph rooted at a directory.
// Every write is append-only, and callers treat writes as
// best-effort: memory must never fail the task that feeds it.
//
// Writes are single-process safe (the agent's tool calls are
// sequential). Concurrent ihme processes appending to the same file
// can interleave a block larger than the platform's atomic-append
// size — the same concurrency limitation as the session file, and
// addressed by the same roadmapped lock.
type Store struct {
	root string
}

// Open resolves the memory's own place without configuration:
// $IHME_MEMORY_PATH, else $XDG_DATA_HOME/ihme/memory, else
// ~/.local/share/ihme/memory — the data-dir sibling of the session
// file's config-dir convention. Nothing is created until the first
// write.
func Open() *Store {
	if p := os.Getenv("IHME_MEMORY_PATH"); p != "" {
		return &Store{root: p}
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return &Store{root: filepath.Join(xdg, "ihme", "memory")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return &Store{}
	}
	return &Store{root: filepath.Join(home, ".local", "share", "ihme", "memory")}
}

// At opens a store rooted at an explicit directory (tests, tooling).
func At(root string) *Store { return &Store{root: root} }

func (s *Store) Root() string { return s.root }

// JournalAppend appends one outline block to today's journal file,
// named in the classic Logseq date format (journals/2006_01_02.md).
func (s *Store) JournalAppend(block string) error {
	name := time.Now().Format("2006_01_02") + ".md"
	return s.appendFile(filepath.Join("journals", name), block)
}

// PageAppend appends one bullet to a topic page, creating the page
// on first use. Titles keep their natural form inside [[links]];
// only the filename is sanitized.
func (s *Store) PageAppend(title, bullet string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("memory page title required")
	}
	bullet = strings.TrimSpace(bullet)
	if !strings.HasPrefix(bullet, "- ") {
		bullet = "- " + bullet
	}
	return s.appendFile(filepath.Join("pages", pageFile(title)), bullet)
}

// ReadPage returns a topic page's full content by title.
func (s *Store) ReadPage(title string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(s.root, "pages", pageFile(title)))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// RecentJournals returns the newest journal files, oldest first so
// the story reads forward, dropping whole older files once the
// budget is spent. Journals hold only durable turns, so whole files
// are the natural unit.
func (s *Store) RecentJournals(maxBytes int) string {
	names := s.list("journals")
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // date-named: lexical == chronological
	var parts []string
	total := 0
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(s.root, "journals", name))
		if err != nil {
			continue
		}
		if total > 0 && total+len(raw) > maxBytes {
			break
		}
		day := strings.TrimSuffix(name, ".md")
		parts = append(parts, "### "+day+"\n"+strings.TrimSpace(string(raw)))
		total += len(raw)
		if total >= maxBytes {
			break
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n")
}

// Hit is one matching line with its graph-relative source file.
type Hit struct {
	Source string `json:"source"`
	Text   string `json:"text"`
}

// Search scans every journal and page line for a case-insensitive
// substring. An exact page-title match returns that whole page
// first, so recalling a label surfaces its full story, not one line
// of it.
func (s *Store) Search(query string, limit int) []Hit {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit <= 0 {
		return nil
	}
	var hits []Hit
	exact := ""
	if page, ok := s.ReadPage(query); ok {
		exact = pageFile(query)
		hits = append(hits, Hit{Source: "pages/" + exact, Text: strings.TrimSpace(page)})
		if len(hits) >= limit {
			return hits
		}
	}
	for _, dir := range []string{"journals", "pages"} {
		for _, name := range s.list(dir) {
			if dir == "pages" && name == exact {
				continue // already returned whole
			}
			raw, err := os.ReadFile(filepath.Join(s.root, dir, name))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				if !strings.Contains(strings.ToLower(line), query) {
					continue
				}
				hits = append(hits, Hit{Source: dir + "/" + name, Text: strings.TrimSpace(line)})
				if len(hits) >= limit {
					return hits
				}
			}
		}
	}
	return hits
}

// Node is one page in the derived graph view.
type Node struct {
	Title     string   `json:"title"`
	Links     []string `json:"links,omitempty"`
	Backlinks int      `json:"backlinks"`
}

var wikilink = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// Graph derives the link graph the Logseq way: nodes are pages plus
// every [[target]] mentioned anywhere, edges come from scanning
// files at read time, and nothing is stored. Journals contribute
// backlinks only — hundreds of dated files as nodes would drown the
// topic graph.
func (s *Store) Graph() []Node {
	out := map[string][]string{}
	backs := map[string]int{}
	nodes := map[string]bool{}
	targets := func(content string) []string {
		var ts []string
		for _, m := range wikilink.FindAllStringSubmatch(content, -1) {
			if t := strings.ToLower(strings.TrimSpace(m[1])); t != "" {
				ts = append(ts, t)
			}
		}
		return ts
	}
	// Backlinks count distinct SOURCE files, not raw mentions: a page
	// that names [[github]] twice is one backlink, not two.
	for _, name := range s.list("pages") {
		title := strings.TrimSuffix(name, ".md")
		nodes[title] = true
		raw, err := os.ReadFile(filepath.Join(s.root, "pages", name))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, t := range targets(string(raw)) {
			nodes[t] = true
			if !seen[t] {
				backs[t]++
				out[title] = append(out[title], t)
				seen[t] = true
			}
		}
	}
	for _, name := range s.list("journals") {
		raw, err := os.ReadFile(filepath.Join(s.root, "journals", name))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, t := range targets(string(raw)) {
			nodes[t] = true
			if !seen[t] {
				backs[t]++
				seen[t] = true
			}
		}
	}
	titles := make([]string, 0, len(nodes))
	for t := range nodes {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	result := make([]Node, 0, len(titles))
	for _, t := range titles {
		result = append(result, Node{Title: t, Links: out[t], Backlinks: backs[t]})
	}
	return result
}

// Stats summarizes the graph for `ihme memory`.
type Stats struct {
	Root       string `json:"root"`
	Journals   int    `json:"journals"`
	Pages      int    `json:"pages"`
	Flashcards int    `json:"flashcards"`
}

func (s *Store) Stats() Stats {
	st := Stats{Root: s.root}
	st.Journals = len(s.list("journals"))
	st.Pages = len(s.list("pages"))
	if cards, ok := s.ReadPage(FlashcardsPage); ok {
		for _, line := range strings.Split(cards, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "- ") {
				st.Flashcards++
			}
		}
	}
	return st
}

// list returns the .md filenames directly under root/<dir>.
func (s *Store) list(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(s.root, dir))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	return names
}

// appendFile creates parents on demand with the same permissions as
// the session file: this graph holds real addresses and labels.
func (s *Store) appendFile(rel, block string) error {
	if s.root == "" {
		return fmt.Errorf("no memory root (home directory unknown)")
	}
	path := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	_, err = f.WriteString(block)
	return err
}

// pageFile maps a page title to a safe filename: lowercase, with
// separators and filesystem-hostile characters collapsed to '-'.
// Titles differing only in case or separators (e.g. "a b" and "a/b")
// collapse to the same file and merge — acceptable because topics are
// service labels (single lowercase words), where this does not arise.
func pageFile(title string) string {
	mapped := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '-'
		}
		return r
	}, strings.ToLower(strings.TrimSpace(title)))
	return mapped + ".md"
}
