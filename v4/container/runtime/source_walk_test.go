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

/* an accessor a provider reads stays resolver-only, and one whose bound view is still to come waits for it; every other named accessor has a runtime form beside it */
var accessorWithoutRuntimeForm = map[string]string{
    "v4/container:MustFromResolver":                                 "generic door, its runtime form is runtime.MustFromRuntime",
    "v4/container:MustFromResolverByType":                           "generic door, by type, resolved through runtime.MustFromRuntime by name",
    "v4/cache:CacheBackendMustFromResolver":                         "provider",
    "v4/cache:CacheSerializerMustFromResolver":                      "provider",
    "v4/messagebus:ConsumeBusFromResolver":                          "provider",
    "v4/messagebus:TransportsMustFromResolver":                      "provider",
    "v4/messagebus:RetryPolicyFromResolver":                         "provider",
    "v4/messagebus:HandlerLocatorMustFromResolver":                  "provider",
    "v4/session:SessionStorageMustFromResolver":                     "provider",
    "v4/openapi:RegistryMustFromResolver":                           "provider",
    "v4/openapi:InfoFromResolver":                                   "provider",
    "integrations/amqp/v4:TransportMustFromResolver":                "provider",
    "integrations/outbox/v4:RelayMustFromResolver":                  "provider",
    "integrations/outbox/v4:NewRelayCommandFromResolver":            "constructor",
    "v4/cache:CacheMustFromResolver":                                "view",
    "v4/storage:StorageMustFromResolver":                            "view",
    "integrations/rueidis/v4:TokenStoreMustFromResolver":            "view",
    "integrations/rueidis/v4:EpochRevocableTokenStoreMustFromResolver": "view",
}

func TestSourceWalk_EveryNamedResolverAccessorHasARuntimeFormOrADecision(t *testing.T) {
    /* a generic accessor and a suffixed one count too; the twin is looked for in the same file */
    accessor := regexp.MustCompile(`(?m)^func ([A-Z][A-Za-z0-9]*)FromResolver([A-Za-z0-9]*)[\[(]`)
    seen := map[string]bool{}
    _, integrationErr := os.Stat(filepath.Join("..", "..", "..", "integrations"))

    for _, source := range fourthMajorSourceList(t) {
        slashed := filepath.ToSlash(source)
        if true == strings.HasSuffix(source, "_test.go") || true == strings.Contains(slashed, "/.example/") {
            continue
        }

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        directory := filepath.ToSlash(filepath.Dir(source))
        if true == strings.HasPrefix(directory, "../../../integrations/") {
            directory = strings.TrimPrefix(directory, "../../../")
        } else {
            directory = "v4/" + strings.TrimPrefix(directory, "../../")
        }

        for _, match := range accessor.FindAllSubmatch(content, -1) {
            stem := string(match[1])
            suffix := string(match[2])
            key := directory + ":" + stem + "FromResolver" + suffix
            seen[key] = true

            hasTwin := regexp.MustCompile(`(?m)^func ` + stem + `FromRuntime` + suffix + `[\[(]`).Match(content)

            if _, decided := accessorWithoutRuntimeForm[key]; true == decided {
                if true == hasTwin {
                    t.Errorf("%s has a runtime form now; drop it from the list", key)
                }

                continue
            }

            if false == hasTwin {
                t.Errorf("%s declares %sFromResolver%s without %sFromRuntime%s beside it; add the runtime form, or list it as provider-only or as waiting for its view", source, stem, suffix, stem, suffix)
            }
        }
    }

    for key := range accessorWithoutRuntimeForm {
        /* a tree without the integrations cannot judge their decisions */
        if nil != integrationErr && true == strings.HasPrefix(key, "integrations/") {
            continue
        }

        if false == seen[key] {
            t.Errorf("the decision for %s names no accessor any more; drop it from the list", key)
        }
    }
}
