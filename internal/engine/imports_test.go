package engine_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportBoundary prevents the core engine/contract packages from acquiring
// process-execution, HTTP, or transport dependencies.
func TestImportBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	banned := []string{
		"internal/transport",
		"internal/ai",
		"net",
		"net/http",
		"os/exec",
	}
	fset := token.NewFileSet()
	for _, pkgDir := range []string{filepath.Join(root, "engine"), filepath.Join(root, "contract")} {
		entries, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range entries {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ImportSpec)
				if !ok || spec.Path == nil {
					return true
				}
				p := strings.Trim(spec.Path.Value, "\"")
				for _, bad := range banned {
					if p == bad || strings.HasPrefix(p, bad+"/") {
						t.Errorf("%s imports forbidden package %q", name, p)
					}
				}
				return true
			})
		}
	}
}
