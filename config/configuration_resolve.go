package config

import (
    "errors"
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
)

/* errUndefinedParameterReference travels as the cause of the undefined-parameter error so the constructor's tolerant pass can recognize exactly this failure by identity: the composition root registers its parameters between construction and boot, so a template referencing one of them is not an error there yet — every other resolution failure still is. */
var errUndefinedParameterReference = errors.New("undefined parameter key")

/* Resolve resolves every parameter's template in one order-independent batch and settles the ones the constructor's tolerant pass deferred; a reference that is still undefined here is the error that pass postponed. */
func (instance *Configuration) Resolve() error {
    return instance.resolveAll(false)
}

func (instance *Configuration) resolveAll(deferUnresolvedReferences bool) error {
    /* the resolution iterates and mutates the shared parameter map, so it holds the write lock the runtime accessors read under; the body reaches parameters only through the lock-free getInternalParameter, never a locking accessor, so the lock is not re-entered. Each parameter's value is written through storeValue, since a consumer holding the pointer reads it through the parameter's own lock and never through this one. */
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* once the application serves, a re-resolution can no longer reconfigure it: every service that needed a parameter copied the value out of it while it was being built, and none of them ever looks again. What it still does is rewrite the whole store underneath readers that are entitled to treat it as settled — so it is refused rather than half-honoured, and the refusal reads the flag under the same write lock the rewrite would hold, so a MarkServing racing this call cannot slip between the check and the rewrite. The documented manual construction resolves before it serves and is untouched. */
    if true == instance.serving.Load() {
        return exception.NewError(
            "cannot resolve the configuration once the application has begun serving; parameters must be registered and resolved during boot",
            nil,
            nil,
        )
    }

    /* the names are walked sorted for the failure's identity, not the result's: the fixpoint is order-independent, but returning on the first broken template of a RANDOM map walk named a different parameter on every boot when several were broken — the dotenv reader sorts for exactly this reason */
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
            /* only the undefined-reference failure is deferrable, recognized by the sentinel cause, and only outside the reserved namespace: a kernel.* parameter is registered by melody itself before this pass, so an undefined reference in one is a settled error no later registration repairs — deferring it would move the failure from the constructor to whichever kernel view reads it next */
            if true == deferUnresolvedReferences &&
                false == instance.isReserved(name) &&
                true == errors.Is(resolveTemplateErr, errUndefinedParameterReference) {
                parameter.deferred.Store(true)

                continue
            }

            failureContext := map[string]any{
                "parameter": name,
            }

            /* the environment key is named beside the parameter because the two are not the same word to the operator: a MELODY_* key and its kernel.* alias are one *Parameter stored under both names, the sorted walk reaches the alias first (M sorts before k), and the failure therefore named "kernel.log_path" to someone who had only ever written MELODY_LOG_PATH — a name they could not find in their own configuration. The key is omitted for a parameter registered at runtime, which has none. */
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

            /* no closer right there — the run ended at a percent or ran out: the fragment is not a placeholder and the percent is data */
            builder.WriteByte('%')
            index = index + 1

            continue
        }

        parameterKey, consumedLength, referenceOpened := parseParameterPlaceholder(value[index:])
        if 0 == consumedLength {
            /* a name-shaped run that a percent opened but nothing closed is a reference with a typo, not data: the contract already demands a literal percent be doubled, so refusing here is what keeps %app-name% from surviving as literal text. A percent in front of a character no name may start with stays data. */
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

/* resolveEnvironmentPlaceholder resolves one %env(...)% construct sitting at the start of the fragment. A fragment with no closing ")%" is reported as consuming nothing, so the caller treats the percent as data; a closed fragment that is not a well-formed placeholder is an error, because a typo that silently survived as literal text is exactly what this reporting exists to catch. */
func (instance *Configuration) resolveEnvironmentPlaceholder(
    fragment string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, int, error) {
    /* the candidate ends at the first ")%" that no percent interrupts: a percent before the closer means a different placeholder's closer is being looked at, and reaching for it would turn a literal "%env(" into a malformed-placeholder boot failure carrying raw template text. A ")" that ")%"-closes nothing does not end the search — "%env(A))%" runs to the real closer and is reported as the malformed placeholder it is — and a "%env(" that never closes at all is an error too, because a placeholder that silently survived as literal text is exactly what this reporting exists to catch. */
    innerEnd := len("%env(")
    for innerEnd < len(fragment) && '%' != fragment[innerEnd] {
        if ')' == fragment[innerEnd] && innerEnd+1 < len(fragment) && '%' == fragment[innerEnd+1] {
            break
        }

        innerEnd = innerEnd + 1
    }

    if innerEnd >= len(fragment) {
        /* only a span spelled in key-grammar characters is carried into the error context: the fragment may hold arbitrary pasted text — a credential typed where the key belongs — and that must not reach the logs */
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
        /* only a candidate spelled in key-grammar characters is carried into the error context: the bounded span may hold arbitrary pasted text — a credential typed where the key belongs — and that must not reach the logs */
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
        /* do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the environment key alone identifies the offending placeholder */
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

/* resolveParameterReference resolves one referenced parameter and splices its fully resolved value in as data. During boot the referenced parameter's own value is stored along the way — post-boot the store is skipped: a consumer reads the value through the parameter's own valueMutex, the re-resolution is deterministic, so rewriting a value the boot already settled would change nothing and only churn the lock under readers. A secret marking on it travels to the reader — a dsn assembled from a declared password is the common case — so redaction covers the assembled value beside the credential itself. */
func (instance *Configuration) resolveParameterReference(
    parameterKey string,
    currentKey string,
    resolvingParameters map[string]bool,
    resolvingEnvironmentKeys map[string]bool,
) (string, error) {
    referencedParameter := instance.getInternalParameter(parameterKey)
    if nil == referencedParameter {
        /* do not embed the raw parameter value here: it commonly holds inline credentials (e.g. a DSN with a password) that would then reach logs unredacted via the exception cause-context chain; the parameter key alone identifies the offending placeholder. The sentinel cause is what lets the constructor's tolerant pass defer exactly this failure. */
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
        /* do not embed the raw value here either: a non-string parameter is not guaranteed non-secret — a signing key registered as bytes is exactly what a template would reference — so only its type identifies it */
        return "", exception.NewError(
            "parameter environment value must be string for template resolution",
            map[string]any{
                "parameterKey":         parameterKey,
                "environmentValueType": fmt.Sprintf("%T", referencedParameter.environmentValue),
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

/* parseParameterPlaceholder reads a %name% reference at the start of the fragment and reports the name and the consumed length — a name may be a single character, since the default processor's fallback accepts one. Zero consumed with the opened flag raised means a name-shaped run began and nothing closed it; zero consumed without it means the percent opened no reference at all and is data. */
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
