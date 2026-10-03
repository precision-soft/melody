package config

import (
    "errors"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
)

/* errUndefinedParameterReference is the cause of the undefined-parameter error, so the constructor's tolerant pass recognizes exactly this failure: the composition root registers its parameters between construction and boot. */
var errUndefinedParameterReference = errors.New("undefined parameter key")

/* Resolve resolves every parameter's template in one order-independent batch and settles the ones the constructor's tolerant pass deferred; a reference that is still undefined here is the error that pass postponed. */
func (instance *Configuration) Resolve() error {
    return instance.resolveAll(false)
}

func (instance *Configuration) resolveAll(deferUnresolvedReferences bool) error {
    /* the resolution mutates the shared parameter map under the write lock and reaches parameters only through the lock-free getInternalParameter, so the lock is not re-entered; each value is written through storeValue, since a consumer reads it through the parameter's own lock */
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* once the application serves, a re-resolution reconfigures nothing and only rewrites the store under settled readers, so it is refused; the flag is read under the same write lock, so a racing MarkServing cannot slip between the check and the rewrite */
    if true == instance.serving.Load() {
        return exception.NewError(
            "cannot resolve the configuration once the application has begun serving; parameters must be registered and resolved during boot",
            nil,
            nil,
        )
    }

    /* walked sorted so the failure names the same parameter on every boot */
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
            /* only the undefined-reference failure is deferrable, and only outside the reserved namespace: a kernel.* parameter is registered by melody before this pass and read by the views the constructor builds right after it, so no later registration repairs it */
            if true == deferUnresolvedReferences &&
                false == instance.isReserved(name) &&
                true == errors.Is(resolveTemplateErr, errUndefinedParameterReference) {
                parameter.deferred.Store(true)

                continue
            }

            failureContext := map[string]any{
                "parameter": name,
            }

            /* the environment key is named beside the parameter, since a MELODY_* key and its kernel.* alias are one parameter and the operator wrote only the key; a runtime parameter has none */
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

/* scanTemplate walks the template left to right, deciding at each percent what it opens: the %% escape, an %env(...)% placeholder, a %parameter% reference, or nothing, a lone percent being data. A referenced value is spliced in as data, never rescanned, so a password holding a percent survives and adjacent placeholders (%a%%b%) resolve. Whatever the scan cannot resolve is an error, so no placeholder survives into a resolved value. */
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
                index,
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

            /* another percent interrupted before a closer: the fragment is not a placeholder and the percent is data */
            builder.WriteByte('%')
            index = index + 1

            continue
        }

        parameterKey, consumedLength, referenceOpened := parseParameterPlaceholder(value[index:])
        if 0 == consumedLength {
            /* a name-shaped run a percent opened and nothing closed is a reference with a typo, not data, since a literal percent must be doubled. The run is not reported, being a slice of the value by construction; the offset of the percent locates it. */
            if true == referenceOpened {
                return "", exception.NewError(
                    "malformed parameter reference in template; a reference closes with a percent (%name%) and a literal percent is written doubled (a password written as pa%%ss%%word resolves to pa%ss%word)",
                    map[string]any{
                        "parameter": currentKey,
                        "offset":    index,
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

/* nameEnvironmentValueRefusal adds the environment key to a refusal raised while its value was being scanned under the reading parameter's name, so the offset is read against the right string. The innermost read adds it once, and a %parameter% reference inside the value, scanned under its own name, is left alone. */
func nameEnvironmentValueRefusal(refusalErr error, environmentKey string, readingParameter string) {
    var refusal *exception.Error
    if false == errors.As(refusalErr, &refusal) || nil == refusal {
        return
    }

    refusalContext := refusal.Context()
    if _, carriesOffset := refusalContext["offset"]; false == carriesOffset {
        return
    }

    if readingParameter != refusalContext["parameter"] {
        return
    }

    if _, alreadyNamed := refusalContext["environmentKey"]; true == alreadyNamed {
        return
    }

    refusal.SetContextValue("environmentKey", environmentKey)
}

/* resolveEnvironmentPlaceholder resolves one %env(...)% construct at the start of the fragment. A candidate interrupted by another percent before any ")%" consumes nothing and the percent is data; a fragment with no closer, and a closed fragment that is not well-formed, are errors, carrying the offset of the opening percent in place of the text. */
func (instance *Configuration) resolveEnvironmentPlaceholder(
    fragment string,
    percentOffset int,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, int, error) {
    /* the candidate ends at the first ")%" no percent interrupts, so a literal "%env(" is not read up to another placeholder's closer; a ")" that closes nothing does not end the search */
    innerEnd := len("%env(")
    for innerEnd < len(fragment) && '%' != fragment[innerEnd] {
        if ')' == fragment[innerEnd] && innerEnd+1 < len(fragment) && '%' == fragment[innerEnd+1] {
            break
        }

        innerEnd = innerEnd + 1
    }

    if innerEnd >= len(fragment) {
        /* nothing of the fragment reaches the error context: with no closer it runs to the end of the value, so it is a slice of the value, possibly a credential; the offset locates it */
        return "", 0, exception.NewError(
            "unterminated environment placeholder in template; %env( opens a placeholder that must close with )%, and a literal percent is written doubled (%%)",
            map[string]any{
                "parameter": currentKey,
                "offset":    percentOffset,
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
        /* only a candidate spelled in key-grammar characters reaches the error context: the span may hold pasted text, a credential among it */
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
        /* an environment value is itself a template, resolved recursively; the guard turns a self-referential value into an error */
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
            nameEnvironmentValueRefusal(envValueErr, environmentKey, currentKey)

            return "", 0, envValueErr
        }

        /* a secret marking on the environment key's own parameter travels here as on the parameter branch, so a dsn assembled from the key is redacted too */
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
        /* the raw value is not embedded: it commonly holds inline credentials, and the error context reaches the logs; the environment key identifies the placeholder */
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

/* resolveParameterReference resolves one referenced parameter and splices its resolved value in as data. The referenced value is stored only during boot, since a post-boot re-resolution changes nothing. A secret marking travels to the reader, so an assembled dsn is redacted beside its credential. */
func (instance *Configuration) resolveParameterReference(
    parameterKey string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    referencedParameter := instance.getInternalParameter(parameterKey)
    if nil == referencedParameter {
        /* the raw value is not embedded: it commonly holds inline credentials, and the error context reaches the logs. The sentinel cause lets the tolerant pass defer exactly this failure. */
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
        /* the raw value is not embedded: a non-string parameter may be a signing key, so only its type identifies it */
        return "", exception.NewError(
            "parameter environment value must be string for template resolution",
            map[string]any{
                "parameterKey":         parameterKey,
                "environmentValueType": fmt.Sprintf("%T", referencedParameter.environmentValue),
            },
            nil,
        )
    }

    /* the project directory is a filesystem path, not a template: a literal percent in it survives a reference */
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

/* templateCarriesConstruct answers whether a scan of the value would do anything at all, so a pre-boot registration is deferred exactly when its raw value is not its resolved value. It dispatches on each percent as scanTemplate does and resolves nothing. */
func templateCarriesConstruct(value string) bool {
    index := 0
    for index < len(value) {
        percentOffset := strings.IndexByte(value[index:], '%')
        if 0 > percentOffset {
            return false
        }

        index = index + percentOffset

        if index+1 < len(value) && '%' == value[index+1] {
            return true
        }

        if true == strings.HasPrefix(value[index:], "%env(") {
            return true
        }

        _, consumedLength, referenceOpened := parseParameterPlaceholder(value[index:])
        if 0 < consumedLength || true == referenceOpened {
            return true
        }

        index = index + 1
    }

    return false
}

/* parseParameterPlaceholder reads a %name% reference at the start of the fragment and reports the name and the consumed length; a name may be one character. Zero consumed with the opened flag means a name-shaped run nothing closed; without it the percent is data. */
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
