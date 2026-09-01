package codegen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func streamOf(elem GoType) GoType {
	return GoType{Kind: "stream", Name: "Emitter", Package: flugoBridgePackage, ElemType: &elem}
}

// TestStreamDartType: a stream return maps to Dart Stream<T>.
func TestStreamDartType(t *testing.T) {
	if got := dartType(streamOf(GoType{Kind: "basic", Name: "int"})); got != "Stream<int>" {
		t.Errorf("dartType = %q, want Stream<int>", got)
	}
	if got := dartType(streamOf(GoType{Kind: "struct", Name: "Progress"})); got != "Stream<Progress>" {
		t.Errorf("dartType = %q, want Stream<Progress>", got)
	}
}

// TestValidateStreamSignatures: (Emitter) and (Emitter, error) are valid; a
// second non-error return or a Secret param is rejected.
func TestValidateStreamSignatures(t *testing.T) {
	errRet := ReturnType{Type: GoType{Kind: "error", Name: "error"}}
	streamRet := ReturnType{Type: streamOf(GoType{Kind: "basic", Name: "int"})}
	intRet := ReturnType{Type: GoType{Kind: "basic", Name: "int"}}

	ok := []BoundType{{Name: "Svc", Methods: []Method{
		{Name: "A", Returns: []ReturnType{streamRet}},
		{Name: "B", Returns: []ReturnType{streamRet, errRet}},
		{Name: "Plain", Params: []Param{stringParam("x")}},
	}}}
	if err := validateStreamSignatures(ok); err != nil {
		t.Fatalf("valid stream signatures rejected: %v", err)
	}

	twoVals := []BoundType{{Name: "Svc", Methods: []Method{
		{Name: "Bad", Returns: []ReturnType{streamRet, intRet}},
	}}}
	if err := validateStreamSignatures(twoVals); err == nil || !strings.Contains(err.Error(), "Svc.Bad") {
		t.Fatalf("stream + extra value must be rejected, got %v", err)
	}

	withSecret := []BoundType{{Name: "Svc", Methods: []Method{
		{Name: "Bad2", Params: []Param{secretParam("s")}, Returns: []ReturnType{streamRet}},
	}}}
	if err := validateStreamSignatures(withSecret); err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("stream + secret must be rejected, got %v", err)
	}
}

// TestGenerateDartServiceStream: a stream method emits a synchronous Stream<T>
// getter delegating to FlugoBridge.openStream<T>, not a Future.
func TestGenerateDartServiceStream(t *testing.T) {
	bt := BoundType{Name: "Downloads", Methods: []Method{{
		Name:    "Progress",
		Params:  []Param{{Name: "id", Type: GoType{Kind: "basic", Name: "string"}}},
		Returns: []ReturnType{{Type: streamOf(GoType{Kind: "struct", Name: "Progress"})}},
	}}}

	var buf bytes.Buffer
	(&Generator{}).generateDartService(&buf, bt)
	out := buf.String()

	for _, want := range []string{
		"Stream<Progress> progress(String id) {",
		"FlugoBridge.openStream<Progress>('Downloads.Progress', [id]",
		"Progress.fromJson(d as Map<String, dynamic>)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated service missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Future<Stream") {
		t.Errorf("stream method must not be wrapped in a Future:\n%s", out)
	}
}

// TestGenerateDartStreamPlumbing: when any method streams, the FlugoBridge gets
// the native-port bindings + openStream helper and the dart:async import.
func TestGenerateDartStreamPlumbing(t *testing.T) {
	bt := BoundType{Name: "Downloads", Methods: []Method{{
		Name: "Progress",
		Returns: []ReturnType{{Type: streamOf(GoType{
			Kind:   "struct",
			Name:   "Progress",
			Fields: []Field{{Name: "Pct", Type: GoType{Kind: "basic", Name: "float64"}, JSONName: "pct"}},
		})}},
	}}}

	dir := t.TempDir()
	if err := (&Generator{}).generateDart([]BoundType{bt}, dir); err != nil {
		t.Fatalf("generateDart: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "bridge.gen.dart"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{
		"import 'dart:async';",
		"'FlugoOpenStream'",
		"'FlugoInitDartApi'",
		"static Stream<T> openStream<T>(",
		"NativeApi.initializeApiDLData",
		"class Progress {", // the element struct got generated
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated bridge missing %q", want)
		}
	}
}
