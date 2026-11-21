package translation

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func NewJsonDirectoryLoader(directory string) *JsonDirectoryLoader {
    return &JsonDirectoryLoader{
        directory: directory,
    }
}

type JsonDirectoryLoader struct {
    directory string
}

/* Load reads every <domain>.<locale>.json of the directory under ctx, which is judged before the directory is read and before each file, so a done context answers its error and no catalog. */
func (instance *JsonDirectoryLoader) Load(ctx context.Context) ([]translationcontract.Catalog, error) {
    if nil == ctx {
        return nil, exception.NewError("the load context is nil", map[string]any{"door": "JsonDirectoryLoader.Load", "directory": instance.directory}, nil)
    }

    if contextErr := instance.refuseDoneContext(ctx); nil != contextErr {
        return nil, contextErr
    }

    entries, readErr := os.ReadDir(instance.directory)
    if nil != readErr {
        return nil, exception.NewError(
            "could not read the translations directory",
            map[string]any{"directory": instance.directory},
            readErr,
        )
    }

    catalogsByLocale := make(map[string]*MapCatalog)
    fileByLocale := make(map[string]string)

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
            /* a hard error, not a skip: a file not named <domain>.<locale>.json is almost always a typo, and skipping it would leave Translate answering raw message ids with nothing pointing at the file */
            return nil, exception.NewError(
                "translation file name does not match <domain>.<locale>.json",
                map[string]any{"file": name, "directory": instance.directory},
                nil,
            )
        }

        if contextErr := instance.refuseDoneContext(ctx); nil != contextErr {
            return nil, contextErr
        }

        payload, fileErr := os.ReadFile(filepath.Join(instance.directory, name))
        if nil != fileErr {
            return nil, exception.NewError(
                "could not read the translation file",
                map[string]any{"file": name, "directory": instance.directory},
                fileErr,
            )
        }

        var messages map[string]string
        if unmarshalErr := json.Unmarshal(payload, &messages); nil != unmarshalErr {
            return nil, exception.NewError(
                "could not parse the translation file",
                map[string]any{"file": name, "directory": instance.directory},
                unmarshalErr,
            )
        }

        /* the manager matches locales in BCP 47 case, so two files spelling one locale differently are refused here, where both files can be named */
        canonical := canonicalLocale(locale)
        catalog, exists := catalogsByLocale[canonical]
        if true == exists && catalog.Locale() != locale {
            return nil, exception.NewError(
                "translation files spell one locale two ways",
                map[string]any{"file": name, "otherFile": fileByLocale[canonical], "directory": instance.directory},
                nil,
            )
        }

        if false == exists {
            catalog = NewMapCatalog(locale)
            catalogsByLocale[canonical] = catalog
            fileByLocale[canonical] = name
        }

        messageIds := make([]string, 0, len(messages))
        for messageId := range messages {
            messageIds = append(messageIds, messageId)
        }
        sort.Strings(messageIds)

        for _, messageId := range messageIds {
            if defect := findPatternDefect([]rune(messages[messageId]), 0); nil != defect {
                context := malformedPatternContext(locale, domain, messageId, defect)
                context["file"] = name
                context["directory"] = instance.directory

                return nil, exception.NewError("translation message pattern is malformed", context, nil)
            }
        }

        for messageId, message := range messages {
            catalog.Add(domain, messageId, message)
        }
    }

    locales := make([]string, 0, len(catalogsByLocale))
    for canonical := range catalogsByLocale {
        locales = append(locales, canonical)
    }
    sort.Strings(locales)

    catalogs := make([]translationcontract.Catalog, 0, len(locales))
    for _, canonical := range locales {
        catalogs = append(catalogs, catalogsByLocale[canonical])
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

func (instance *JsonDirectoryLoader) refuseDoneContext(ctx context.Context) error {
    if contextErr := ctx.Err(); nil != contextErr {
        return exception.NewError(
            "the translations load was cancelled",
            map[string]any{"directory": instance.directory},
            contextErr,
        )
    }

    return nil
}
