package codegen

import (
	"strings"
	"testing"
)

// TestMapCodegen: map deserialization must reconstruct the declared Map<K, V>
// faithfully — each value converted via its own type, and integer keys (which
// encoding/json renders as strings) parsed back — rather than the old
// Map<String, dynamic> that mismatched the declared return type.
func TestMapCodegen(t *testing.T) {
	basic := func(name string) GoType { return GoType{Kind: "basic", Name: name} }
	mapOf := func(k, v GoType) GoType {
		return GoType{Kind: "map", Name: "map", KeyType: &k, ElemType: &v}
	}

	cases := []struct {
		name     string
		gt       GoType
		wantType string
		wantIn   []string // substrings the fromJSON expression must contain
	}{
		{
			name:     "map[string]int",
			gt:       mapOf(basic("string"), basic("int")),
			wantType: "Map<String, int>",
			wantIn:   []string{"MapEntry<String, int>", "k as String", "(v as num?)?.toInt() ?? 0"},
		},
		{
			name:     "map[int]string",
			gt:       mapOf(basic("int"), basic("string")),
			wantType: "Map<int, String>",
			wantIn:   []string{"MapEntry<int, String>", "int.parse(k as String)", "v as String"},
		},
		{
			name:     "map[string]struct",
			gt:       mapOf(basic("string"), GoType{Kind: "struct", Name: "Foo"}),
			wantType: "Map<String, Foo>",
			wantIn:   []string{"MapEntry<String, Foo>", "Foo.fromJson(v as Map<String, dynamic>)"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dartType(c.gt); got != c.wantType {
				t.Errorf("dartType = %q, want %q", got, c.wantType)
			}
			expr := dartFromJSON(c.gt, "raw")
			for _, want := range c.wantIn {
				if !strings.Contains(expr, want) {
					t.Errorf("dartFromJSON = %q, missing %q", expr, want)
				}
			}
			// The declared type and the deserializer's constructed type must agree.
			if strings.Contains(expr, "Map<String, dynamic>.from") {
				t.Errorf("dartFromJSON still emits the untyped Map<String, dynamic>: %q", expr)
			}
		})
	}
}
