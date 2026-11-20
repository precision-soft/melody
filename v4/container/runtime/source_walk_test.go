package runtime

import (
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"
)

/* fourthMajorSourceList lists the Go files of the fourth major: the framework, its example and every v4 integration, the repository read from this package's directory */
func fourthMajorSourceList(t *testing.T) []string {
    t.Helper()

    sourceList := []string{}

    collect := func(root string, insideMajor func(path string) bool) {
        walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
            if nil != err {
                return err
            }

            if true == entry.IsDir() && ("vendor" == entry.Name() || "node_modules" == entry.Name() || ".git" == entry.Name()) {
                return filepath.SkipDir
            }

            if false == entry.IsDir() && true == strings.HasSuffix(path, ".go") && true == insideMajor(path) {
                sourceList = append(sourceList, path)
            }

            return nil
        })
        if nil != walkErr {
            t.Fatalf("walk %s: %v", root, walkErr)
        }
    }

    collect(filepath.Join("..", ".."), func(path string) bool { return true })

    integrationRoot := filepath.Join("..", "..", "..", "integrations")
    if _, statErr := os.Stat(integrationRoot); nil == statErr {
        collect(integrationRoot, func(path string) bool {
            return true == strings.Contains(filepath.ToSlash(path), "/v4/")
        })
    }

    if 100 > len(sourceList) {
        t.Fatalf("expected the fourth major's sources, found %d files", len(sourceList))
    }

    return sourceList
}

func TestSourceWalk_NoFileImportsTheRemovedRuntimePackages(t *testing.T) {
    removedImport := regexp.MustCompile(`"github\.com/precision-soft/melody/v4/runtime(/contract)?"`)

    for _, source := range fourthMajorSourceList(t) {
        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if true == removedImport.Match(content) {
            t.Fatalf("%s imports a removed runtime package; the runtime lives in v4/container/runtime and v4/container/contract", source)
        }
    }
}

/* a runtime rebuilt from another's scope takes a kind the caller names, where WithContext keeps the one it had */
func TestSourceWalk_NoProductionFileRebuildsARuntimeFromAnothersScope(t *testing.T) {
    rebuilt := regexp.MustCompile(`New(Request|Run)\(\s*[^,()]+(\([^()]*\))?,\s*[A-Za-z_][A-Za-z0-9_]*\.Scope\(\)\s*,?\s*\)`)

    for _, source := range fourthMajorSourceList(t) {
        if true == strings.HasSuffix(source, "_test.go") {
            continue
        }

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if match := rebuilt.Find(content); nil != match {
            t.Fatalf("%s rebuilds a runtime from another runtime's scope (%s); derive it with WithContext", source, match)
        }
    }
}

func TestSourceWalk_NoFileNamesTheRemovedResolutionContextDoors(t *testing.T) {
    /* spelled in pieces so this file does not match itself */
    removed := regexp.MustCompile(`(^|[^A-Za-z0-9_])(Context` + `Of\b|Resolution` + `Context\b)`)

    for _, source := range fourthMajorSourceList(t) {
        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if match := removed.Find(content); nil != match {
            t.Fatalf("%s names a removed door (%s); read resolver.Runtime().Context()", source, match)
        }
    }
}
