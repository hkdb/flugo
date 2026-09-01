// Package codegen scans Go source for bridge.Bind() calls, discovers bound types
// and their public methods via AST, then generates bridge code and Dart wrappers.
package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

//go:embed filechooser.dart
var filechooserDart []byte

// BoundType represents a Go struct registered with bridge.Bind().
type BoundType struct {
	Name    string
	Methods []Method
}

// Method represents a public method on a bound type.
type Method struct {
	Name    string
	Params  []Param
	Returns []ReturnType
}

// Param represents a method parameter.
type Param struct {
	Name string
	Type GoType
}

// ReturnType represents a return type.
type ReturnType struct {
	Type GoType
}

// GoType represents a Go type with enough info for code generation.
type GoType struct {
	Name      string  // Go type name (e.g., "string", "[]byte", "KeyPair")
	Kind      string  // "basic", "slice", "struct", "map", "pointer", "error"
	ElemType  *GoType // For slices, pointers, maps
	KeyType   *GoType // For maps
	Package   string  // Package path for struct types
	Fields    []Field // For struct types
	IsPointer bool
}

// Field represents a struct field.
type Field struct {
	Name     string
	Type     GoType
	JSONName string
}

// Generator holds state for code generation.
type Generator struct {
	backendDir string
	modulePath string
}

// New creates a Generator for the given backend directory and Go module path.
func New(backendDir, modulePath string) *Generator {
	return &Generator{
		backendDir: backendDir,
		modulePath: modulePath,
	}
}

// Generate runs the full codegen pipeline: scan, analyze, generate.
func (g *Generator) Generate(outputGoDir, outputDartDir string) error {
	boundNames, err := g.scanBindCalls()
	if err != nil {
		return fmt.Errorf("scanning Bind() calls: %w", err)
	}
	if len(boundNames) == 0 {
		return fmt.Errorf("no bridge.Bind() calls found in %s", g.backendDir)
	}

	boundTypes, err := g.analyzeTypes(boundNames)
	if err != nil {
		return fmt.Errorf("analyzing types: %w", err)
	}

	if err := validateSecretSignatures(boundTypes); err != nil {
		return err
	}
	if err := validateStreamSignatures(boundTypes); err != nil {
		return err
	}
	if err := validateReturnSignatures(boundTypes); err != nil {
		return err
	}

	if err := os.MkdirAll(outputGoDir, 0o755); err != nil {
		return fmt.Errorf("creating Go output dir: %w", err)
	}
	if err := os.MkdirAll(outputDartDir, 0o755); err != nil {
		return fmt.Errorf("creating Dart output dir: %w", err)
	}

	// Clean up stale bridge.gen.go from the old output location (outputGoDir).
	// The Go bridge file is now written to g.backendDir so it compiles as
	// part of package main.
	stale := filepath.Join(outputGoDir, "bridge.gen.go")
	if _, err := os.Stat(stale); err == nil {
		os.Remove(stale)
	}

	if err := g.generateGobridge(boundTypes, outputGoDir); err != nil {
		return fmt.Errorf("generating Go bridge: %w", err)
	}

	if err := g.generateCHeader(boundTypes, outputGoDir); err != nil {
		return fmt.Errorf("generating C header: %w", err)
	}

	if err := g.generateDart(boundTypes, outputDartDir); err != nil {
		return fmt.Errorf("generating Dart wrappers: %w", err)
	}

	if err := g.generateBuiltinDart(outputDartDir); err != nil {
		return fmt.Errorf("generating built-in Dart files: %w", err)
	}

	return nil
}

// scanBindCalls parses Go source files and finds bridge.Bind(&SomeType{}) calls,
// returning the type names.
func (g *Generator) scanBindCalls() ([]string, error) {
	fset := token.NewFileSet()
	var names []string

	goFiles, err := filepath.Glob(filepath.Join(g.backendDir, "*.go"))
	if err != nil {
		return nil, err
	}

	for _, file := range goFiles {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Match bridge.Bind(...) or just Bind(...)
			if !isBindCall(call) {
				return true
			}

			if len(call.Args) != 1 {
				return true
			}

			typeName := extractTypeName(call.Args[0])
			if typeName != "" {
				names = append(names, typeName)
			}
			return true
		})
	}

	return names, nil
}

// isBindCall checks if a call expression is bridge.Bind() or Bind().
func isBindCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		ident, ok := fn.X.(*ast.Ident)
		if !ok {
			return false
		}
		return ident.Name == "bridge" && fn.Sel.Name == "Bind"
	case *ast.Ident:
		return fn.Name == "Bind"
	}
	return false
}

// extractTypeName gets the type name from an argument like &SomeType{} or &SomeType.
func extractTypeName(expr ast.Expr) string {
	// &SomeType{}
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok {
		return ""
	}

	switch v := unary.X.(type) {
	case *ast.CompositeLit:
		return typeExprName(v.Type)
	case *ast.Ident:
		return v.Name
	}
	return ""
}

