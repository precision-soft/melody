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

        value, resolveTemplateErr := instance.resolveTemplate(
            stringValue,
            name,
            make(map[string]bool),
            make(map[string]bool),
        )
        if nil != resolveTemplateErr {
            return exception.NewError(
                "failed to resolve parameter",
                map[string]any{
                    "parameter": name,
                },
                resolveTemplateErr,
            )
        }

        parameter.value = value
    }

    return nil
}

/* resolveTemplate resolves one parameter's template. The guard on the parameter name turns a self-reference — direct, or through any chain of parameters and environment keys — into an error instead of an endless recursion. */
func (instance *Configuration) resolveTemplate(
    value string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    if true == resolvingParameters[currentKey] {
        return "", exception.NewError(
            "circular parameter reference detected",
            map[string]any{
                "parameter": currentKey,
            },
            nil,
        )
    }

    resolvingParameters[currentKey] = true
    defer func() {
        delete(resolvingParameters, currentKey)
    }()

    return instance.scanTemplate(value, currentKey, resolvingParameters, resolvingEnvironmentKeys)
}

/* scanTemplate walks the template left to right, deciding at each percent what it opens: the %% escape for one literal percent, an %env(...)% placeholder, a %parameter% reference, or nothing — a lone percent is data. A referenced value is resolved recursively and spliced in as pure data, never rescanned, so a password holding a percent survives as written and adjacent placeholders (%a%%b%) resolve instead of swallowing each other. Whatever the scan cannot resolve is an error here and now — no placeholder survives into a resolved value, which is what lets the doubled-percent escape and genuine literals coexist without an after-the-fact validation guessing which was which. */
func (instance *Configuration) scanTemplate(
    value string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    var builder strings.Builder

    index := 0
    for index < len(value) {
        percentOffset := strings.IndexByte(value[index:], '%')
        if 0 > percentOffset {
            builder.WriteString(value[index:])

            break
        }

        builder.WriteString(value[index : index+percentOffset])
        index = index + percentOffset

        if index+1 < len(value) && '%' == value[index+1] {
            builder.WriteByte('%')
            index = index + 2

            continue
        }

        if true == strings.HasPrefix(value[index:], "%env(") {
            resolvedEnvironment, consumedLength, environmentErr := instance.resolveEnvironmentPlaceholder(
                value[index:],
                currentKey,
                resolvingParameters,
                resolvingEnvironmentKeys,
            )
            if nil != environmentErr {
                return "", environmentErr
            }

            if 0 < consumedLength {
                builder.WriteString(resolvedEnvironment)
                index = index + consumedLength

                continue
            }

            /* no closing ")%" ahead: the fragment is not a placeholder and the percent is data */
            builder.WriteByte('%')
            index = index + 1

            continue
        }

        parameterKey, consumedLength := parseParameterPlaceholder(value[index:])
        if 0 == consumedLength {
            builder.WriteByte('%')
            index = index + 1

            continue
        }

        resolvedParameter, parameterErr := instance.resolveParameterReference(
            parameterKey,
            currentKey,
            resolvingParameters,
            resolvingEnvironmentKeys,
        )
        if nil != parameterErr {
            return "", parameterErr
        }

        builder.WriteString(resolvedParameter)
        index = index + consumedLength
    }

    return builder.String(), nil
}

