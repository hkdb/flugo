package codegen

import (
	"strings"
	"testing"
)

func gptr(gt GoType) *GoType { return &gt }

// TestNullTolerantDecode guards the fix for the omitempty decode crash: an
// absent/null JSON value (a Go `,omitempty` field that was omitted) must default
// to the type's zero value, not a non-null cast that throws
// `type 'Null' is not a subtype of type 'String'`.
func TestNullTolerantDecode(t *testing.T) {
	basic := func(name string) GoType { return GoType{Kind: "basic", Name: name} }
	cases := []struct {
		name string
		gt   GoType
		want string
	}{
		{"string", basic("string"), "(x as String?) ?? ''"},
		{"int", basic("int"), "(x as num?)?.toInt() ?? 0"},
		{"int64", basic("int64"), "(x as num?)?.toInt() ?? 0"},
		{"float64", basic("float64"), "(x as num?)?.toDouble() ?? 0"},
		{"bool", basic("bool"), "(x as bool?) ?? false"},
		{"bytes", GoType{Kind: "bytes", Name: "[]byte"}, "base64Decode((x as String?) ?? '')"},
		{"slice", GoType{Kind: "slice", ElemType: gptr(basic("string"))}, "(x as List?) ?? const []"},
		{"map", GoType{Kind: "map", KeyType: gptr(basic("string")), ElemType: gptr(basic("int"))}, "(x as Map?) ?? const <dynamic, dynamic>{}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dartFromJSON(c.gt, "x"); !strings.Contains(got, c.want) {
				t.Errorf("dartFromJSON(%s) = %q, want substring %q", c.name, got, c.want)
			}
		})
	}
}