func typeExprName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// analyzeTypes uses go/packages to load type information for the bound types.
func (g *Generator) analyzeTypes(names []string) ([]BoundType, error) {
	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedName,
		Dir:  g.backendDir,
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found in %s", g.backendDir)
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		var errs []string
		for _, e := range pkg.Errors {
			errs = append(errs, e.Error())
		}
		return nil, fmt.Errorf("package errors: %s", strings.Join(errs, "; "))
	}

	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}

	var result []BoundType

	scope := pkg.Types.Scope()
	for _, n := range names {
		obj := scope.Lookup(n)
		if obj == nil {
			return nil, fmt.Errorf("type %s not found in package", n)
		}

		named, ok := obj.Type().(*types.Named)
		if !ok {
			return nil, fmt.Errorf("%s is not a named type", n)
		}

		bt := BoundType{Name: n}
		ptrType := types.NewPointer(named)
		mset := types.NewMethodSet(ptrType)

		for i := 0; i < mset.Len(); i++ {
			sel := mset.At(i)
			if !sel.Obj().Exported() {
				continue
			}

			fn, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}

			sig := fn.Type().(*types.Signature)
			m := Method{Name: fn.Name()}

			params := sig.Params()
			for j := 0; j < params.Len(); j++ {
				p := params.At(j)
				m.Params = append(m.Params, Param{
					Name: p.Name(),
					Type: goTypeFromTypesType(p.Type()),
				})
			}

			results := sig.Results()
			for j := 0; j < results.Len(); j++ {
				r := results.At(j)
				m.Returns = append(m.Returns, ReturnType{
					Type: goTypeFromTypesType(r.Type()),
				})
			}

			bt.Methods = append(bt.Methods, m)
		}

		result = append(result, bt)
	}

	return result, nil
}

// goTypeFromTypesType converts a go/types.Type to our GoType representation.
func goTypeFromTypesType(t types.Type) GoType {
	// bridge.Emitter[T] is the streaming marker: a method returning one becomes
	// a Dart Stream<T>. Detect the instantiated generic before it falls through
	// to the struct case (its underlying is a struct). Reached via the Pointer
	// case for the usual *bridge.Emitter[T] return.
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == flugoBridgePackage && obj.Name() == "Emitter" {
			if args := named.TypeArgs(); args != nil && args.Len() == 1 {
				elem := goTypeFromTypesType(args.At(0))
				return GoType{Name: "Emitter", Kind: "stream", Package: flugoBridgePackage, ElemType: &elem}
			}
		}
	}
	switch v := t.Underlying().(type) {
	case *types.Basic:
		return GoType{Name: v.Name(), Kind: "basic"}
	case *types.Slice:
		elem := goTypeFromTypesType(v.Elem())
		if elem.Name == "byte" {
			return GoType{Name: "[]byte", Kind: "bytes"}
		}
		return GoType{Name: "[]" + elem.Name, Kind: "slice", ElemType: &elem}
	case *types.Pointer:
		elem := goTypeFromTypesType(v.Elem())
		elem.IsPointer = true
		return elem
	case *types.Struct:
		gt := GoType{Kind: "struct"}
		if named, ok := t.(*types.Named); ok {
			gt.Name = named.Obj().Name()
			gt.Package = named.Obj().Pkg().Path()
		}
		for i := 0; i < v.NumFields(); i++ {
			f := v.Field(i)
			if !f.Exported() {
				continue
			}
			jsonTag := parseJSONTag(v.Tag(i))
			if jsonTag == "-" {
				continue
			}
			if jsonTag == "" {
				jsonTag = toLowerCamel(f.Name())
			}
			gt.Fields = append(gt.Fields, Field{
				Name:     f.Name(),
				Type:     goTypeFromTypesType(f.Type()),
				JSONName: jsonTag,
			})
		}
		return gt
	case *types.Map:
		key := goTypeFromTypesType(v.Key())
		elem := goTypeFromTypesType(v.Elem())
		return GoType{Name: "map", Kind: "map", KeyType: &key, ElemType: &elem}
	case *types.Interface:
		if t.String() == "error" {
			return GoType{Name: "error", Kind: "error"}
		}
		return GoType{Name: "interface{}", Kind: "interface"}
	default:
		return GoType{Name: t.String(), Kind: "unknown"}
	}
}

func parseJSONTag(tag string) string {
	// Parse struct tag to find json tag.
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]

		// Unquote the value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		val := tag[1:i]
		tag = tag[i+1:]

		if name == "json" {
			if idx := strings.Index(val, ","); idx != -1 {
				val = val[:idx]
			}
			return val
		}
	}
	return ""
}

// --- Go bridge code generation ---
//
// The current bridge.gen.go is a stub: it only blank-imports the runtime
// bridge package, which performs method dispatch via reflection at runtime
// (see github.com/hkdb/flugo/pkg/bridge). No per-type generated code is
// emitted today. The boundTypes parameter is kept (as `_`) so the three
// generators (generateGobridge / generateCHeader / generateDart) keep a
// uniform signature, and so a future switch to static dispatch can use it
// without changing call sites.

func (g *Generator) generateGobridge(_ []BoundType, _ string) error {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by flugo. DO NOT EDIT.\n")
	buf.WriteString("package main\n\n")
	buf.WriteString("// This file is intentionally minimal.\n")
	buf.WriteString("// The bridge runtime (github.com/hkdb/flugo/pkg/bridge) handles\n")
	buf.WriteString("// method dispatch via reflection at runtime.\n")
	buf.WriteString("// No generated Go dispatch code is needed.\n\n")
	buf.WriteString("import (\n\t_ \"github.com/hkdb/flugo/pkg/bridge\"\n\t_ \"github.com/hkdb/flugo/pkg/filechooser\"\n)\n")

	// Write to the backend root directory (same package as main.go) so blank
	// imports are compiled. The bridge/ subdirectory is a separate Go package
	// and would never be included in the main build.
	return os.WriteFile(filepath.Join(g.backendDir, "bridge.gen.go"), buf.Bytes(), 0o644)
}

// --- C header generation ---

