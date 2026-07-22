package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/skill"
)

// skill/SKILL.md is runtime prompt content: the model reads it AND
// the tool schemas in the same run. If the skill stops mentioning a
// tool, or the reserve contract's required fields, the model reads
// two different contracts — that drift shipped once (the rejected[]
// split) and this test is its tombstone.
func TestSkillStaysInSyncWithEmbeddedTools(t *testing.T) {
	instructions := skill.Instructions()
	for _, tool := range tools(nil, newRunState(""), "a@b", scriptedAsker()) {
		if !strings.Contains(instructions, tool.Name()) {
			t.Errorf("skill/SKILL.md never mentions embedded tool %q — update its execution-adapter section", tool.Name())
		}
		if tool.Name() != "reserve_address" {
			continue
		}
		schema, err := json.Marshal(tool.Schema())
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatal(err)
		}
		for _, field := range parsed.Required {
			if !strings.Contains(instructions, field) {
				t.Errorf("reserve_address requires %q but skill/SKILL.md never names it — the model reads both contracts", field)
			}
		}
	}
}