/* resolveEnvironmentPlaceholder resolves one %env(...)% construct sitting at the start of the fragment. A fragment with no closing ")%" is reported as consuming nothing, so the caller treats the percent as data; a closed fragment that is not a well-formed placeholder is an error, because a typo that silently survived as literal text is exactly what this reporting exists to catch. */
func (instance *Configuration) resolveEnvironmentPlaceholder(
    fragment string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, int, error) {
    /* the candidate ends where the placeholder grammar ends: scanning ahead to an arbitrary ")%" would let a distant closer — the end of a different, well-formed placeholder — turn a literal "%env(" into a malformed-placeholder boot failure, and would carry raw template text into the error context */
    innerEnd := len("%env(")
    for innerEnd < len(fragment) && true == isEnvPlaceholderInnerCharacter(fragment[innerEnd]) {
        innerEnd = innerEnd + 1
    }

    if innerEnd+1 >= len(fragment) || ')' != fragment[innerEnd] || '%' != fragment[innerEnd+1] {
        return "", 0, nil
    }

    candidate := fragment[:innerEnd+2]

    submatches := envPlaceholderPattern.FindStringSubmatch(candidate)
    if nil == submatches || candidate != submatches[0] {
        /* @important the placeholder text names an environment key, never its value, so it is safe to carry into the error context */
        return "", 0, exception.NewError(
            "malformed environment placeholder in template; the only supported form besides %env(KEY)% is %env(default:<fallback parameter>:KEY)%, and a type cast belongs on the typed accessor (Bool, Int, Float, Duration) rather than in the placeholder",
            map[string]any{
                "parameter":   currentKey,
                "placeholder": candidate,
            },
            nil,
        )
    }

    hasDefaultProcessor := "" != submatches[1]
    fallbackParameterKey := submatches[2]
    environmentKey := submatches[3]

    envValue, exists := instance.environment.Get(environmentKey)
    if true == exists {
        /* an environment value is itself a template, so it is resolved recursively; the guard is what turns a self-referential value — APP_A=x%env(APP_A)% — into an error instead of an endless recursion */
        if true == resolvingEnvironmentKeys[environmentKey] {
            return "", 0, exception.NewError(
                "circular reference detected while resolving placeholders",
                map[string]any{
                    "parameter":      currentKey,
                    "environmentKey": environmentKey,
                },
                nil,
            )
        }

        resolvingEnvironmentKeys[environmentKey] = true

        resolvedEnvValue, envValueErr := instance.scanTemplate(
            envValue,
            currentKey,
            resolvingParameters,
            resolvingEnvironmentKeys,
        )

        delete(resolvingEnvironmentKeys, environmentKey)

        if nil != envValueErr {
            return "", 0, envValueErr
        }

        /* the environment key was registered as a parameter under its own name, and marking that parameter secret is how a credential read through %env(KEY)% is declared; the marking has to travel here exactly as it does on the parameter branch, or a dsn assembled from the key would print in full beside the redacted key */
        environmentParameter := instance.getInternalParameter(environmentKey)
        if nil != environmentParameter && true == environmentParameter.isSecret.Load() {
            currentParameter := instance.getInternalParameter(currentKey)
            if nil != currentParameter {
                currentParameter.isSecret.Store(true)
            }
        }

        return resolvedEnvValue, len(candidate), nil
    }

    if false == hasDefaultProcessor {
        /* @important do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the environment key alone identifies the offending placeholder */
        return "", 0, exception.NewError(
            "undefined environment key in template",
            map[string]any{
                "environmentKey": environmentKey,
            },
            nil,
        )
    }

    if "" == fallbackParameterKey {
        return "", len(candidate), nil
    }

    resolvedFallback, fallbackErr := instance.resolveParameterReference(
        fallbackParameterKey,
        currentKey,
        resolvingParameters,
        resolvingEnvironmentKeys,
    )
    if nil != fallbackErr {
        return "", 0, fallbackErr
    }

    return resolvedFallback, len(candidate), nil
}

/* resolveParameterReference resolves one referenced parameter and splices its fully resolved value in as data. The referenced parameter's own value is stored along the way, and a secret marking on it travels to the reader — a dsn assembled from a declared password is the common case — so redaction covers the assembled value beside the credential itself. */
func (instance *Configuration) resolveParameterReference(
    parameterKey string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    referencedParameter := instance.getInternalParameter(parameterKey)
    if nil == referencedParameter {
        /* @important do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the parameter key alone identifies the offending placeholder */
        return "", exception.NewError(
            "undefined parameter key in template; a value that contains a literal percent rather than a reference must double it (a password written as pa%%ss%%word resolves to pa%ss%word)",
            map[string]any{
                "parameterKey": parameterKey,
                "parameter":    currentKey,
            },
            nil,
        )
    }

    environmentValueString, ok := referencedParameter.environmentValue.(string)
    if false == ok {
        return "", exception.NewError(
            "parameter environment value must be string for template resolution",
            map[string]any{
                "parameterKey":     parameterKey,
                "environmentValue": referencedParameter.environmentValue,
            },
            nil,
        )
    }

    /* the project directory is a filesystem path, not a template: a literal percent in it must survive a reference, exactly as Resolve leaves the parameter itself unscanned */
    if KernelProjectDir == parameterKey {
        return environmentValueString, nil
    }

    resolvedReferencedValue, resolveErr := instance.resolveTemplate(
        environmentValueString,
        parameterKey,
        resolvingParameters,
        resolvingEnvironmentKeys,
    )
    if nil != resolveErr {
        return "", resolveErr
    }

    referencedParameter.value = resolvedReferencedValue

    if true == referencedParameter.isSecret.Load() {
        currentParameter := instance.getInternalParameter(currentKey)
        if nil != currentParameter {
            currentParameter.isSecret.Store(true)
        }
    }

    return resolvedReferencedValue, nil
}

/* parseParameterPlaceholder reads a %name% reference at the start of the fragment and reports the name and the consumed length, or zero when the fragment opens no reference — a name may be a single character, since the default processor's fallback accepts one. */
func parseParameterPlaceholder(fragment string) (string, int) {
    if 2 > len(fragment) {
        return "", 0
    }

    if false == isParameterNameStartCharacter(fragment[1]) {
        return "", 0
    }

    end := 2
    for end < len(fragment) && true == isParameterNameCharacter(fragment[end]) {
        end = end + 1
    }

    if end >= len(fragment) || '%' != fragment[end] {
        return "", 0
    }

    return fragment[1:end], end + 1
}

func isParameterNameStartCharacter(character byte) bool {
    return ('A' <= character && 'Z' >= character) || ('a' <= character && 'z' >= character) || '_' == character
}

func isParameterNameCharacter(character byte) bool {
    return true == isParameterNameStartCharacter(character) || ('0' <= character && '9' >= character) || '.' == character
}

func isEnvPlaceholderInnerCharacter(character byte) bool {
    return true == isParameterNameCharacter(character) || ':' == character
}