// generateCHeader emits the FlugoCall/FlugoCallBytes/FlugoCallSecure/
// FlugoGetBytes/FlugoFreeResult C bindings that Flutter's FFI binds to.
// These are static — they don't depend on per-type info — so boundTypes is
// currently unused. Kept (as `_`) for signature parity with the other
// generators.
func (g *Generator) generateCHeader(boundTypes []BoundType, outputDir string) error {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by flugo. DO NOT EDIT.\n")
	buf.WriteString("#ifndef FLUGO_BRIDGE_H\n")
	buf.WriteString("#define FLUGO_BRIDGE_H\n\n")
	buf.WriteString("#include <stdint.h>\n\n")
	buf.WriteString("// FlugoCall dispatches a JSON method call to the Go backend.\n")
	buf.WriteString("// Returns a JSON response string that must be freed with FlugoFreeResult.\n")
	buf.WriteString("extern char* FlugoCall(char* name, int nameLen, char* payload, int payloadLen);\n\n")
	buf.WriteString("// FlugoCallBytes dispatches a binary method call. Returns a buffer ID.\n")
	buf.WriteString("extern int64_t FlugoCallBytes(char* name, int nameLen, char* data, int dataLen);\n\n")
	buf.WriteString("// FlugoCallSecure dispatches a binary method call into a method bound\n")
	buf.WriteString("// with a *bridge.Secret parameter. Bytes are sealed into a memguard\n")
	buf.WriteString("// enclave on intake (no JSON, no Go-string materialization). Returns a\n")
	buf.WriteString("// JSON response string that must be freed with FlugoFreeResult.\n")
	buf.WriteString("extern char* FlugoCallSecure(char* name, int nameLen, uint8_t* data, int dataLen);\n\n")
	buf.WriteString("// FlugoGetBytes retrieves a byte buffer by ID. Caller must free the result.\n")
	buf.WriteString("extern char* FlugoGetBytes(int64_t id, int* outLen);\n\n")
	buf.WriteString("// FlugoFreeResult frees a string returned by FlugoCall.\n")
	buf.WriteString("extern void FlugoFreeResult(char* ptr);\n\n")
	if hasStreamMethods(boundTypes) {
		buf.WriteString("// FlugoInitDartApi wires up the Dart dynamic-linking API for Go->Dart\n")
		buf.WriteString("// stream pushes. Call once with NativeApi.initializeApiDLData. Returns 1 on success.\n")
		buf.WriteString("extern int FlugoInitDartApi(void* data);\n\n")
		buf.WriteString("// FlugoOpenStream starts a bound streaming method, posting JSON events to\n")
		buf.WriteString("// the given Dart port. Returns a stream id for FlugoStreamCancel.\n")
		buf.WriteString("extern int64_t FlugoOpenStream(char* name, int nameLen, char* payload, int payloadLen, int64_t port);\n\n")
		buf.WriteString("// FlugoStreamCancel tells the Go producer to stop when the Dart subscriber cancels.\n")
		buf.WriteString("extern void FlugoStreamCancel(int64_t sid);\n\n")
	}
	buf.WriteString("#endif // FLUGO_BRIDGE_H\n")

	return os.WriteFile(filepath.Join(outputDir, "bridge.gen.h"), buf.Bytes(), 0o644)
}

// --- Dart code generation ---

// flugoBridgePackage is the Go import path for the flugo bridge package.
// Special types from this package (Secret) get the secure-channel codegen
// treatment instead of the standard JSON path.
const flugoBridgePackage = "github.com/hkdb/flugo/pkg/bridge"

// isSecretParam reports whether a parameter is a *bridge.Secret pointer.
// Methods with exactly one Secret parameter are dispatched via the
// FlugoCallSecure raw-bytes path; the Dart wrapper takes a Uint8List.
func isSecretParam(p Param) bool {
	return p.Type.Kind == "struct" &&
		p.Type.Name == "Secret" &&
		p.Type.Package == flugoBridgePackage &&
		p.Type.IsPointer
}

// isSecureMethod reports whether a method should use the secure dispatch
// path: exactly one user-visible parameter, of type *bridge.Secret.
func isSecureMethod(m Method) bool {
	return len(m.Params) == 1 && isSecretParam(m.Params[0])
}

// validateSecretSignatures rejects any method that mixes a *bridge.Secret
// with other parameters (or takes more than one Secret). The secure channel
// is single-secret-only; before this check, a mixed signature silently fell
// through to the JSON path and emitted Dart referencing a nonexistent Secret
// class — a broken build with no explanation. Secrets must be the method's
// ONLY parameter; pair the secure call with a stage-then-consume method for
// operations that also need non-secret arguments (see docs/SECRETS.md).
func validateSecretSignatures(boundTypes []BoundType) error {
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			secrets := 0
			for _, p := range m.Params {
				if isSecretParam(p) {
					secrets++
				}
			}
			if secrets == 0 || isSecureMethod(m) {
				continue
			}
			return fmt.Errorf(
				"%s.%s: *bridge.Secret must be the method's ONLY parameter — the secure channel is single-secret-only.\n"+
					"Split the call: stage the secret via a dedicated lone-Secret method, then consume it in the "+
					"method that carries the other arguments (see docs/SECRETS.md, \"stage-then-consume\")",
				bt.Name, m.Name)
		}
	}
	return nil
}

// hasSecureMethods reports whether any bound method uses the secure path.
// Gates emission of the FlugoCallSecure typedef + helper.
func hasSecureMethods(boundTypes []BoundType) bool {
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			if isSecureMethod(m) {
				return true
			}
		}
	}
	return false
}

// streamElem returns the element type of a method's stream return (Kind
// "stream") and whether the method IS a stream method. A stream method returns
// exactly one *bridge.Emitter[T], optionally with a trailing error.
func streamElem(m Method) (GoType, bool) {
	var elem GoType
	found := false
	for _, r := range m.Returns {
		if r.Type.Kind == "stream" {
			if r.Type.ElemType != nil {
				elem = *r.Type.ElemType
			}
			found = true
		}
	}
	return elem, found
}

