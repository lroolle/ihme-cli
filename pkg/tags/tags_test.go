package tags

import (
	"reflect"
	"testing"
)

func TestParseTagsAndNote(t *testing.T) {
	tests := []struct {
		input string
		tags  []string
		note  string
	}{
		{"#dev #work | main account", []string{"dev", "work"}, "main account"},
		{"#shopping | amazon prime", []string{"shopping"}, "amazon prime"},
		{"#dev", []string{"dev"}, ""},
		{"just a note", nil, "just a note"},
		{"", nil, ""},
		{"#a #b #c | ", []string{"a", "b", "c"}, ""},
		{"#tag1 | note with #hash", []string{"tag1"}, "note with #hash"},
	}

	for _, tt := range tests {
		p := Parse(tt.input)
		if !reflect.DeepEqual(p.Tags, tt.tags) && !(len(p.Tags) == 0 && len(tt.tags) == 0) {
			t.Errorf("Parse(%q).Tags = %v, want %v", tt.input, p.Tags, tt.tags)
		}
		if p.Note != tt.note {
			t.Errorf("Parse(%q).Note = %q, want %q", tt.input, p.Note, tt.note)
		}
	}
}

func TestSerialize(t *testing.T) {
	tests := []struct {
		tags []string
		note string
		want string
	}{
		{[]string{"dev", "work"}, "main", "#dev #work | main"},
		{[]string{"dev"}, "", "#dev"},
		{nil, "just a note", "just a note"},
		{nil, "", ""},
	}

	for _, tt := range tests {
		got := Serialize(tt.tags, tt.note)
		if got != tt.want {
			t.Errorf("Serialize(%v, %q) = %q, want %q", tt.tags, tt.note, got, tt.want)
		}
	}
}

func TestAll(t *testing.T) {
	notes := []string{
		"#dev #work | note1",
		"#dev #personal | note2",
		"#shopping | note3",
	}
	result := All(notes)
	expected := []string{"dev", "work", "personal", "shopping"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("All = %v, want %v", result, expected)
	}
}
