package translation_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/precision-soft/melody/v3/translation"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
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

    catalogs, loadErr := translation.NewJsonDirectoryLoader(directory).Load()
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

func TestJsonDirectoryLoader_IgnoresNonJsonAndUnparseableNames(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": "Hello"}`)
    writeCatalogFile(t, directory, "readme.txt", "not a catalog")
    writeCatalogFile(t, directory, "en.json", `{"orphan": "no domain"}`)
    if mkdirErr := os.Mkdir(filepath.Join(directory, "nested.en.json"), 0o750); nil != mkdirErr {
        t.Fatalf("could not create directory entry: %v", mkdirErr)
    }

    catalogs, loadErr := translation.NewJsonDirectoryLoader(directory).Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %v", loadErr)
    }

    if 1 != len(catalogs) {
        t.Fatalf("expected 1 catalog, got %d", len(catalogs))
    }

    if "en" != catalogs[0].Locale() {
        t.Fatalf("unexpected locale: %q", catalogs[0].Locale())
    }

    if _, found := catalogs[0].Get("orphan", "en"); true == found {
        t.Fatalf("domainless file should have been skipped")
    }
}

func TestJsonDirectoryLoader_MalformedJsonReturnsError(t *testing.T) {
    directory := t.TempDir()
    writeCatalogFile(t, directory, "messages.en.json", `{"greeting": `)

    _, loadErr := translation.NewJsonDirectoryLoader(directory).Load()
    if nil == loadErr {
        t.Fatalf("expected an error for malformed json")
    }
}

func TestJsonDirectoryLoader_MissingDirectoryReturnsError(t *testing.T) {
    missing := filepath.Join(t.TempDir(), "does-not-exist")

    _, loadErr := translation.NewJsonDirectoryLoader(missing).Load()
    if nil == loadErr {
        t.Fatalf("expected an error for a missing directory")
    }
}
