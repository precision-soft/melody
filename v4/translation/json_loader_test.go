package translation

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"

    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func writeCatalogFile(t *testing.T, directory string, name string, content string) {
    t.Helper()

    if writeErr := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); nil != writeErr {
        t.Fatalf("could not write fixture %q: %v", name, writeErr)
    }
}

func catalogForLocale(catalogs []translationcontract.Catalog, locale string) translationcontract.Catalog {
    for _, catalog := range catalogs {
        if locale == catalog.Locale() {
            return catalog
        }
    }

    return nil
}

func TestJsonDirectoryLoader_LoadsDomainsAndLocales(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello", "farewell": "Bye"}`)
    writeCatalogFile(t, directory, "errors.en.json", `{"not_found": "Not found"}`)
    writeCatalogFile(t, directory, "messages.ro.json", `{"greeting": "Salut"}`)

    catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
    if nil != loadErr {
        t.Fatalf("unexpected load error: %v", loadErr)
    }

    if 2 != len(catalogs) {
        t.Fatalf("expected 2 locale catalogs, got %d", len(catalogs))
    }

    english := catalogForLocale(catalogs, "en")
    if nil == english {
        t.Fatalf("missing english catalog")
    }

    if greeting, found := english.Get("greeting", "messages"); false == found || "Hello" != greeting {
        t.Fatalf("unexpected english messages.greeting: %q found=%v", greeting, found)
    }

    if notFound, found := english.Get("not_found", "errors"); false == found || "Not found" != notFound {
        t.Fatalf("unexpected english errors.not_found: %q found=%v", notFound, found)
    }

    romanian := catalogForLocale(catalogs, "ro")
    if nil == romanian {
        t.Fatalf("missing romanian catalog")
    }

    if greeting, found := romanian.Get("greeting", "messages"); false == found || "Salut" != greeting {
        t.Fatalf("unexpected romanian messages.greeting: %q found=%v", greeting, found)
    }
}

func TestJsonDirectoryLoader_IgnoresNonJsonFilesAndDirectories(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello"}`)
    writeCatalogFile(t, directory, "readme.txt", "not a catalog")
    if mkdirErr := os.Mkdir(filepath.Join(directory, "nested.en.json"), 0o750); nil != mkdirErr {
        t.Fatalf("could not create directory entry: %v", mkdirErr)
    }

    catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
    if nil != loadErr {
        t.Fatalf("unexpected load error: %v", loadErr)
    }

    if 1 != len(catalogs) {
        t.Fatalf("expected 1 catalog, got %d", len(catalogs))
    }

    if "en" != catalogs[0].Locale() {
        t.Fatalf("unexpected locale: %q", catalogs[0].Locale())
    }
}

func TestJsonDirectoryLoader_MisnamedJsonFileIsAHardError(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello"}`)
    writeCatalogFile(t, directory, "en.json", `{"orphan": "no domain"}`)

    /* a misnamed file is refused by name, not skipped */
    _, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
    if nil == loadErr {
        t.Fatalf("expected a hard error for a .json file that does not parse as <domain>.<locale>.json")
    }

    if false == strings.Contains(loadErr.Error(), "translation file name does not match") {
        t.Fatalf("expected the refusal to name the naming rule, got: %v", loadErr)
    }
}

func TestJsonDirectoryLoader_MalformedJsonReturnsError(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": `)

    _, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
    if nil == loadErr {
        t.Fatalf("expected an error for malformed json")
    }

    /* the refusal names the directory beside the bare file name, as the misnamed-file refusal does */
    melodyErr, isMelodyErr := loadErr.(*exception.Error)
    if false == isMelodyErr || directory != melodyErr.Context()["directory"] || "messages.en.json" != melodyErr.Context()["file"] {
        t.Fatalf("expected the directory beside the file, got %v", loadErr)
    }
}

func TestJsonDirectoryLoader_MissingDirectoryReturnsError(t *testing.T) {
    missing := filepath.Join(t.TempDir(), "does-not-exist")

    _, loadErr := NewJsonDirectoryLoader(missing).Load(context.Background())
    if nil == loadErr {
        t.Fatalf("expected an error for a missing directory")
    }
}