// isStreamMethod reports whether a method is exposed as a Dart Stream<T>.
func isStreamMethod(m Method) bool {
	_, ok := streamElem(m)
	return ok
}

// hasStreamMethods gates emission of the Go→Dart streaming FFI plumbing
// (Dart-native-port bindings + openStream/cancelStream helpers).
func hasStreamMethods(boundTypes []BoundType) bool {
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			if isStreamMethod(m) {
				return true
			}
		}
	}
	return false
}

// validateStreamSignatures rejects malformed stream methods: an *bridge.Emitter
// return may only be paired with a trailing error (no second value), and cannot
// combine with the secure (*bridge.Secret) channel. A bad shape would otherwise
// emit Dart that doesn't compile.
func validateStreamSignatures(boundTypes []BoundType) error {
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			if !isStreamMethod(m) {
				continue
			}
			nonErr := 0
			for _, r := range m.Returns {
				if r.Type.Kind != "error" {
					nonErr++
				}
			}
			if nonErr != 1 {
				return fmt.Errorf(
					"%s.%s: a streaming method must return exactly one *bridge.Emitter[T] (optionally with a trailing error)",
					bt.Name, m.Name)
			}
			if isSecureMethod(m) {
				return fmt.Errorf(
					"%s.%s: a streaming method cannot also take a *bridge.Secret parameter", bt.Name, m.Name)
			}
		}
	}
	return nil
}

// validateReturnSignatures rejects bound (non-stream) methods that return more
// than one non-error value. The runtime dispatch (encodeResults) only supports
// (), (T), (error), and (T, error) — for >1 non-error return it would send the
// first value while the Dart wrapper is typed off the last, silently corrupting
// results. Reject the shape at generation time rather than mis-generate.
// Stream methods are validated separately (validateStreamSignatures).
func validateReturnSignatures(boundTypes []BoundType) error {
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			if isStreamMethod(m) {
				continue
			}
			nonErr := 0
			for _, r := range m.Returns {
				if r.Type.Kind != "error" {
					nonErr++
				}
			}
			if nonErr > 1 {
				return fmt.Errorf(
					"%s.%s: a bound method may return at most one non-error value (got %d); return a struct if you need multiple values",
					bt.Name, m.Name, nonErr)
			}
		}
	}
	return nil
}

// needsTypedData reports whether any bound method has a parameter or return
// type that maps to Uint8List in Dart (i.e. a Go `[]byte` or *bridge.Secret).
// Used to gate the `import 'dart:typed_data'` line so we don't emit
// unused-import warnings on services that never exchange raw bytes.
func needsTypedData(boundTypes []BoundType) bool {
	if hasSecureMethods(boundTypes) {
		return true
	}
	var typeUsesBytes func(t GoType) bool
	typeUsesBytes = func(t GoType) bool {
		if t.Kind == "bytes" {
			return true
		}
		if t.ElemType != nil && typeUsesBytes(*t.ElemType) {
			return true
		}
		for _, f := range t.Fields {
			if typeUsesBytes(f.Type) {
				return true
			}
		}
		return false
	}
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			for _, p := range m.Params {
				if typeUsesBytes(p.Type) {
					return true
				}
			}
			for _, r := range m.Returns {
				if typeUsesBytes(r.Type) {
					return true
				}
			}
		}
	}
	return false
}

