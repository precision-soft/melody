package config

import (
    "bufio"
    "bytes"
    "io/fs"
    "path"
    "sort"
    "strings"
    "unicode"
    "unicode/utf8"

    "github.com/joho/godotenv"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

const (
    /* the two markers stand in for a dollar while the file goes through godotenv, which would otherwise expand it against that one file's keys; wrapped in NUL, no typed value collides with them, and with no dollar, backslash or quote in them godotenv passes them through */
    dollarReferenceMarker = "\x00melodyDotEnvReference\x00"
    dollarLiteralMarker   = "\x00melodyDotEnvLiteralDollar\x00"
)

/* EnvironmentSourceConfig names where the .env artifacts are read from: the filesystem, and the directory inside it, "" reading as its root. */
type EnvironmentSourceConfig struct {
    FileSystem    fs.FS
    BaseDirectory string
}

func NewEnvironmentSource(environmentSourceConfig EnvironmentSourceConfig) *EnvironmentSource {
    return &EnvironmentSource{
        fileSystem:    environmentSourceConfig.FileSystem,
        baseDirectory: environmentSourceConfig.BaseDirectory,
    }
}

type EnvironmentSource struct {
    fileSystem    fs.FS
    baseDirectory string
    /* the keys each .env value was assembled from through ${KEY}, filled by Load, so a secret marking reaches a value no template names; held behind a pointer so the source stays comparable */
    provenance *dotEnvProvenance
}

type dotEnvProvenance struct {
    assembledFrom map[string][]string
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

    assembledFrom, expandDotEnvReferencesErr := expandDotEnvReferences(values)
    if nil != expandDotEnvReferencesErr {
        return nil, expandDotEnvReferencesErr
    }

    refuseEnvironmentRenameErr := refuseEnvironmentRename(values, environmentName, assembledFrom[EnvironmentNameKey])
    if nil != refuseEnvironmentRenameErr {
        return nil, refuseEnvironmentRenameErr
    }

    instance.provenance = &dotEnvProvenance{assembledFrom: assembledFrom}

    return values, nil
}

/* refuseEnvironmentRename fails the load when .env.<name> or .env.<name>.local leaves MELODY_ENV naming another environment than the one that selected them: the process would run the selected files under another name, the production files as dev with the debug tooling on. The name is compared once the references are expanded, and an absent or empty value reads as dev, as it does when the files are selected. A name assembled from other keys is reported by those keys, never by the value they gave it. */
func refuseEnvironmentRename(values map[string]string, environmentName string, assembledFrom []string) error {
    overwritingName := strings.TrimSpace(values[EnvironmentNameKey])

    finalName := overwritingName
    if "" == finalName {
        finalName = EnvironmentDevelopment
    }

    if environmentName == finalName {
        return nil
    }

    context := exceptioncontract.Context{
        "environmentKey":      EnvironmentNameKey,
        "selectedEnvironment": environmentName,
        "environmentFiles":    []string{".env." + environmentName, ".env." + environmentName + ".local"},
    }

    if 0 < len(assembledFrom) {
        context["overwritingEnvironmentAssembledFrom"] = assembledFrom
    } else {
        context["overwritingEnvironment"] = overwritingName
    }

    return exception.NewError(
        "a per-environment env file renames the environment that selected it; MELODY_ENV in .env or .env.local selects the .env.<name> files, which may repeat the name but not change it, and an empty one reads as dev",
        context,
        nil,
    )
}

func (instance *EnvironmentSource) loadDotEnvFiles(values map[string]string) (string, error) {
    dotEnvPath := path.Join(instance.baseDirectory, ".env")
    loadOptionalDotEnvFileErr := instance.loadOptionalDotEnvFile(values, dotEnvPath)
    if nil != loadOptionalDotEnvFileErr {
        return "", loadOptionalDotEnvFileErr
    }

    dotEnvLocalPath := path.Join(instance.baseDirectory, ".env.local")
    loadOptionalDotEnvFileErr = instance.loadOptionalDotEnvFile(values, dotEnvLocalPath)
    if nil != loadOptionalDotEnvFileErr {
        return "", loadOptionalDotEnvFileErr
    }

    _, exists := values[EnvironmentNameKey]
    if false == exists {
        return EnvironmentDevelopment, nil
    }

    /* the environment name picks the next two files to load, so a reference inside it is resolved now, against the two files already read; the whole set is resolved again once every file is merged */
    environmentValue, expandErr := expandDotEnvValue(
        EnvironmentNameKey,
        values,
        make(map[string]string, 1),
        make(map[string]bool, 1),
        nil,
    )
    if nil != expandErr {
        /* a reference in MELODY_ENV can see only .env and .env.local, since the value picks which .env.<name> files load next; the error says so */
        return "", exception.NewError(
            "could not resolve the MELODY_ENV value against .env and .env.local; the environment name selects the .env.<name> files, so a reference in it can only read keys those two files define",
            nil,
            expandErr,
        )
    }

    environmentName := strings.TrimSpace(environmentValue)
    if "" == environmentName {
        return EnvironmentDevelopment, nil
    }

    return environmentName, nil
}

func (instance *EnvironmentSource) loadDotEnvEnvironmentFiles(
    values map[string]string,
    environmentName string,
) error {
    baseName := ".env." + environmentName

    environmentPath := path.Join(instance.baseDirectory, baseName)
    loadOptionalDotEnvFileErr := instance.loadOptionalDotEnvFile(values, environmentPath)
    if nil != loadOptionalDotEnvFileErr {
        return loadOptionalDotEnvFileErr
    }

    environmentLocalPath := path.Join(instance.baseDirectory, baseName+".local")
    loadOptionalDotEnvFileErr = instance.loadOptionalDotEnvFile(values, environmentLocalPath)
    if nil != loadOptionalDotEnvFileErr {
        return loadOptionalDotEnvFileErr
    }

    return nil
}

func (instance *EnvironmentSource) loadOptionalDotEnvFile(values map[string]string, pathValue string) error {
    _, err := fs.Stat(instance.fileSystem, pathValue)
    if nil != err {
        if true == exception.ChainIs(err, fs.ErrNotExist) {
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

    /* the parser is handed content that ends with a newline, as a file does: the preprocessor joins its lines without one, and the parser read a last line without a separator as the value of an empty key, whether the file ended with a newline or not, where on every other line it is the error it is, so the line vanished */
    parsed, parseErr := godotenv.Parse(strings.NewReader(preprocessed + "\n"))
    if nil != parseErr {
        /* the parser's error is not the cause: godotenv quotes the file content it choked on, where the credentials live; only a content-free description travels, and the path names the file */
        return exception.NewError(
            "failed to parse env file",
            exceptioncontract.Context{
                "path":         pathValue,
                "parseFailure": sanitizeDotEnvParseFailure(parseErr),
            },
            nil,
        )
    }

    refuseRewrittenValueErr := refuseRewrittenDotEnvValue(data, parsed, pathValue)
    if nil != refuseRewrittenValueErr {
        return refuseRewrittenValueErr
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

/* refuseRewrittenDotEnvValue fails a file whose bytes are not valid UTF-8 when godotenv rewrote such a byte to U+FFFD in a value, which it does for an unquoted value and never for a quoted one: the parameter would hold another value than the file, a Latin-1 password among them. The parser's own reading is judged, so a comment, a second statement on a line or a key's spelling follow godotenv's grammar; a value the file spells verbatim, a U+FFFD typed into it, is no rewrite. The key and the file are named and the value never is. */
func refuseRewrittenDotEnvValue(data []byte, parsed map[string]string, pathValue string) error {
    if true == utf8.Valid(data) {
        return nil
    }

    rewrittenKeys := make([]string, 0)
    for key, value := range parsed {
        if false == strings.ContainsRune(value, utf8.RuneError) || true == bytes.Contains(data, []byte(value)) {
            continue
        }

        rewrittenKeys = append(rewrittenKeys, strings.TrimSpace(key))
    }

    if 0 == len(rewrittenKeys) {
        return nil
    }

    sort.Strings(rewrittenKeys)

    return exception.NewError(
        "an unquoted env value is not valid UTF-8; quote it, since a quoted value is kept byte for byte",
        exceptioncontract.Context{
            "path": pathValue,
            "keys": rewrittenKeys,
        },
        nil,
    )
}

/* sanitizeDotEnvParseFailure keeps the failure's shape and drops the file content it quotes. The unterminated-value failure is read by its prefix before the " near " cut, since godotenv appends the value's first line to it raw; the malformed-name failure carries " near " ahead of the content it quotes. */
func sanitizeDotEnvParseFailure(parseErr error) string {
    message := parseErr.Error()

    if true == strings.HasPrefix(message, "unterminated quoted value") {
        return "unterminated quoted value"
    }

    if index := strings.Index(message, " near "); 0 <= index {
        return message[:index]
    }

    return "env file content did not parse"
}

func (instance *EnvironmentSource) dotEnvAssembledFrom() map[string][]string {
    if nil == instance.provenance {
        return nil
    }

    return instance.provenance.assembledFrom
}

/* expandDotEnvReferences resolves the ${KEY} and $KEY references of every loaded .env artifact over the merged set, since the parser alone resolves them per file and a reference across files would become the empty string. A reference that names no key fails the boot, as %env(KEY)% does; a dollar escaped with a backslash is data. */
func expandDotEnvReferences(values map[string]string) (map[string][]string, error) {
    assembledFrom := make(map[string][]string)
    resolved := make(map[string]string, len(values))
    resolving := make(map[string]bool, len(values))

    keys := make([]string, 0, len(values))
    for key := range values {
        keys = append(keys, key)
    }

    /* sorted, so the boot fails on the same reference every time */
    sort.Strings(keys)

    for _, key := range keys {
        _, expandErr := expandDotEnvValue(key, values, resolved, resolving, assembledFrom)
        if nil != expandErr {
            return nil, expandErr
        }
    }

    for key, value := range resolved {
        values[key] = value
    }

    return assembledFrom, nil
}

/* expandDotEnvValue resolves one key's references and memoizes the result. A referenced value is spliced in as data, never rescanned, so a password holding a dollar survives. The resolving set turns a key that reads itself, or a ring of keys, into a named error. */
func expandDotEnvValue(
    key string,
    values map[string]string,
    resolved map[string]string,
    resolving map[string]bool,
    assembledFrom map[string][]string,
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

        referencedKey, consumedLength, malformedBracedReference := parseDotEnvReference(fragment)
        if 0 == consumedLength {
            /* the braced content is not reported: it may hold a pasted credential, and the enclosing key is enough to find it */
            if true == malformedBracedReference {
                return "", exception.NewError(
                    "malformed reference in env file value; ${...} must name a key of upper case letters, digits and underscores, and a literal dollar is written as \\$",
                    exceptioncontract.Context{
                        "key": key,
                    },
                    nil,
                )
            }

            /* nothing name-shaped follows, so the dollar is data */
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
            /* the value is not reported: it commonly holds an inline credential, and the two keys are enough to find it */
            return "", exception.NewError(
                "undefined key referenced in env file; write a literal dollar as \\$",
                exceptioncontract.Context{
                    "key":           key,
                    "referencedKey": referencedKey,
                },
                nil,
            )
        }

        referencedValue, referencedErr := expandDotEnvValue(referencedKey, values, resolved, resolving, assembledFrom)
        if nil != referencedErr {
            return "", referencedErr
        }

        if nil != assembledFrom {
            assembledFrom[key] = append(assembledFrom[key], referencedKey)
        }

        builder.WriteString(referencedValue)

        remaining = fragment[consumedLength:]
    }

    value := builder.String()
    resolved[key] = value

    return value, nil
}

/* parseDotEnvReference reads the key name a reference marker opens, braced or bare, and reports how much of the fragment it consumed. Zero means the dollar is data, except for a braced form closed over a name outside the key grammar, which raises the malformed flag and is refused as a misspelled %env(...)% is. An unclosed brace stays data. */
func parseDotEnvReference(fragment string) (string, int, bool) {
    if 0 == len(fragment) {
        return "", 0, false
    }

    if '{' == fragment[0] {
        end := 1
        for end < len(fragment) && '}' != fragment[end] {
            end = end + 1
        }

        if end >= len(fragment) {
            return "", 0, false
        }

        name := fragment[1:end]
        if false == isDotEnvKeyName(name) {
            return "", 0, true
        }

        return name, end + 1, false
    }

    end := 0
    for end < len(fragment) && true == isDotEnvKeyNameCharacter(fragment[end]) {
        end = end + 1
    }

    name := fragment[:end]
    if false == isDotEnvKeyName(name) {
        return "", 0, false
    }

    return name, end, false
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

/* the name a reference may carry is exactly what godotenv expands, [A-Z0-9_]: this decides whether a dollar in a value opens a reference at all, so `DB_PASSWORD=pa$sword` and `LABEL=$1.50` stay the data godotenv reads them as. */
func isDotEnvKeyNameStartCharacter(character byte) bool {
    return ('A' <= character && 'Z' >= character) || '_' == character
}

func isDotEnvKeyNameCharacter(character byte) bool {
    return true == isDotEnvKeyNameStartCharacter(character) || ('0' <= character && '9' >= character)
}

func preprocessDotEnvContent(content string) (string, error) {
    /* an editor saving UTF-8 with a byte order mark puts U+FEFF before the first key; it is not whitespace, and godotenv would reject the line */
    content = strings.TrimPrefix(content, "\ufeff")

    scanner := bufio.NewScanner(strings.NewReader(content))
    scanner.Buffer(
        make([]byte, 0, 64*1024),
        1024*1024,
    )

    lines := make([]string, 0)

    /* the quote state spans lines, as godotenv accepts a quoted value over several of them, so a '#' or a blank line inside such a value stays data */
    inQuotes := false
    var quoteChar byte = 0

    for scanner.Scan() {
        line := scanner.Text()

        /* the line is walked byte by byte, never through runes: a rune round-trip would rewrite bytes that are not valid UTF-8, a Latin-1 password among them. The line is collected into a slice, so the dollar handling below can take back a backslash that escaped the dollar. */
        output := make([]byte, 0, len(line))

        openedInQuotes := inQuotes
        var previousChar byte = 0

        /* godotenv opens a quoted value only when the quote is the first non-space byte after the separator; a quote elsewhere is data and must not flip the cross-line state */
        sawSeparator := openedInQuotes
        valueStarted := openedInQuotes

        for index := 0; index < len(line); index = index + 1 {
            character := line[index]

            if '"' == character || '\'' == character {
                if true == inQuotes {
                    /* godotenv skips a quote preceded by a backslash, so an escaped quote does not terminate the value */
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
                /* a comment before any separator comments the whole line out; after it, godotenv's own countback decides where the value ends, except for an empty value, whose leading '#' the countback skips. That one is cut here, under the same rule, a '#' preceded by a space or by nothing, so "KEY=#glued" stays data. */
                if '#' == character && (false == sawSeparator || false == valueStarted) {
                    if 0 == previousChar || true == isDotEnvSpaceByte(previousChar) {
                        break
                    }
                }

                if false == sawSeparator {
                    if '=' == character || ':' == character {
                        sawSeparator = true
                    }
                } else if false == valueStarted && false == isDotEnvSpaceByte(character) {
                    valueStarted = true
                }
            }

            /* every dollar in a value leaves here as a marker, so godotenv expands nothing and the references are resolved over the merged files; a single-quoted value is left alone, as godotenv leaves it, and an escaped dollar becomes the literal marker */
            if '$' == character && true == sawSeparator {
                singleQuotedValue := true == inQuotes && '\'' == quoteChar

                if false == singleQuotedValue {
                    if '\\' == previousChar && 0 < len(output) {
                        output = output[:len(output)-1]
                        output = append(output, dollarLiteralMarker...)
                    } else {
                        output = append(output, dollarReferenceMarker...)
                    }

                    previousChar = character
                    continue
                }
            }

            output = append(output, character)
            previousChar = character
        }

        processed := string(output)

        /* inside an unterminated quoted value trailing whitespace and blank lines are data */
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

func isDotEnvSpaceByte(character byte) bool {
    return ' ' == character || '\t' == character || '\v' == character || '\f' == character || '\r' == character
}

var _ configcontract.EnvironmentSource = (*EnvironmentSource)(nil)
