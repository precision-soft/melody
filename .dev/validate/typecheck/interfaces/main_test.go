package main

import (
    "os"
    "path/filepath"
    "testing"
)

/* writeFixtureModule writes a module with one file per reading the list has to make, so each assertion below names the declaration it is about. */
func writeFixtureModule(t *testing.T) string {
    t.Helper()

    moduleDirectory := t.TempDir()
    fileMap := map[string]string{
        "go.mod": "module example.com/fixture\n\ngo 1.25\n",
        "root.go": `package fixture

import (
    "io"

    "example.com/fixture/internal/secret"
)

type RootContract interface{ Run() }

type unexportedContract interface{ Run() }

type Number interface{ ~int | ~int64 }

type Box[T any] interface{ Get() T }

type Plain struct{}

type RootAlias = RootContract

type Writer = io.Writer

type Hidden = secret.Contract

type localContract interface{ First() }

type LocalAlias = localContract

type AnonymousAlias = interface{ First() }

type EmptyAlias = interface{}
`,
        "contract/contract.go": `package contract

type Listed interface{ Call() error }
`,
        "contract/contract_test.go": `package contract

type TestOnly interface{ Call() }
`,
        "internal/secret/secret.go": `package secret

type Contract interface{ Keep() }
`,
        "nested/go.mod": "module example.com/fixture/nested\n\ngo 1.25\n",
        "nested/nested.go": `package nested

type Stranger interface{ Visit() }
`,
    }

    for relativePath, content := range fileMap {
        absolutePath := filepath.Join(moduleDirectory, relativePath)
        if mkdirErr := os.MkdirAll(filepath.Dir(absolutePath), 0o755); nil != mkdirErr {
            t.Fatalf("create %s: %v", filepath.Dir(absolutePath), mkdirErr)
        }
        if writeErr := os.WriteFile(absolutePath, []byte(content), 0o644); nil != writeErr {
            t.Fatalf("write %s: %v", absolutePath, writeErr)
        }
    }

    return moduleDirectory
}

func fixtureEnvironment() []string {
    return append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
}

/* the reading the contract list depends on, one fixture: exported only, an alias resolved, an alias of an unexported or an anonymous interface listed under the alias, internal/ out, a type-set constraint out, a generic interface in, and nothing of a test file or of a module nested beneath this one. */
func TestListInterfaces_ReadsTheExportedContractsOfTheModuleOnly(t *testing.T) {
    moduleDirectory := writeFixtureModule(t)

    entryList, listErr := listInterfaces(moduleDirectory, fixtureEnvironment())
    if nil != listErr {
        t.Fatalf("list the fixture's interfaces: %v", listErr)
    }

    fileByName := map[string]string{}
    for _, entry := range entryList {
        fileByName[entry.name] = entry.file
    }

    expectedFileByName := map[string]string{
        "AnonymousAlias":  "root.go",
        "Box":             "root.go",
        "EmptyAlias":      "root.go",
        "Hidden":          "root.go",
        "LocalAlias":      "root.go",
        "RootContract":    "root.go",
        "Writer":          "root.go",
        "contract.Listed": "contract/contract.go",
    }

    for expectedName, expectedFile := range expectedFileByName {
        file, isListed := fileByName[expectedName]
        if false == isListed {
            t.Fatalf("%s is missing from the list %v", expectedName, fileByName)
        }
        if expectedFile != file {
            t.Fatalf("%s is declared in %s, the list says %s", expectedName, expectedFile, file)
        }
    }

    if len(expectedFileByName) != len(fileByName) {
        t.Fatalf("wanted exactly %v, got %v", expectedFileByName, fileByName)
    }
}

/* apidiff reports a method of an alias's unexported interface under the interface's own name, so the entry carries it for the classification; every other entry is measured under its own name */
func TestListInterfaces_NamesTheInterfaceAnAliasIsMeasuredUnder(t *testing.T) {
    moduleDirectory := writeFixtureModule(t)

    entryList, listErr := listInterfaces(moduleDirectory, fixtureEnvironment())
    if nil != listErr {
        t.Fatalf("list the fixture's interfaces: %v", listErr)
    }

    for _, entry := range entryList {
        expectedMeasuredName := ""
        if "LocalAlias" == entry.name {
            expectedMeasuredName = "localContract"
        }

        if expectedMeasuredName != entry.measuredName {
            t.Fatalf("%s is measured under %q, the list says %q", entry.name, expectedMeasuredName, entry.measuredName)
        }
    }
}

/* each exclusion on its own, so a mutation that lets one of them in names the one it let in */
func TestListInterfaces_LeavesOutWhatNoImplementerCanReach(t *testing.T) {
    moduleDirectory := writeFixtureModule(t)

    entryList, listErr := listInterfaces(moduleDirectory, fixtureEnvironment())
    if nil != listErr {
        t.Fatalf("list the fixture's interfaces: %v", listErr)
    }

    refusedNameMap := map[string]string{
        "unexportedContract":       "an unexported interface",
        "localContract":            "an unexported interface, listed under its exported alias",
        "Number":                   "a type-set constraint",
        "RootAlias":                "an alias of an interface the module declares itself",
        "internal/secret.Contract": "an interface under internal/",
        "contract.TestOnly":        "an interface of a test file",
        "nested.Stranger":          "an interface of a module nested beneath this one",
        "Plain":                    "a struct",
    }

    for _, entry := range entryList {
        if reason, isRefused := refusedNameMap[entry.name]; true == isRefused {
            t.Fatalf("%s is listed, and it is %s", entry.name, reason)
        }
    }
}

/* a module that does not type-check gives no list: a list read off a broken package would pass a completeness check it never read */
func TestListInterfaces_RefusesAModuleThatDoesNotTypeCheck(t *testing.T) {
    moduleDirectory := t.TempDir()
    if writeErr := os.WriteFile(filepath.Join(moduleDirectory, "go.mod"), []byte("module example.com/broken\n\ngo 1.25\n"), 0o644); nil != writeErr {
        t.Fatalf("write go.mod: %v", writeErr)
    }
    if writeErr := os.WriteFile(filepath.Join(moduleDirectory, "broken.go"), []byte("package broken\n\ntype Contract interface{ Run() Missing }\n"), 0o644); nil != writeErr {
        t.Fatalf("write broken.go: %v", writeErr)
    }

    if _, listErr := listInterfaces(moduleDirectory, fixtureEnvironment()); nil == listErr {
        t.Fatalf("a module that does not type-check must not give a list")
    }
}