func (g *Generator) generateDart(boundTypes []BoundType, outputDir string) error {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by flugo. DO NOT EDIT.\n\n")
	if hasStreamMethods(boundTypes) {
		buf.WriteString("import 'dart:async';\n")
	}
	buf.WriteString("import 'dart:convert';\n")
	buf.WriteString("import 'dart:ffi';\n")
	buf.WriteString("import 'dart:isolate';\n")
	if needsTypedData(boundTypes) {
		buf.WriteString("import 'dart:typed_data';\n")
	}
	buf.WriteString("import 'package:ffi/ffi.dart';\n\n")

	// Generate FlugoException class.
	buf.WriteString("class FlugoException implements Exception {\n")
	buf.WriteString("  final String message;\n")
	buf.WriteString("  FlugoException(this.message);\n\n")
	buf.WriteString("  @override\n")
	buf.WriteString("  String toString() => message;\n")
	buf.WriteString("}\n\n")

	secureMethods := hasSecureMethods(boundTypes)
	streamMethods := hasStreamMethods(boundTypes)

	// Generate FFI bindings class.
	buf.WriteString("// FFI function signatures.\n")
	buf.WriteString("typedef _FlugoCallNative = Pointer<Utf8> Function(\n")
	buf.WriteString("    Pointer<Utf8> name, Int32 nameLen, Pointer<Utf8> payload, Int32 payloadLen);\n")
	buf.WriteString("typedef _FlugoCallDart = Pointer<Utf8> Function(\n")
	buf.WriteString("    Pointer<Utf8> name, int nameLen, Pointer<Utf8> payload, int payloadLen);\n\n")
	buf.WriteString("typedef _FlugoFreeResultNative = Void Function(Pointer<Utf8> ptr);\n")
	buf.WriteString("typedef _FlugoFreeResultDart = void Function(Pointer<Utf8> ptr);\n\n")
	if secureMethods {
		buf.WriteString("typedef _FlugoCallSecureNative = Pointer<Utf8> Function(\n")
		buf.WriteString("    Pointer<Utf8> name, Int32 nameLen, Pointer<Uint8> data, Int32 dataLen);\n")
		buf.WriteString("typedef _FlugoCallSecureDart = Pointer<Utf8> Function(\n")
		buf.WriteString("    Pointer<Utf8> name, int nameLen, Pointer<Uint8> data, int dataLen);\n\n")
	}
	if streamMethods {
		buf.WriteString("typedef _FlugoInitDartApiNative = Int Function(Pointer<Void> data);\n")
		buf.WriteString("typedef _FlugoInitDartApiDart = int Function(Pointer<Void> data);\n")
		buf.WriteString("typedef _FlugoOpenStreamNative = Int64 Function(\n")
		buf.WriteString("    Pointer<Utf8> name, Int32 nameLen, Pointer<Utf8> payload, Int32 payloadLen, Int64 port);\n")
		buf.WriteString("typedef _FlugoOpenStreamDart = int Function(\n")
		buf.WriteString("    Pointer<Utf8> name, int nameLen, Pointer<Utf8> payload, int payloadLen, int port);\n")
		buf.WriteString("typedef _FlugoStreamCancelNative = Void Function(Int64 sid);\n")
		buf.WriteString("typedef _FlugoStreamCancelDart = void Function(int sid);\n\n")
	}

	buf.WriteString("class FlugoBridge {\n")
	buf.WriteString("  static late final DynamicLibrary _lib;\n")
	buf.WriteString("  static late final _FlugoCallDart _flugoCall;\n")
	buf.WriteString("  static late final _FlugoFreeResultDart _flugoFreeResult;\n")
	if secureMethods {
		buf.WriteString("  static late final _FlugoCallSecureDart _flugoCallSecure;\n")
	}
	if streamMethods {
		buf.WriteString("  static late final _FlugoInitDartApiDart _flugoInitDartApi;\n")
		buf.WriteString("  static late final _FlugoOpenStreamDart _flugoOpenStream;\n")
		buf.WriteString("  static late final _FlugoStreamCancelDart _flugoStreamCancel;\n")
		buf.WriteString("  static bool _streamsInitialized = false;\n")
	}
	buf.WriteString("  static bool _initialized = false;\n")
	buf.WriteString("  static String? _libPath;\n\n")

	buf.WriteString("  static void init(DynamicLibrary lib, [String? path]) {\n")
	buf.WriteString("    if (_initialized) return;\n")
	buf.WriteString("    _lib = lib;\n")
	buf.WriteString("    _libPath = path;\n")
	buf.WriteString("    _flugoCall = _lib.lookupFunction<_FlugoCallNative, _FlugoCallDart>('FlugoCall');\n")
	buf.WriteString("    _flugoFreeResult = _lib.lookupFunction<_FlugoFreeResultNative, _FlugoFreeResultDart>('FlugoFreeResult');\n")
	if secureMethods {
		buf.WriteString("    _flugoCallSecure = _lib.lookupFunction<_FlugoCallSecureNative, _FlugoCallSecureDart>('FlugoCallSecure');\n")
	}
	if streamMethods {
		buf.WriteString("    _flugoInitDartApi = _lib.lookupFunction<_FlugoInitDartApiNative, _FlugoInitDartApiDart>('FlugoInitDartApi');\n")
		buf.WriteString("    _flugoOpenStream = _lib.lookupFunction<_FlugoOpenStreamNative, _FlugoOpenStreamDart>('FlugoOpenStream');\n")
		buf.WriteString("    _flugoStreamCancel = _lib.lookupFunction<_FlugoStreamCancelNative, _FlugoStreamCancelDart>('FlugoStreamCancel');\n")
	}
	buf.WriteString("    _initialized = true;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  static String call(String method, [List<dynamic>? params]) {\n")
	buf.WriteString("    final nameUtf8 = method.toNativeUtf8();\n")
	buf.WriteString("    final payload = params != null ? jsonEncode(params) : '[]';\n")
	buf.WriteString("    final payloadUtf8 = payload.toNativeUtf8();\n\n")
	buf.WriteString("    // Lengths MUST be UTF-8 BYTE counts: String.length is UTF-16 code\n")
	buf.WriteString("    // units, which undercounts any non-ASCII character and truncates\n")
	buf.WriteString("    // the buffer on the Go side.\n")
	buf.WriteString("    final resultPtr = _flugoCall(\n")
	buf.WriteString("      nameUtf8, utf8.encode(method).length, payloadUtf8, utf8.encode(payload).length,\n")
	buf.WriteString("    );\n\n")
	buf.WriteString("    calloc.free(nameUtf8);\n")
	buf.WriteString("    calloc.free(payloadUtf8);\n\n")
	buf.WriteString("    final result = resultPtr.toDartString();\n")
	buf.WriteString("    _flugoFreeResult(resultPtr);\n")
	buf.WriteString("    return result;\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  static Future<String> callAsync(String method, [List<dynamic>? params]) {\n")
	buf.WriteString("    final libPath = _libPath;\n")
	buf.WriteString("    return Isolate.run(() {\n")
	buf.WriteString("      // Re-open the library in the background isolate. The OS caches\n")
	buf.WriteString("      // loaded shared libraries so this is essentially free.\n")
	buf.WriteString("      if (!_initialized && libPath != null) {\n")
	buf.WriteString("        init(DynamicLibrary.open(libPath), libPath);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      return call(method, params);\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  static dynamic decodeResponse(String json) {\n")
	buf.WriteString("    final map = jsonDecode(json) as Map<String, dynamic>;\n")
	buf.WriteString("    if (map.containsKey('error') && map['error'] != null && map['error'] != '') {\n")
	buf.WriteString("      throw FlugoException(map['error'] as String);\n")
	buf.WriteString("    }\n")
	buf.WriteString("    return map['result'];\n")
	buf.WriteString("  }\n")

	if secureMethods {
		buf.WriteString("\n")
		buf.WriteString("  // callSecure dispatches a method bound with a *bridge.Secret parameter.\n")
		buf.WriteString("  // The secret bytes are copied into a malloc'd C buffer (no JSON, no\n")
		buf.WriteString("  // String materialization); the C buffer is wiped before being freed,\n")
		buf.WriteString("  // and the Go side seals the bytes into a memguard enclave on intake.\n")
		buf.WriteString("  // The caller's Uint8List is unchanged here — wipe it on the caller\n")
		buf.WriteString("  // side after this returns.\n")
		buf.WriteString("  //\n")
		buf.WriteString("  // Synchronous (no isolate hop) so we don't pay the cost of copying\n")
		buf.WriteString("  // the Uint8List into the worker isolate. Secure dispatch is rare\n")
		buf.WriteString("  // (typically only for unlock) so blocking the UI for one keystore\n")
		buf.WriteString("  // validation is acceptable.\n")
		buf.WriteString("  static String callSecure(String method, Uint8List secret) {\n")
		buf.WriteString("    final nameUtf8 = method.toNativeUtf8();\n")
		buf.WriteString("    final dataPtr = calloc<Uint8>(secret.length);\n")
		buf.WriteString("    for (var i = 0; i < secret.length; i++) {\n")
		buf.WriteString("      dataPtr[i] = secret[i];\n")
		buf.WriteString("    }\n")
		buf.WriteString("    final resultPtr = _flugoCallSecure(\n")
		buf.WriteString("      nameUtf8, utf8.encode(method).length, dataPtr, secret.length,\n")
		buf.WriteString("    );\n")
		buf.WriteString("    // Wipe the C-side copy before freeing.\n")
		buf.WriteString("    for (var i = 0; i < secret.length; i++) {\n")
		buf.WriteString("      dataPtr[i] = 0;\n")
		buf.WriteString("    }\n")
		buf.WriteString("    calloc.free(dataPtr);\n")
		buf.WriteString("    calloc.free(nameUtf8);\n")
		buf.WriteString("    final result = resultPtr.toDartString();\n")
		buf.WriteString("    _flugoFreeResult(resultPtr);\n")
		buf.WriteString("    return result;\n")
		buf.WriteString("  }\n")
	}

	if streamMethods {
		buf.WriteString("\n")
		buf.WriteString("  static void _ensureStreams() {\n")
		buf.WriteString("    if (_streamsInitialized) return;\n")
		buf.WriteString("    // Wire up the Dart dynamic-linking API so Go can post to our ports.\n")
		buf.WriteString("    final ok = _flugoInitDartApi(NativeApi.initializeApiDLData);\n")
		buf.WriteString("    if (ok == 0) {\n")
		buf.WriteString("      throw FlugoException('flugo: failed to initialize the Dart streaming API');\n")
		buf.WriteString("    }\n")
		buf.WriteString("    _streamsInitialized = true;\n")
		buf.WriteString("  }\n\n")
		buf.WriteString("  // openStream starts a Go streaming method and exposes it as a Dart\n")
		buf.WriteString("  // Stream<T>. Each stream owns a RawReceivePort that Go posts JSON events\n")
		buf.WriteString("  // to ({ev: data|done|err}); cancelling the Dart subscription tells Go to\n")
		buf.WriteString("  // stop. decode turns each decoded 'data' value into a T.\n")
		buf.WriteString("  static Stream<T> openStream<T>(\n")
		buf.WriteString("      String method, List<dynamic> params, T Function(dynamic) decode) {\n")
		buf.WriteString("    _ensureStreams();\n")
		buf.WriteString("    final port = RawReceivePort();\n")
		buf.WriteString("    late StreamController<T> controller;\n")
		buf.WriteString("    var streamId = 0;\n")
		buf.WriteString("    var finished = false;\n")
		buf.WriteString("    void finish() {\n")
		buf.WriteString("      if (finished) return;\n")
		buf.WriteString("      finished = true;\n")
		buf.WriteString("      if (streamId != 0) _flugoStreamCancel(streamId);\n")
		buf.WriteString("      port.close();\n")
		buf.WriteString("    }\n")
		buf.WriteString("    port.handler = (dynamic message) {\n")
		buf.WriteString("      final map = jsonDecode(message as String) as Map<String, dynamic>;\n")
		buf.WriteString("      switch (map['ev']) {\n")
		buf.WriteString("        case 'data':\n")
		buf.WriteString("          if (!controller.isClosed) controller.add(decode(map['data']));\n")
		buf.WriteString("          break;\n")
		buf.WriteString("        case 'err':\n")
		buf.WriteString("          if (!controller.isClosed) {\n")
		buf.WriteString("            controller.addError(FlugoException((map['err'] ?? 'stream error') as String));\n")
		buf.WriteString("          }\n")
		buf.WriteString("          finish();\n")
		buf.WriteString("          if (!controller.isClosed) controller.close();\n")
		buf.WriteString("          break;\n")
		buf.WriteString("        case 'done':\n")
		buf.WriteString("          finish();\n")
		buf.WriteString("          if (!controller.isClosed) controller.close();\n")
		buf.WriteString("          break;\n")
		buf.WriteString("      }\n")
		buf.WriteString("    };\n")
		buf.WriteString("    controller = StreamController<T>(onCancel: finish);\n")
		buf.WriteString("    final nameUtf8 = method.toNativeUtf8();\n")
		buf.WriteString("    final payload = jsonEncode(params);\n")
		buf.WriteString("    final payloadUtf8 = payload.toNativeUtf8();\n")
		buf.WriteString("    streamId = _flugoOpenStream(\n")
		buf.WriteString("      nameUtf8, utf8.encode(method).length, payloadUtf8, utf8.encode(payload).length,\n")
		buf.WriteString("      port.sendPort.nativePort,\n")
		buf.WriteString("    );\n")
		buf.WriteString("    calloc.free(nameUtf8);\n")
		buf.WriteString("    calloc.free(payloadUtf8);\n")
		buf.WriteString("    return controller.stream;\n")
		buf.WriteString("  }\n")
	}

	buf.WriteString("}\n\n")

	// Generate struct classes for return types.
	generatedStructs := map[string]bool{}
	for _, bt := range boundTypes {
		for _, m := range bt.Methods {
			for _, r := range m.Returns {
				g.generateDartStruct(&buf, r.Type, generatedStructs)
			}
			for _, p := range m.Params {
				g.generateDartStruct(&buf, p.Type, generatedStructs)
			}
		}
	}

	// Generate typed service classes.
	for _, bt := range boundTypes {
		g.generateDartService(&buf, bt)
	}

	return os.WriteFile(filepath.Join(outputDir, "bridge.gen.dart"), buf.Bytes(), 0o644)
}

