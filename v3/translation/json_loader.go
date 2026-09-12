package translation

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

func NewJsonDirectoryLoader(directory string) *JsonDirectoryLoader {
    return &JsonDirectoryLoader{
        directory: directory,
    }
}

type JsonDirectoryLoader struct {
    directory string
}

func (instance *JsonDirectoryLoader) Load() ([]translationcontract.Catalog, error) {
    entries, readErr := os.ReadDir(instance.directory)
    if nil != readErr {
        return nil, exception.NewError(
            "could not read the translations directory",
            map[string]any{"directory": instance.directory},
            readErr,
        )
    }

    catalogsByLocale := make(map[string]*MapCatalog)

    for _, entry := range entries {
        if true == entry.IsDir() {
            continue
        }

        name := entry.Name()
        if false == strings.HasSuffix(name, ".json") {
            continue
        }

        domain, locale, ok := parseCatalogFileName(name)
        if false == ok {
            /* a hard error, not a skip: a .json file in the translations directory that does not parse as <domain>.<locale>.json is almost always a naming typo (en.json, messages_en.json), and skipping it loads a catalog with that file's translations silently absent — the runtime symptom is Trans returning raw message ids, which points nowhere near the mis-named file. Every other failure in this loader is already a hard error naming the file. */
            return nil, exception.NewError(
                "translation file name does not match <domain>.<locale>.json",
                map[string]any{"file": name, "directory": instance.directory},
                nil,
            )
        }

        payload, fileErr := os.ReadFile(filepath.Join(instance.directory, name))
        if nil != fileErr {
            return nil, exception.NewError(
                "could not read the translation file",
                map[string]any{"file": name},
                fileErr,
            )
        }

        var messages map[string]string
        if unmarshalErr := json.Unmarshal(payload, &messages); nil != unmarshalErr {
            return nil, exception.NewError(
                "could not parse the translation file",
                map[string]any{"file": name},
                unmarshalErr,
            )
        }

        catalog, exists := catalogsByLocale[locale]
        if false == exists {
            catalog = NewMapCatalog(locale)
            catalogsByLocale[locale] = catalog
        }

        for messageId, message := range messages {
            catalog.Add(domain, messageId, message)
        }
    }

    catalogs := make([]translationcontract.Catalog, 0, len(catalogsByLocale))
    for _, catalog := range catalogsByLocale {
        catalogs = append(catalogs, catalog)
    }

    return catalogs, nil
}

func parseCatalogFileName(name string) (string, string, bool) {
    base := strings.TrimSuffix(name, ".json")

    lastDot := strings.LastIndexByte(base, '.')
    if -1 == lastDot {
        return "", "", false
    }

    domain := base[:lastDot]
    locale := base[lastDot+1:]
    if "" == domain || "" == locale {
        return "", "", false
    }

    return domain, locale, true
}

var _ translationcontract.Loader = (*JsonDirectoryLoader)(nil)
