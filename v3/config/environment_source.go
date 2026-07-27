package config

import (
    "bufio"
    "errors"
    "io/fs"
    "path/filepath"
    "sort"
    "strings"
    "unicode"

    "github.com/joho/godotenv"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const (
    /* the two markers stand in for a dollar while the file goes through godotenv, which would otherwise expand it against that one file's keys. Both are wrapped in NUL so no value a human types can collide with them, and neither contains a dollar, a backslash or a quote, so godotenv's own escape and quote handling passes them through untouched. */
    dollarReferenceMarker = "\x00melodyDotEnvReference\x00"
    dollarLiteralMarker   = "\x00melodyDotEnvLiteralDollar\x00"
)

var (
    dollarReferenceMarkerRunes = []rune(dollarReferenceMarker)
    dollarLiteralMarkerRunes   = []rune(dollarLiteralMarker)
)

func NewEnvironmentSource(
    fileSystem fs.FS,
    baseDir string,
) *EnvironmentSource {
    return &EnvironmentSource{
        fileSystem: fileSystem,
        baseDir:    baseDir,
    }
}

type EnvironmentSource struct {
    fileSystem fs.FS
    baseDir    string
}

func (instance *EnvironmentSource) Load() (map[string]string, error) {
    values := make(map[string]string)

    environmentName, loadDotEnvFilesErr := instance.loadDotEnvFiles(values)
    if nil != loadDotEnvFilesErr {
        return nil, loadDotEnvFilesErr
    }

    loadDotEnvEnvironmentFilesErr := instance.loadDotEnvEnvironmentFiles(values, environmentName)
    if nil != loadDotEnvEnvironmentFilesErr {
        return nil, loadDotEnvEnvironmentFilesErr
    }

    expandDotEnvReferencesErr := expandDotEnvReferences(values)
    if nil != expandDotEnvReferencesErr {
        return nil, expandDotEnvReferencesErr
    }

    return values, nil
}

func (instance *EnvironmentSource) loadDotEnvFiles(values map[string]string) (string, error) {
    dotEnvPath := filepath.Join(instance.baseDir, ".env")
    loadOptionalDotEnvFileErr := instance.loadOptionalDotEnvFile(values, dotEnvPath)
    if nil != loadOptionalDotEnvFileErr {
        return "", loadOptionalDotEnvFileErr
    }

    dotEnvLocalPath := filepath.Join(instance.baseDir, ".env.local")
    loadOptionalDotEnvFileErr = instance.loadOptionalDotEnvFile(values, dotEnvLocalPath)
    if nil != loadOptionalDotEnvFileErr {
        return "", loadOptionalDotEnvFileErr
    }

    _, exists := values[EnvKey]
    if false == exists {
        return EnvDevelopment, nil
    }

    /* the environment name picks the next two files to load, so a reference inside it has to be resolved now, against the two files already read — nothing later can be visible to it. The whole set is resolved again once every file is merged, which is where this key gets its final value. */
    environmentValue, expandErr := expandDotEnvValue(
        EnvKey,
        values,
        make(map[string]string, 1),
        make(map[string]bool, 1),
    )
    if nil != expandErr {
        return "", expandErr
    }

    environmentName := strings.TrimSpace(environmentValue)
    if "" == environmentName {
        return EnvDevelopment, nil
    }

    return environmentName, nil
}

func (instance *EnvironmentSource) loadDotEnvEnvironmentFiles(
    values map[string]string,
    environmentName string,
) error {
    baseName := ".env." + environmentName

    environmentPath := filepath.Join(instance.baseDir, baseName)
    loadOptionalDotEnvFileErr := instance.loadOptionalDotEnvFile(values, environmentPath)
    if nil != loadOptionalDotEnvFileErr {
        return loadOptionalDotEnvFileErr
    }

    environmentLocalPath := filepath.Join(instance.baseDir, baseName+".local")
    loadOptionalDotEnvFileErr = instance.loadOptionalDotEnvFile(values, environmentLocalPath)
    if nil != loadOptionalDotEnvFileErr {
        return loadOptionalDotEnvFileErr
    }

    return nil
}

func (instance *EnvironmentSource) loadRequiredDotEnvFile(values map[string]string, pathValue string) error {
    _, err := fs.Stat(instance.fileSystem, pathValue)
    if nil != err {
        if true == errors.Is(err, fs.ErrNotExist) {
            return exception.NewError(
                "env file is required but was not found",
                exceptioncontract.Context{
                    "path": pathValue,
                },
                err,
            )
        }

        return exception.NewError(
            "failed to stat env file",
            exceptioncontract.Context{
                "path": pathValue,
            },
            err,
        )
    }

    return instance.loadExistingDotEnvFile(values, pathValue)
}

func (instance *EnvironmentSource) loadOptionalDotEnvFile(values map[string]string, pathValue string) error {
    _, err := fs.Stat(instance.fileSystem, pathValue)
    if nil != err {
        if true == errors.Is(err, fs.ErrNotExist) {
            return nil
        }

        return exception.NewError(
            "failed to stat env file",
            exceptioncontract.Context{
                "path": pathValue,
            },
            err,
        )
    }

    return instance.loadExistingDotEnvFile(values, pathValue)
}

func (instance *EnvironmentSource) loadExistingDotEnvFile(values map[string]string, pathValue string) error {
    data, readFileErr := fs.ReadFile(instance.fileSystem, pathValue)
    if nil != readFileErr {
        return exception.NewError(
            "failed to read env file",
            exceptioncontract.Context{
                "path": pathValue,
            },
            readFileErr,
        )
    }

    preprocessed, preprocessDotEnvContentErr := preprocessDotEnvContent(string(data))
    if nil != preprocessDotEnvContentErr {
        return exception.NewError(
            "failed to preprocess env file",
            exceptioncontract.Context{
                "path": pathValue,
            },
            preprocessDotEnvContentErr,
        )
    }

    parsed, parseErr := godotenv.Parse(strings.NewReader(preprocessed))
    if nil != parseErr {
        return exception.NewError(
            "failed to parse env file",
            exceptioncontract.Context{
                "path": pathValue,
            },
            parseErr,
        )
    }

    for key, value := range parsed {
        trimmedKey := strings.TrimSpace(key)
        if "" == trimmedKey {
            continue
        }

        values[trimmedKey] = value
    }

    return nil
}

/* expandDotEnvReferences resolves the ${KEY} and $KEY references of every loaded .env artifact at once, over the merged set. The parser resolves them per file, which is what makes the four-file layout misfire so quietly: .env holds the credential, .env.local assembles the connection string that reads it, and the reference — invisible to the file it sits in — becomes the empty string, so the application boots against "postgres://:@db/app" with nothing logged. Here a reference that names no key fails the boot, the same rule %env(KEY)% already follows. A dollar the file escaped with a backslash is data and is written out as a plain dollar. */
func expandDotEnvReferences(values map[string]string) error {
    resolved := make(map[string]string, len(values))
    resolving := make(map[string]bool, len(values))

    keys := make([]string, 0, len(values))
    for key := range values {
        keys = append(keys, key)
    }

    /* a stable order so the boot fails on the same reference every time when a file carries several broken ones */
    sort.Strings(keys)

    for _, key := range keys {
        _, expandErr := expandDotEnvValue(key, values, resolved, resolving)
        if nil != expandErr {
            return expandErr
        }
    }

    for key, value := range resolved {
        values[key] = value
    }

    return nil
}

/* expandDotEnvValue resolves one key's references and memoizes the result. A referenced value is resolved first and spliced in as data, never rescanned, so a password that happens to hold a dollar survives being read through a reference. The resolving set is what turns a key that reads itself, and any ring of keys that read each other, into a named error instead of an endless recursion. */
func expandDotEnvValue(
    key string,
    values map[string]string,
    resolved map[string]string,
    resolving map[string]bool,
) (string, error) {
    if value, exists := resolved[key]; true == exists {
        return value, nil
    }

    rawValue, exists := values[key]
    if false == exists {
        return "", exception.NewError(
            "undefined key referenced in env file",
            exceptioncontract.Context{
                "key": key,
            },
            nil,
        )
    }

    resolving[key] = true
    defer delete(resolving, key)

    var builder strings.Builder

    remaining := rawValue
    for 0 < len(remaining) {
        literalOffset := strings.Index(remaining, dollarLiteralMarker)
        referenceOffset := strings.Index(remaining, dollarReferenceMarker)

        if 0 > literalOffset && 0 > referenceOffset {
            builder.WriteString(remaining)

            break
        }

        if 0 <= literalOffset && (0 > referenceOffset || literalOffset < referenceOffset) {
            builder.WriteString(remaining[:literalOffset])
            builder.WriteByte('$')

            remaining = remaining[literalOffset+len(dollarLiteralMarker):]

            continue
        }

        builder.WriteString(remaining[:referenceOffset])

        fragment := remaining[referenceOffset+len(dollarReferenceMarker):]

        referencedKey, consumedLength := parseDotEnvReference(fragment)
        if 0 == consumedLength {
            /* nothing name-shaped follows, so the dollar was data — a lone one at the end of a value, or one in front of a character no key may start with */
            builder.WriteByte('$')

            remaining = fragment

            continue
        }

        if key == referencedKey {
            return "", exception.NewError(
                "env file key references itself",
                exceptioncontract.Context{
                    "key": key,
                },
                nil,
            )
        }

        if true == resolving[referencedKey] {
            return "", exception.NewError(
                "circular reference between env file keys",
                exceptioncontract.Context{
                    "key":           key,
                    "referencedKey": referencedKey,
                },
                nil,
            )
        }

        if _, referencedExists := values[referencedKey]; false == referencedExists {
            /* @important the offending value is not reported: it commonly holds an inline credential, and naming the two keys is enough to find it */
            return "", exception.NewError(
                "undefined key referenced in env file; write a literal dollar as \\$",
                exceptioncontract.Context{
                    "key":           key,
                    "referencedKey": referencedKey,
                },
                nil,
            )
        }

        referencedValue, referencedErr := expandDotEnvValue(referencedKey, values, resolved, resolving)
        if nil != referencedErr {
            return "", referencedErr
        }

        builder.WriteString(referencedValue)

        remaining = fragment[consumedLength:]
    }

    value := builder.String()
    resolved[key] = value

    return value, nil
}

/* parseDotEnvReference reads the key name a reference marker opens, in either the braced or the bare form, and reports how much of the fragment it consumed. Zero means the marker opened no reference and the dollar it stood for is data. */
func parseDotEnvReference(fragment string) (string, int) {
    if 0 == len(fragment) {
        return "", 0
    }

    if '{' == fragment[0] {
        end := 1
        for end < len(fragment) && '}' != fragment[end] {
            end = end + 1
        }

        if end >= len(fragment) {
            return "", 0
        }

        name := fragment[1:end]
        if false == isDotEnvKeyName(name) {
            return "", 0
        }

        return name, end + 1
    }

    end := 0
    for end < len(fragment) && true == isDotEnvKeyNameCharacter(fragment[end]) {
        end = end + 1
    }

    name := fragment[:end]
    if false == isDotEnvKeyName(name) {
        return "", 0
    }

    return name, end
}

func isDotEnvKeyName(name string) bool {
    if 0 == len(name) {
        return false
    }

    if false == isDotEnvKeyNameStartCharacter(name[0]) {
        return false
    }

    for index := 1; index < len(name); index = index + 1 {
        if false == isDotEnvKeyNameCharacter(name[index]) {
            return false
        }
    }

    return true
}

/* the name a reference may carry is exactly what godotenv expands, upper case and digits and underscore, and nothing else. It is deliberately narrower than what a key name may be, because the two questions are different: this one decides whether a dollar in a VALUE opens a reference at all, and every character admitted here is a character that stops being data.

Admitting lower case cost a literal that had always worked. `DB_PASSWORD=pa$sword` is a password, not a reference to a key named `sword` — but with lower case admitted it parses as one, no such key exists, and the boot fails on a value that had been read literally for as long as the file existed. The dot cost the same for a value such as `LABEL=$1.50` once a digit followed. godotenv's own expansion (`parser.go`, `expandVarRegex`) admits `[A-Z0-9_]` for exactly this reason, and matching it is what keeps a value that godotenv read as data reading as data here. */
func isDotEnvKeyNameStartCharacter(character byte) bool {
    return ('A' <= character && 'Z' >= character) || '_' == character
}

func isDotEnvKeyNameCharacter(character byte) bool {
    return true == isDotEnvKeyNameStartCharacter(character) || ('0' <= character && '9' >= character)
}

func preprocessDotEnvContent(content string) (string, error) {
    /* an editor that saves the file as UTF-8 with a byte order mark puts U+FEFF before the first key; it is not whitespace, so nothing downstream trims it and godotenv rejects the line as a malformed variable name */
    content = strings.TrimPrefix(content, "\ufeff")

    scanner := bufio.NewScanner(strings.NewReader(content))
    scanner.Buffer(
        make([]byte, 0, 64*1024),
        1024*1024,
    )

    lines := make([]string, 0)

    /* the quote state spans lines: godotenv accepts a quoted value that runs over several of them, and a scanner that forgot it was inside quotes would read a '#' in the value as a comment and drop a blank line out of the middle of it */
    inQuotes := false
    var quoteChar rune = 0

    for scanner.Scan() {
        line := scanner.Text()

        /* the produced line is collected as runes rather than into a builder because the dollar handling below has to take the preceding backslash back out again once it turns out to have been escaping the dollar */
        output := make([]rune, 0, len(line))

        openedInQuotes := inQuotes
        var previousChar rune = 0

        /* godotenv opens a quoted value only when the quote is the first non-space rune of the value portion, after the key separator (its hasQuotePrefix); a quote anywhere else in an unquoted value is literal data and must not flip the cross-line quote state. A line that continues a value opened on an earlier line is entirely inside that value already. */
        sawSeparator := openedInQuotes
        valueStarted := openedInQuotes

        for _, character := range line {
            if '"' == character || '\'' == character {
                if true == inQuotes {
                    /* godotenv skips a quote preceded by a backslash, so an escaped quote inside the value does not terminate it */
                    if quoteChar == character && '\\' != previousChar {
                        inQuotes = false
                        quoteChar = 0
                    }
                } else if true == sawSeparator && false == valueStarted {
                    inQuotes = true
                    quoteChar = character
                    valueStarted = true
                }

                output = append(output, character)
                previousChar = character
                continue
            }

            if false == inQuotes {
                if '#' == character {
                    if 0 == previousChar || true == unicode.IsSpace(previousChar) {
                        break
                    }
                }

                if false == sawSeparator {
                    if '=' == character || ':' == character {
                        sawSeparator = true
                    }
                } else if false == valueStarted && false == unicode.IsSpace(character) {
                    valueStarted = true
                }
            }

            /* every dollar in a value leaves here as a marker, so godotenv sees none and expands nothing: its expansion looks only at the keys of the file being parsed, which turns a reference across the four-file layout into the empty string without a word. The markers are resolved after every file is merged, where a reference can actually be looked up. A single-quoted value is left alone because godotenv never expands one either, and a dollar the file escaped becomes the literal marker, which comes back out as a plain dollar and is never looked up. */
            if '$' == character && true == sawSeparator {
                singleQuotedValue := true == inQuotes && '\'' == quoteChar

                if false == singleQuotedValue {
                    if '\\' == previousChar && 0 < len(output) {
                        output = output[:len(output)-1]
                        output = append(output, dollarLiteralMarkerRunes...)
                    } else {
                        output = append(output, dollarReferenceMarkerRunes...)
                    }

                    previousChar = character
                    continue
                }
            }

            output = append(output, character)
            previousChar = character
        }

        processed := string(output)

        /* trailing whitespace inside an unterminated quoted value is part of the value, and a blank line there is a blank line of data — neither may be trimmed away or skipped */
        if false == inQuotes {
            processed = strings.TrimRightFunc(processed, unicode.IsSpace)
        }

        if false == inQuotes && false == openedInQuotes && "" == strings.TrimSpace(processed) {
            continue
        }

        lines = append(lines, processed)
    }

    if nil != scanner.Err() {
        return "", exception.NewError(
            "failed to scan env content",
            nil,
            scanner.Err(),
        )
    }

    return strings.Join(lines, "\n"), nil
}

var _ configcontract.EnvironmentSource = (*EnvironmentSource)(nil)
