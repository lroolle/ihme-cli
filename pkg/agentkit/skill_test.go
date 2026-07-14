package agentkit_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

func TestSkillInvocationIsTaskTurn(t *testing.T) {
	s := agentkit.Skill{Name: "ihme", Instructions: "1. check auth\n2. search first"}
	msg := s.Invocation("create an address for example.com")
	if msg.Role != agentkit.RoleUser {
		t.Fatalf("role = %q, want user (skill is a task turn, not system prompt)", msg.Role)
	}
	for _, want := range []string{"search first", "example.com", `name="ihme"`} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("invocation missing %q:\n%s", want, msg.Text)
		}
	}
}

// Guard: the kernel imports only the standard library — never ihme
// packages, never third-party modules. Import direction is the
// reusability contract; this test walks every non-test source file
// under pkg/agentkit and fails on any dotted import path (stdlib
// packages have no dot in their first path element).
func TestImportDirection(t *testing.T) {
	root := "." // pkg/agentkit
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			first := strings.SplitN(p, "/", 2)[0]
			if strings.Contains(first, ".") && !strings.HasPrefix(p, "github.com/lroolle/ihme-cli/pkg/agentkit") {
				t.Errorf("%s imports %q — kernel must stay stdlib-only", path, p)
			}
			if strings.HasPrefix(p, "github.com/lroolle/ihme-cli") && !strings.HasPrefix(p, "github.com/lroolle/ihme-cli/pkg/agentkit") {
				t.Errorf("%s imports %q — kernel must not import ihme packages", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
