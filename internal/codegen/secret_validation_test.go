package codegen

import (
	"strings"
	"testing"
)

func secretParam(name string) Param {
	return Param{Name: name, Type: GoType{
		Kind:      "struct",
		Name:      "Secret",
		Package:   flugoBridgePackage,
		IsPointer: true,
	}}
}

func stringParam(name string) Param {
	return Param{Name: name, Type: GoType{Kind: "string"}}
}

// TestValidateSecretSignatures: a lone Secret passes; a Secret mixed with
// other params (or multiple Secrets) fails generation with a clear error —
// the case that used to silently emit Dart referencing a nonexistent Secret
// class.
func TestValidateSecretSignatures(t *testing.T) {
	ok := []BoundType{{
		Name: "Svc",
		Methods: []Method{
			{Name: "Unlock", Params: []Param{secretParam("secret")}},
			{Name: "Plain", Params: []Param{stringParam("name")}},
			{Name: "NoArgs"},
		},
	}}
	if err := validateSecretSignatures(ok); err != nil {
		t.Fatalf("valid signatures rejected: %v", err)
	}

	mixed := []BoundType{{
		Name: "Svc",
		Methods: []Method{
			{Name: "BadMix", Params: []Param{stringParam("name"), secretParam("secret")}},
		},
	}}
	err := validateSecretSignatures(mixed)
	if err == nil {
		t.Fatal("mixed Secret signature must be rejected")
	}
	if !strings.Contains(err.Error(), "Svc.BadMix") || !strings.Contains(err.Error(), "stage-then-consume") {
		t.Fatalf("error must name the method and point at the pattern: %v", err)
	}

	twoSecrets := []BoundType{{
		Name: "Svc",
		Methods: []Method{
			{Name: "TwoSecrets", Params: []Param{secretParam("a"), secretParam("b")}},
		},
	}}
	if err := validateSecretSignatures(twoSecrets); err == nil {
		t.Fatal("multiple Secrets must be rejected")
	}
}