func (g *Generator) generateDartStruct(buf *bytes.Buffer, gt GoType, generated map[string]bool) {
	// Unwrap containers (slice/map/stream) so element/key structs are generated
	// even when the outer type isn't itself a struct.
	if gt.Kind != "struct" {
		if gt.KeyType != nil {
			g.generateDartStruct(buf, *gt.KeyType, generated)
		}
		if gt.ElemType != nil {
			g.generateDartStruct(buf, *gt.ElemType, generated)
		}
		return
	}
	if gt.Name == "" || generated[gt.Name] {
		return
	}
	// bridge.Secret is a marker type for the secure-channel codegen path;
	// it has no Dart class representation (the Dart side just sees Uint8List).
	if gt.Package == flugoBridgePackage && gt.Name == "Secret" {
		return
	}
	generated[gt.Name] = true

	// First generate any nested struct types.
	for _, f := range gt.Fields {
		g.generateDartStruct(buf, f.Type, generated)
	}

	fmt.Fprintf(buf, "class %s {\n", gt.Name)

	// Fields.
	for _, f := range gt.Fields {
		fmt.Fprintf(buf, "  final %s %s;\n", dartType(f.Type), toLowerCamel(f.Name))
	}
	buf.WriteString("\n")

	// Constructor.
	fmt.Fprintf(buf, "  %s({\n", gt.Name)
	for _, f := range gt.Fields {
		fmt.Fprintf(buf, "    required this.%s,\n", toLowerCamel(f.Name))
	}
	buf.WriteString("  });\n\n")

	// fromJson factory.
	fmt.Fprintf(buf, "  factory %s.fromJson(Map<String, dynamic> json) {\n", gt.Name)
	fmt.Fprintf(buf, "    return %s(\n", gt.Name)
	for _, f := range gt.Fields {
		fmt.Fprintf(buf, "      %s: %s,\n", toLowerCamel(f.Name), dartFromJSON(f.Type, fmt.Sprintf("json['%s']", f.JSONName)))
	}
	buf.WriteString("    );\n")
	buf.WriteString("  }\n\n")

	// toJson method.
	buf.WriteString("  Map<String, dynamic> toJson() => {\n")
	for _, f := range gt.Fields {
		fmt.Fprintf(buf, "        '%s': %s,\n", f.JSONName, dartToJSON(f.Type, toLowerCamel(f.Name)))
	}
	buf.WriteString("      };\n")
	buf.WriteString("}\n\n")
}

