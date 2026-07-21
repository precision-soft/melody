package config

import (
    "strings"

    "github.com/precision-soft/melody/v3/exception"
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

    for _, candidate := range envPlaceholderShapePattern.FindAllString(resolved, -1) {
        if true == envPlaceholderPattern.MatchString(candidate) {
            continue
        }

        /* @important the placeholder text names an environment key, never its value, so it is safe to carry into the error context */
        return "", exception.NewError(
            "malformed environment placeholder in template; the only supported form besides %env(KEY)% is %env(default:<fallback parameter>:KEY)%, and a type cast belongs on the typed accessor (Bool, Int, Float, Duration) rather than in the placeholder",
            map[string]any{
                "parameter":   currentKey,
                "placeholder": candidate,
            },
            nil,
        )
    }

    resolved = envPlaceholderPattern.ReplaceAllStringFunc(resolved, func(match string) string {
        if nil != err {
            return match
        }

        submatches := envPlaceholderPattern.FindStringSubmatch(match)
        if 4 > len(submatches) {
            return match
        }

        hasDefaultProcessor := "" != submatches[1]
        fallbackParameterKey := submatches[2]
        environmentKey := submatches[3]

        envValue, exists := instance.environment.Get(environmentKey)
        if true == exists {
            return envValue
        }

        if false == hasDefaultProcessor {
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

        if "" == fallbackParameterKey {
            return ""
        }

        /* hand the fallback back as a parameter placeholder instead of resolving it here: the parameter branch below already carries the recursion, the circular-reference guard and the undefined-key reporting, and the enclosing fixed-point loop runs another pass over this output */
        return "%" + fallbackParameterKey + "%"
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
                "undefined parameter key in template; a value that contains a literal percent rather than a reference must double it (a password written as pa%%ss%%word resolves to pa%ss%word)",
                map[string]any{
                    "parameterKey": parameterKey,
                    "parameter":    currentKey,
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

        /* a template that reads a secret carries it into its own value — a dsn assembled from a declared password is the common case — so the secret marking has to travel with it, otherwise redaction would cover the password parameter while the dsn beside it prints in full */
        if true == referencedParameter.isSecret {
            currentParameter := instance.getInternalParameter(currentKey)
            if nil != currentParameter {
                currentParameter.isSecret = true
            }
        }

        return resolvedReferencedValue
    })

    if nil != err {
        return "", err
    }

    return resolved, nil
}
