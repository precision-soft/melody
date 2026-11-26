package lock

import (
    "go/ast"
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestPackage_ExportsNoRuntimeBoundView(t *testing.T) {
    for _, directory := range []string{".", "contract"} {
        entries, readErr := os.ReadDir(directory)
        if nil != readErr {
            t.Fatalf("read %s: %v", directory, readErr)
        }

        for _, entry := range entries {
            if false == strings.HasSuffix(entry.Name(), ".go") || true == strings.HasSuffix(entry.Name(), "_test.go") {
                continue
            }

            path := filepath.Join(directory, entry.Name())
            parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
            if nil != parseErr {
                t.Fatalf("parse %s: %v", path, parseErr)
            }

            for _, declaration := range parsed.Decls {
                switch typed := declaration.(type) {
                case *ast.FuncDecl:
                    if nil == typed.Recv && ("For" == typed.Name.Name || "FromRuntime" == typed.Name.Name) {
                        t.Fatalf("expected the lock to export no bound view, %s declares %s", path, typed.Name.Name)
                    }
                case *ast.GenDecl:
                    for _, specification := range typed.Specs {
                        typeSpecification, isType := specification.(*ast.TypeSpec)
                        if true == isType && true == strings.HasPrefix(typeSpecification.Name.Name, "Bound") {
                            t.Fatalf("expected the lock to export no bound view, %s declares %s", path, typeSpecification.Name.Name)
                        }
                    }
                }
            }
        }
    }
}