func (g *Generator) generateDartService(buf *bytes.Buffer, bt BoundType) {
	serviceName := toLowerCamel(bt.Name)
	fmt.Fprintf(buf, "// %s exposes the Go %s methods to Dart.\n", bt.Name, bt.Name)
	fmt.Fprintf(buf, "class %s {\n", bt.Name)

	for _, m := range bt.Methods {
		dartMethodName := toLowerCamel(m.Name)
		methodKey := bt.Name + "." + m.Name

		// Determine return type.
		returnDartType := "void"
		hasError := false
		var resultType *GoType
		for _, r := range m.Returns {
			if r.Type.Kind == "error" {
				hasError = true
				continue
			}
			resultType = &r.Type
			returnDartType = dartType(r.Type)
		}
		_ = hasError

		// Secure path: methods with a single *bridge.Secret parameter use
		// FlugoCallSecure (raw bytes, no JSON), and the Dart wrapper takes
		// a Uint8List that the caller is encouraged to wipe afterwards.
		if isSecureMethod(m) {
			paramName := toLowerCamel(m.Params[0].Name)
			if paramName == "" {
				paramName = "secret"
			}
			futureType := fmt.Sprintf("Future<%s>", returnDartType)
			fmt.Fprintf(buf, "\n  %s %s(Uint8List %s) async {\n", futureType, dartMethodName, paramName)
			fmt.Fprintf(buf, "    final response = FlugoBridge.callSecure('%s', %s);\n", methodKey, paramName)
			if resultType == nil {
				buf.WriteString("    FlugoBridge.decodeResponse(response);\n")
			}
			if resultType != nil {
				buf.WriteString("    final result = FlugoBridge.decodeResponse(response);\n")
				fmt.Fprintf(buf, "    return %s;\n", dartFromJSON(*resultType, "result"))
			}
			buf.WriteString("  }\n")
			continue
		}

		// Stream path: methods returning *bridge.Emitter[T] become a synchronous
		// Dart Stream<T> backed by a RawReceivePort (Go pushes events).
		if elem, ok := streamElem(m); ok {
			elemDart := dartType(elem)
			var sParams []string
			var sArgs []string
			for _, p := range m.Params {
				sParams = append(sParams, fmt.Sprintf("%s %s", dartType(p.Type), toLowerCamel(p.Name)))
				sArgs = append(sArgs, dartToJSON(p.Type, toLowerCamel(p.Name)))
			}
			fmt.Fprintf(buf, "\n  Stream<%s> %s(%s) {\n", elemDart, dartMethodName, strings.Join(sParams, ", "))
			fmt.Fprintf(buf, "    return FlugoBridge.openStream<%s>('%s', [%s], (d) => %s);\n",
				elemDart, methodKey, strings.Join(sArgs, ", "), dartFromJSON(elem, "d"))
			buf.WriteString("  }\n")
			continue
		}

		// Build parameter list.
		var paramList []string
		var argList []string
		for _, p := range m.Params {
			paramList = append(paramList, fmt.Sprintf("%s %s", dartType(p.Type), toLowerCamel(p.Name)))
			argList = append(argList, dartToJSON(p.Type, toLowerCamel(p.Name)))
		}

		futureType := fmt.Sprintf("Future<%s>", returnDartType)
		params := strings.Join(paramList, ", ")

		fmt.Fprintf(buf, "\n  %s %s(%s) async {\n", futureType, dartMethodName, params)
		fmt.Fprintf(buf, "    final response = await FlugoBridge.callAsync('%s', [%s]);\n", methodKey, strings.Join(argList, ", "))

		if resultType == nil {
			buf.WriteString("    FlugoBridge.decodeResponse(response);\n")
		}
		if resultType != nil {
			buf.WriteString("    final result = FlugoBridge.decodeResponse(response);\n")
			fmt.Fprintf(buf, "    return %s;\n", dartFromJSON(*resultType, "result"))
		}

		buf.WriteString("  }\n")
	}

	buf.WriteString("}\n\n")

	// Create a top-level instance.
	fmt.Fprintf(buf, "final %s = %s();\n\n", serviceName, bt.Name)
}

