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

    dollarReferenceMarker = "\x00melodyDotEnvReference\x00"
    dollarLiteralMarker   = "\x00melodyDotEnvLiteralDollar\x00"
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

    environmentValue, expandErr := expandDotEnvValue(
        EnvKey,
        values,
        make(map[string]string, 1),
        make(map[string]bool, 1),
    )
    if nil != expandErr {

        return "", exception.NewError(
            "could not resolve the MELODY_ENV value against .env and .env.local; the environment name selects the .env.<name> files, so a reference in it can only read keys those two files define",
            nil,
            expandErr,
        )
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
                "path":         pathValue,
                "parseFailure": sanitizeDotEnvParseFailure(parseErr),
            },
            nil,
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

func sanitizeDotEnvParseFailure(parseErr error) string {
    message := parseErr.Error()

    if index := strings.Index(message, " near "); 0 <= index {
        return message[:index]
    }

    if true == strings.HasPrefix(message, "unterminated quoted value") {
        return "unterminated quoted value"
    }

    return "env file content did not parse"
}

func expandDotEnvReferences(values map[string]string) error {
    resolved := make(map[string]string, len(values))
    resolving := make(map[string]bool, len(values))

    keys := make([]string, 0, len(values))
    for key := range values {
        keys = append(keys, key)
    }

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

        referencedKey, consumedLength, malformedBracedReference := parseDotEnvReference(fragment)
        if 0 == consumedLength {

            if true == malformedBracedReference {
                return "", exception.NewError(
                    "malformed reference in env file value; ${...} must name a key of upper case letters, digits and underscores, and a literal dollar is written as \\$",
                    exceptioncontract.Context{
                        "key": key,
                    },
                    nil,
                )
            }

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

func isDotEnvKeyNameStartCharacter(character byte) bool {
    return ('A' <= character && 'Z' >= character) || '_' == character
}

func isDotEnvKeyNameCharacter(character byte) bool {
    return true == isDotEnvKeyNameStartCharacter(character) || ('0' <= character && '9' >= character)
}

func preprocessDotEnvContent(content string) (string, error) {

    content = strings.TrimPrefix(content, "\ufeff")

    scanner := bufio.NewScanner(strings.NewReader(content))
    scanner.Buffer(
        make([]byte, 0, 64*1024),
        1024*1024,
    )

    lines := make([]string, 0)

    inQuotes := false
    var quoteChar byte = 0

    for scanner.Scan() {
        line := scanner.Text()

        output := make([]byte, 0, len(line))

        openedInQuotes := inQuotes
        var previousChar byte = 0

        sawSeparator := openedInQuotes
        valueStarted := openedInQuotes

        for index := 0; index < len(line); index = index + 1 {
            character := line[index]

            if '"' == character || '\'' == character {
                if true == inQuotes {

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

                if '#' == character && false == sawSeparator {
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
