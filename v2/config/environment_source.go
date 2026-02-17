package config

import (
	"bufio"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
	configcontract "github.com/precision-soft/melody/v2/config/contract"
	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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

		values[trimmedKey] = strings.TrimSpace(value)
	}

	return nil
}

func preprocessDotEnvContent(content string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(
		make([]byte, 0, 64*1024),
		1024*1024,
	)

	lines := make([]string, 0)

	for scanner.Scan() {
		line := scanner.Text()

		builder := strings.Builder{}

		inQuotes := false
		var quoteChar rune = 0

		for _, character := range line {
			if '"' == character || '\'' == character {
				if false == inQuotes {
					inQuotes = true
					quoteChar = character
				} else if true == (quoteChar == character) {
					inQuotes = false
					quoteChar = 0
				}

				_, _ = builder.WriteRune(character)
				continue
			}

			if '#' == character && false == inQuotes {
				break
			}

			_, _ = builder.WriteRune(character)
		}

		processed := strings.TrimRightFunc(builder.String(), unicode.IsSpace)
		if "" == strings.TrimSpace(processed) {
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
