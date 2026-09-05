package wiring

import (
    "os"
    "path/filepath"
    "testing"
)

/* writeFixtureFile lays one source file into a test-local project tree, creating the directories on the way. */
func writeFixtureFile(t *testing.T, projectDirectory string, relativePath string, content string) {
    t.Helper()

    fullPath := filepath.Join(projectDirectory, relativePath)
    if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); nil != mkdirErr {
        t.Fatalf("mkdir: %v", mkdirErr)
    }

    if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }
}

/* bindSetWithPackage builds a set holding one package binding, for the tests that need the set rather than the binding the fluent Package call answers with. */
func bindSetWithPackage(importPath string, directory string) *BindSet {
    bindSet := NewBindSet()
    bindSet.Package(importPath, directory)

    return bindSet
}