// --- Dart type mapping helpers ---

func dartType(gt GoType) string {
	switch gt.Kind {
	case "basic":
		switch gt.Name {
		case "string":
			return "String"
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
			return "int"
		case "float32", "float64":
			return "double"
		case "bool":
			return "bool"
		}
	case "bytes":
		return "Uint8List"
	case "slice":
		if gt.ElemType != nil {
			return fmt.Sprintf("List<%s>", dartType(*gt.ElemType))
		}
		return "List<dynamic>"
	case "struct":
		return gt.Name
	case "map":
		keyT := "String"
		valT := "dynamic"
		if gt.KeyType != nil {
			keyT = dartType(*gt.KeyType)
		}
		if gt.ElemType != nil {
			valT = dartType(*gt.ElemType)
		}
		return fmt.Sprintf("Map<%s, %s>", keyT, valT)
	case "stream":
		if gt.ElemType != nil {
			return fmt.Sprintf("Stream<%s>", dartType(*gt.ElemType))
		}
		return "Stream<dynamic>"
	case "error":
		return "void"
	}
	return "dynamic"
}

// dartFromJSON emits the Dart expression that decodes `expr` (a JSON value) into
// the given type. Scalars and containers are NULL-TOLERANT: a field that is
// absent (e.g. Go `,omitempty` omitted it) or explicitly null decodes to the
// type's zero value rather than throwing a `null is not a subtype` cast error.
// Struct/interface stay non-null casts — a nested object has no zero value
// without making the Dart field nullable (see docs/STREAMING.md).
func dartFromJSON(gt GoType, expr string) string {
	switch gt.Kind {
	case "basic":
		switch gt.Name {
		case "string":
			return fmt.Sprintf("(%s as String?) ?? ''", expr)
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
			return fmt.Sprintf("(%s as num?)?.toInt() ?? 0", expr)
		case "float32", "float64":
			return fmt.Sprintf("(%s as num?)?.toDouble() ?? 0", expr)
		case "bool":
			return fmt.Sprintf("(%s as bool?) ?? false", expr)
		}
	case "bytes":
		return fmt.Sprintf("base64Decode((%s as String?) ?? '')", expr)
	case "slice":
		if gt.ElemType != nil {
			return fmt.Sprintf("((%s as List?) ?? const []).map((e) => %s).toList()", expr, dartFromJSON(*gt.ElemType, "e"))
		}
		return fmt.Sprintf("(%s as List<dynamic>?) ?? const []", expr)
	case "struct":
		return fmt.Sprintf("%s.fromJson(%s as Map<String, dynamic>)", gt.Name, expr)
	case "map":
		keyT := "String"
		if gt.KeyType != nil {
			keyT = dartType(*gt.KeyType)
		}
		valT := "dynamic"
		valConv := "v"
		if gt.ElemType != nil {
			valT = dartType(*gt.ElemType)
			valConv = dartFromJSON(*gt.ElemType, "v")
		}
		return fmt.Sprintf("((%s as Map?) ?? const <dynamic, dynamic>{}).map((k, v) => MapEntry<%s, %s>(%s, %s))",
			expr, keyT, valT, jsonMapKey(gt.KeyType), valConv)
	}
	return expr
}

// jsonMapKey converts a decoded JSON object key (always a Dart String) into the
// Go map's key type. encoding/json renders integer map keys as strings, so those
// are parsed back; every other key type stays a String.
func jsonMapKey(kt *GoType) string {
	if kt != nil {
		switch kt.Name {
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64":
			return "int.parse(k as String)"
		}
	}
	return "k as String"
}

func dartToJSON(gt GoType, varName string) string {
	switch gt.Kind {
	case "bytes":
		return fmt.Sprintf("base64Encode(%s)", varName)
	case "struct":
		return fmt.Sprintf("%s.toJson()", varName)
	}
	return varName
}

// --- Built-in Dart file generation ---

// generateBuiltinDart writes embedded Dart wrapper files to the output directory.
func (g *Generator) generateBuiltinDart(outputDir string) error {
	return os.WriteFile(filepath.Join(outputDir, "filechooser.dart"), filechooserDart, 0o644)
}

// --- String helpers ---

func toLowerCamel(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