func TestJsonDirectoryLoader_NamesTheFileTheMessageAndTheOffset(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "a.en.json", `{"greeting": "Hello"}`)
    writeCatalogFile(t, directory, "errors.ro.json", `{"a_valid": "{n, plural, one {# eroare} other {# erori}}", "c_broken": "{g, select, male {el}}", "b_broken": "{n, plural, one {# eroare} other}"}`)

    /* map order is random: the first defect in sorted message ids is the one named, on every load */
    for attempt := 0; attempt < 20; attempt++ {
        catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
        if nil == loadErr {
            t.Fatalf("expected the loader to refuse a plural whose other has no block")
        }

        if nil != catalogs {
            t.Fatalf("expected no catalogs beside the refusal, the valid file read first included, got %d", len(catalogs))
        }

        context := exception.FromError(loadErr).Context()
        if "errors.ro.json" != context["file"] || directory != context["directory"] || "ro" != context["locale"] || "errors" != context["domain"] || "b_broken" != context["messageId"] || 27 != context["offset"] || patternDefectKeywordWithoutBlock != context["reason"] {
            t.Fatalf("expected the file, the directory, the locale, the domain, the message id, the offset and the reason named, got %v", context)
        }
    }
}

func TestJsonDirectoryLoader_RefusesTwoFilesSpellingOneLocaleTwoWays(t *testing.T) {
    directory := t.TempDir()
    if writeErr := os.WriteFile(filepath.Join(directory, "messages.ro-RO.json"), []byte(`{"a": "A"}`), 0o600); nil != writeErr {
        t.Fatal(writeErr)
    }
    if writeErr := os.WriteFile(filepath.Join(directory, "validation.ro-ro.json"), []byte(`{"b": "B"}`), 0o600); nil != writeErr {
        t.Fatal(writeErr)
    }

    _, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
    exceptionError, isExceptionError := loadErr.(*exception.Error)
    if false == isExceptionError || "validation.ro-ro.json" != exceptionError.Context()["file"] || "messages.ro-RO.json" != exceptionError.Context()["otherFile"] {
        t.Fatalf("expected both files named, got %v", loadErr)
    }
}

func TestJsonDirectoryLoader_AnswersItsCatalogsInAStableOrder(t *testing.T) {
    directory := t.TempDir()
    for _, name := range []string{"messages.ro.json", "messages.en.json", "messages.de.json"} {
        if writeErr := os.WriteFile(filepath.Join(directory, name), []byte(`{"a": "A"}`), 0o600); nil != writeErr {
            t.Fatal(writeErr)
        }
    }

    for attempt := 0; attempt < 20; attempt++ {
        catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(context.Background())
        if nil != loadErr || 3 != len(catalogs) || "de" != catalogs[0].Locale() || "en" != catalogs[1].Locale() || "ro" != catalogs[2].Locale() {
            t.Fatalf("expected de, en, ro, got %v (%v)", catalogs, loadErr)
        }
    }
}

/* cancelledAfterContext answers no error for its first allowed calls to Err and context.Canceled after them, so a test can end the context between two files without a hook inside the loader. */
type cancelledAfterContext struct {
    context.Context
    allowed int
    calls   int
}

func (instance *cancelledAfterContext) Err() error {
    instance.calls++
    if instance.calls <= instance.allowed {
        return nil
    }

    return context.Canceled
}

func TestJsonDirectoryLoader_RefusesADoneContext(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello"}`)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(ctx)
    if false == errors.Is(loadErr, context.Canceled) || nil != catalogs {
        t.Fatalf("expected context.Canceled and no catalog, got %v, %v", catalogs, loadErr)
    }

    if directory != exception.FromError(loadErr).Context()["directory"] {
        t.Fatalf("expected the directory named, got %v", exception.FromError(loadErr).Context())
    }

    missingDirectory := filepath.Join(directory, "missing")
    _, missingErr := NewJsonDirectoryLoader(missingDirectory).Load(ctx)
    if false == errors.Is(missingErr, context.Canceled) || missingDirectory != exception.FromError(missingErr).Context()["directory"] {
        t.Fatalf("expected the done context refused before the directory is read, got %v", missingErr)
    }

    _, nilErr := NewJsonDirectoryLoader(directory).Load(nil)
    if nil == nilErr || false == strings.Contains(nilErr.Error(), "the load context is nil") {
        t.Fatalf("expected a nil context refused, got %v", nilErr)
    }
}

func TestJsonDirectoryLoader_StopsBetweenTwoFilesWhenTheContextEnds(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello"}`)
    writeCatalogFile(t, directory, "messages.ro.json", `{"greeting": "Salut"}`)

    ctx := &cancelledAfterContext{Context: context.Background(), allowed: 2}

    catalogs, loadErr := NewJsonDirectoryLoader(directory).Load(ctx)
    if false == errors.Is(loadErr, context.Canceled) || nil != catalogs {
        t.Fatalf("expected the load stopped before the second file with no partial catalog, got %v, %v", catalogs, loadErr)
    }

    if 3 != ctx.calls {
        t.Fatalf("expected the context judged before the directory and before each file, got %d calls", ctx.calls)
    }
}
