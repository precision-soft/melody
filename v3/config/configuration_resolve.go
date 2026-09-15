package config

import (
    "errors"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

var errUndefinedParameterReference = errors.New("undefined parameter key")

/* Resolve resolves every parameter's template in one order-independent batch and settles the ones the constructor's tolerant pass deferred; a reference that is still undefined here is the error that pass postponed. */
func (instance *Configuration) Resolve() error {
    return instance.resolveAll(false)
}

func (instance *Configuration) resolveAll(deferUnresolvedReferences bool) error {

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.serving.Load() {
        return exception.NewError(
            "cannot resolve the configuration once the application has begun serving; parameters must be registered and resolved during boot",
            nil,
            nil,
        )
    }

    names := make([]string, 0, len(instance.parameters))
    for name := range instance.parameters {
        names = append(names, name)
    }
    sort.Strings(names)

    for _, name := range names {
        parameter := instance.parameters[name]

        if KernelProjectDir == name {
            continue
        }

        environmentValue := parameter.environmentValue

        stringValue, ok := environmentValue.(string)
        if false == ok {
            parameter.storeValue(environmentValue)

            continue
        }

        if "" == stringValue {
            parameter.storeValue(stringValue)

            continue
        }

        value, resolveTemplateErr := instance.resolveTemplate(
            stringValue,
            name,
            make(map[string]bool),
            make(map[string]bool),
        )
        if nil != resolveTemplateErr {

            if true == deferUnresolvedReferences &&
                false == instance.isReserved(name) &&
                true == errors.Is(resolveTemplateErr, errUndefinedParameterReference) {
                parameter.deferred.Store(true)

                continue
            }

            failureContext := map[string]any{
                "parameter": name,
            }

            if "" != parameter.environmentKey && parameter.environmentKey != name {
                failureContext["environmentKey"] = parameter.environmentKey
            }

            return exception.NewError(
                "failed to resolve parameter",
                failureContext,
                resolveTemplateErr,
            )
        }

        parameter.storeValue(value)
        parameter.deferred.Store(false)
    }

    instance.resolved = true

    return nil
}

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

            builder.WriteByte('%')
            index = index + 1

            continue
        }

        parameterKey, consumedLength, referenceOpened := parseParameterPlaceholder(value[index:])
        if 0 == consumedLength {

            if true == referenceOpened {
                return "", exception.NewError(
                    "malformed parameter reference in template; a reference closes with a percent (%name%) and a literal percent is written doubled (a password written as pa%%ss%%word resolves to pa%ss%word)",
                    map[string]any{
                        "parameter": currentKey,
                        "reference": "%" + parameterKey,
                    },
                    nil,
                )
            }

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

func (instance *Configuration) resolveEnvironmentPlaceholder(
    fragment string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, int, error) {

    innerEnd := len("%env(")
    for innerEnd < len(fragment) && '%' != fragment[innerEnd] {
        if ')' == fragment[innerEnd] && innerEnd+1 < len(fragment) && '%' == fragment[innerEnd+1] {
            break
        }

        innerEnd = innerEnd + 1
    }

    if innerEnd >= len(fragment) {

        reportedPlaceholder := "%env(<redacted>"
        if true == isKeyGrammarText(fragment[len("%env("):]) {
            reportedPlaceholder = fragment
        }

        return "", 0, exception.NewError(
            "unterminated environment placeholder in template; %env( opens a placeholder that must close with )%, and a literal percent is written doubled (%%)",
            map[string]any{
                "parameter":   currentKey,
                "placeholder": reportedPlaceholder,
            },
            nil,
        )
    }

    if '%' == fragment[innerEnd] {
        return "", 0, nil
    }

    candidate := fragment[:innerEnd+2]

    submatches := envPlaceholderPattern.FindStringSubmatch(candidate)
    if nil == submatches || candidate != submatches[0] {

        reportedPlaceholder := "%env(<redacted>)%"
        if true == isKeyGrammarText(candidate[len("%env("):len(candidate)-len(")%")]) {
            reportedPlaceholder = candidate
        }

        return "", 0, exception.NewError(
            "malformed environment placeholder in template; the only supported form besides %env(KEY)% is %env(default:<fallback parameter>:KEY)%, and a type cast belongs on the typed accessor (Bool, Int, Float, Duration) rather than in the placeholder",
            map[string]any{
                "parameter":   currentKey,
                "placeholder": reportedPlaceholder,
            },
            nil,
        )
    }

    hasDefaultProcessor := "" != submatches[1]
    fallbackParameterKey := submatches[2]
    environmentKey := submatches[3]

    envValue, exists := instance.environment.Get(environmentKey)
    if true == exists {

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

func (instance *Configuration) resolveParameterReference(
    parameterKey string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    referencedParameter := instance.getInternalParameter(parameterKey)
    if nil == referencedParameter {

        return "", exception.NewError(
            "undefined parameter key in template; a value that contains a literal percent rather than a reference must double it (a password written as pa%%ss%%word resolves to pa%ss%word)",
            map[string]any{
                "parameterKey": parameterKey,
                "parameter":    currentKey,
            },
            errUndefinedParameterReference,
        )
    }

    environmentValueString, ok := referencedParameter.environmentValue.(string)
    if false == ok {

        return "", exception.NewError(
            "parameter environment value must be string for template resolution",
            map[string]any{
                "parameterKey":         parameterKey,
                "environmentValueType": fmt.Sprintf("%T", referencedParameter.environmentValue),
            },
            nil,
        )
    }

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

    if false == instance.resolved {
        referencedParameter.storeValue(resolvedReferencedValue)
        referencedParameter.deferred.Store(false)
    }

    if true == referencedParameter.isSecret.Load() {
        currentParameter := instance.getInternalParameter(currentKey)
        if nil != currentParameter {
            currentParameter.isSecret.Store(true)
        }
    }

    return resolvedReferencedValue, nil
}

func parseParameterPlaceholder(fragment string) (string, int, bool) {
    if 2 > len(fragment) {
        return "", 0, false
    }

    if false == isParameterNameStartCharacter(fragment[1]) {
        return "", 0, false
    }

    end := 2
    for end < len(fragment) && true == isParameterNameCharacter(fragment[end]) {
        end = end + 1
    }

    if end >= len(fragment) || '%' != fragment[end] {
        return fragment[1:end], 0, true
    }

    return fragment[1:end], end + 1, false
}

func isParameterNameStartCharacter(character byte) bool {
    return ('A' <= character && 'Z' >= character) || ('a' <= character && 'z' >= character) || '_' == character
}

func isParameterNameCharacter(character byte) bool {
    return true == isParameterNameStartCharacter(character) || ('0' <= character && '9' >= character) || '.' == character
}

func isKeyGrammarText(text string) bool {
    for index := 0; index < len(text); index = index + 1 {
        if false == isParameterNameCharacter(text[index]) && ':' != text[index] {
            return false
        }
    }

    return true
}
