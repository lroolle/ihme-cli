package schema_test

import (
	"reflect"
	"testing"

	"github.com/lroolle/ihme-cli/pkg/agentkit/schema"
)

func TestObjectBuilder(t *testing.T) {
	got := schema.Object(
		schema.Property("label", schema.String("service label")).Required(),
		schema.Property("rounds", schema.Int("rotation rounds")),
		schema.Property("kind", schema.Enum("address kind", "work", "personal")),
	)
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":  map[string]any{"type": "string", "description": "service label"},
			"rounds": map[string]any{"type": "integer", "description": "rotation rounds"},
			"kind":   map[string]any{"type": "string", "description": "address kind", "enum": []string{"work", "personal"}},
		},
		"required": []string{"label"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestObjectNoRequired(t *testing.T) {
	got := schema.Object(schema.Property("note", schema.String("n")))
	if _, ok := got["required"]; ok {
		t.Fatal("empty required list must be omitted")
	}
}
