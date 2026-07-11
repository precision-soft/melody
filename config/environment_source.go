package config

import (
    "bufio"
    "errors"
    "io/fs"
    "path/filepath"
    "strings"
    "unicode"

    "github.com/joho/godotenv"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

    environmentValue, exists := values[EnvKey]
    if false == exists {
        return EnvDevelopment, nil
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

        builder := strings.Builder{}

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

                _, _ = builder.WriteRune(character)
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

            _, _ = builder.WriteRune(character)
            previousChar = character
        }

        processed := builder.String()

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
