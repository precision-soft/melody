package config

import (
    "strings"

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

        if true == strings.Contains(stringValue, "%%") {
            if nil == instance.parametersWithEscapedPercents {
                instance.parametersWithEscapedPercents = map[string]bool{}
            }

            instance.parametersWithEscapedPercents[name] = true
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

    /* the env-substitution branch performs a flat text replacement that the parameter-recursion cycle guard (the resolving map) does not cover, so a self-referential env value (direct APP_A=x%env(APP_A)% or indirect A=%env(B)%, B=y%env(A)%) would expand forever without ever reaching a fixed point; bound the passes by the number of resolvable keys plus a margin and report a circular reference the way the parameter path does */
    maximumResolutionPasses := len(instance.parameters) + len(instance.environment.values) + 8

    previous := ""
    resolved := escapedValue

    passes := 0
    for previous != resolved {
        if passes >= maximumResolutionPasses {
            return "", exception.NewError(
                "circular reference detected while resolving placeholders",
                map[string]any{
                    "parameter": currentKey,
                },
                nil,
            )
        }
        passes++

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
            /* @important do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the environment key alone identifies the offending placeholder */
            err = exception.NewError(
                "undefined environment key in template",
                map[string]any{
                    "environmentKey": environmentKey,
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
            /* @important do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the parameter key alone identifies the offending placeholder */
            err = exception.NewError(
                "undefined parameter key in template",
                map[string]any{
                    "parameterKey": parameterKey,
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

        var resolvedReferencedValue string
        resolvedReferencedValue, err = instance.resolveWithTemplates(
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
