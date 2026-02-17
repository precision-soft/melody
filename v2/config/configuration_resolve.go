package config

import (
	"github.com/precision-soft/melody/v2/exception"
)

func (instance *Configuration) Resolve() error {
	for name, parameter := range instance.parameters {
		if KernelProjectDir == name {
			continue
		}

		environmentValue := parameter.environmentValue

		stringValue, ok := environmentValue.(string)
		if false == ok {
			parameter.value = environmentValue

			continue
		}

		if "" == stringValue {
			if true == parameter.IsDefault() {
				stringValue = parameter.environmentValue.(string)
			} else {
				parameter.value = stringValue

				continue
			}
		}

		value, resolveWithTemplatesErr := instance.resolveWithTemplates(
			stringValue,
			name,
			make(map[string]bool),
		)
		if nil != resolveWithTemplatesErr {
			return exception.NewError(
				"failed to resolve parameter",
				map[string]any{
					"parameter": name,
				},
				resolveWithTemplatesErr,
			)
		}

		parameter.value = value
	}

	return nil
}

func (instance *Configuration) resolveWithTemplates(
	value string,
	currentKey string,
	resolving map[string]bool,
) (string, error) {
	if true == resolving[currentKey] {
		return "", exception.NewError(
			"circular parameter reference detected",
			map[string]any{
				"parameter": currentKey,
			},
			nil,
		)
	}

	escapedValue := instance.escapePercents(value)

	resolving[currentKey] = true
	defer func() {
		delete(resolving, currentKey)
	}()

	previous := ""
	resolved := escapedValue

	for previous != resolved {
		previous = resolved

		singlePassResolved, resolveSinglePassErr := instance.resolveSinglePass(
			resolved,
			currentKey,
			resolving,
		)
		if nil != resolveSinglePassErr {
			return "", resolveSinglePassErr
		}

		resolved = singlePassResolved
	}

	finalValue := instance.unescapePercents(resolved)

	return finalValue, nil
}

func (instance *Configuration) resolveSinglePass(
	value string,
	currentKey string,
	resolving map[string]bool,
) (string, error) {
	resolved := value
	var err error

	resolved = envPlaceholderPattern.ReplaceAllStringFunc(resolved, func(match string) string {
		if nil != err {
			return match
		}

		submatches := envPlaceholderPattern.FindStringSubmatch(match)
		if 2 > len(submatches) {
			return match
		}

		environmentKey := submatches[1]

		envValue, exists := instance.environment.Get(environmentKey)
		if false == exists {
			err = exception.NewError(
				"undefined environment key in template",
				map[string]any{
					"environmentKey": environmentKey,
					"value":          value,
				},
				nil,
			)

			return match
		}

		return envValue
	})

	if nil != err {
		return "", err
	}

	resolved = parameterPlaceholderPattern.ReplaceAllStringFunc(resolved, func(match string) string {
		if nil != err {
			return match
		}

		submatches := parameterPlaceholderPattern.FindStringSubmatch(match)
		if 2 > len(submatches) {
			return match
		}

		parameterKey := submatches[1]

		if parameterKey == currentKey {
			return match
		}

		referencedParameter := instance.getInternalParameter(parameterKey)
		if nil == referencedParameter {
			err = exception.NewError(
				"undefined parameter key in template",
				map[string]any{
					"parameterKey": parameterKey,
					"value":        value,
				},
				nil,
			)

			return match
		}

		environmentValueString, ok := referencedParameter.environmentValue.(string)
		if false == ok {
			err = exception.NewError(
				"parameter environment value must be string for template resolution",
				map[string]any{
					"parameterKey":     parameterKey,
					"environmentValue": referencedParameter.environmentValue,
				},
				nil,
			)

			return match
		}

		resolvedReferencedValue, err := instance.resolveWithTemplates(
			environmentValueString,
			parameterKey,
			resolving,
		)
		if nil != err {
			return match
		}

		referencedParameter.value = resolvedReferencedValue

		return resolvedReferencedValue
	})

	if nil != err {
		return "", err
	}

	return resolved, nil
}
