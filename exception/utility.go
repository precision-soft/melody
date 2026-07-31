package exception

import (
    "errors"
    "reflect"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* isNilInterfaceValue reports whether value is an interface holding a typed nil — a non-nil interface whose concrete pointer, map, slice, function or channel is nil. Such a value passes every `nil == err` comparison while any method call on it dereferences the nil receiver, so the utilities in this file treat it exactly like the nil it means. The helper is local rather than shared: internal.IsNilInterface lives in a package that imports this one, so importing it back would close an import cycle. */
func isNilInterfaceValue(value any) bool {
    if nil == value {
        return true
    }

    reflectedValue := reflect.ValueOf(value)

    switch reflectedValue.Kind() {
    case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
        return reflectedValue.IsNil()
    default:
        return false
    }
}

/* LogContext assembles the loggable context of an error: its message under "error", the context of the nearest ContextProvider in its chain, the cause chain walked from the error's own wrap link, and every extra map merged on top in order, later entries winning. A nil error — including a typed nil, which would otherwise panic on the very line that logs it — yields only the merged extras. */
func LogContext(err error, extra ...exceptioncontract.Context) exceptioncontract.Context {
    if nil == err || true == isNilInterfaceValue(err) {
        mergedContext := (exceptioncontract.Context)(nil)

        for _, extraContext := range extra {
            if nil == extraContext {
                continue
            }

            if nil == mergedContext {
                mergedContext = make(exceptioncontract.Context, len(extraContext))
            }

            for key, value := range extraContext {
                mergedContext[key] = value
            }
        }

        return mergedContext
    }

    context := exceptioncontract.Context{
        "error": err.Error(),
    }

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        errorContext := provider.Context()
        for key, value := range errorContext {
            if "error" == key {
                continue
            }

            context[key] = value
        }
    }

    /* the chain is anchored on the top error's own wrap link, not on the nearest *Error found by a deep search: anchoring deep skipped every link above the found error and dropped that error's own context from the record entirely whenever it was not the top — the standard HttpException-wrapping-an-Error shape logged neither */
    causeErr := errors.Unwrap(err)
    if nil != causeErr && false == isNilInterfaceValue(causeErr) {
        _, hasCause := context["cause"]
        _, hasCauseChain := context["causeChain"]

        if false == hasCause || false == hasCauseChain {
            causeChain := BuildCauseChain(causeErr, 8)
            if 0 < len(causeChain) {
                if false == hasCause {
                    context["cause"] = causeChain[0]
                }
                if false == hasCauseChain {
                    context["causeChain"] = causeChain
                }
            } else if false == hasCause {
                context["cause"] = causeErr.Error()
            }
        }

        _, hasCauseContextChain := context["causeContextChain"]
        if false == hasCauseContextChain {
            causeContextChain := BuildCauseContextChain(causeErr, 8)
            if 0 < len(causeContextChain) {
                context["causeContextChain"] = causeContextChain
            }
        }
    }

    for _, extraContext := range extra {
        if nil == extraContext {
            continue
        }

        for key, value := range extraContext {
            context[key] = value
        }
    }

    return context
}

func FromError(err error) *Error {
    if nil == err || true == isNilInterfaceValue(err) {
        return nil
    }

    exceptionError, ok := err.(*Error)
    if true == ok {
        return exceptionError
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        context = provider.Context()
    }

    return NewError(err.Error(), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
    if nil == err || true == isNilInterfaceValue(err) {
        return nil
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        context = provider.Context()
    }

    return newWithLevel(err.Error(), context, err, level)
}

func FromErrorWithLevelAndContext(err error, level loggingcontract.Level, context exceptioncontract.Context) *Error {
    if nil == err || true == isNilInterfaceValue(err) {
        return nil
    }

    mergedContext := make(exceptioncontract.Context)

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        for key, value := range provider.Context() {
            mergedContext[key] = value
        }
    }

    for key, value := range context {
        mergedContext[key] = value
    }

    return newWithLevel(err.Error(), mergedContext, err, level)
}

/* MarkLogged marks the nearest markable error in the chain, matching the depth at which logging.LogError reads the mark back: the mark was written on the top value alone while the reader searched the chain, so marking a wrapped error was a silent no-op and the one failure produced two records. A typed nil is returned unchanged instead of dereferenced — the previous nil guard compared an interface produced by a successful assertion, which is never nil, so it could not fire. */
func MarkLogged(err error) error {
    if nil == err || true == isNilInterfaceValue(err) {
        return err
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if true == errors.As(err, &alreadyLoggedValue) && false == isNilInterfaceValue(alreadyLoggedValue) {
        alreadyLoggedValue.MarkAsLogged()
    }

    return err
}

func copyStringMap[T any](input map[string]T) map[string]T {
    if nil == input {
        return make(map[string]T)
    }

    copied := make(map[string]T, len(input))

    for key, value := range input {
        copied[key] = value
    }

    return copied
}

/* causeChainCapacityHint bounds the pre-allocated slice capacity for the cause-chain builders. The walk still honours the caller's maxDepth, but the up-front allocation must not be driven by an unclamped caller value: a maxDepth meaning "unlimited" (for example math.MaxInt) would otherwise panic in makeslice, and merely large values would eagerly allocate gigabytes for a short chain. */
const causeChainCapacityHint = 8

func BuildCauseChain(causeErr error, maxDepth int) []string {
    if nil == causeErr || true == isNilInterfaceValue(causeErr) {
        return nil
    }

    if 0 >= maxDepth {
        return []string{causeErr.Error()}
    }

    capacity := maxDepth
    if capacity > causeChainCapacityHint {
        capacity = causeChainCapacityHint
    }

    chain := make([]string, 0, capacity)

    current := causeErr
    for depth := 0; depth < maxDepth && nil != current; depth++ {
        /* a typed-nil link ends the chain: it is the nil its producer meant, and calling Error() through it would panic inside the walk that exists to describe a failure */
        if true == isNilInterfaceValue(current) {
            break
        }

        chain = append(chain, current.Error())
        current = errors.Unwrap(current)
    }

    return chain
}

func BuildCauseContextChain(causeErr error, maxDepth int) []map[string]any {
    /* a typed nil needs no entry guard here: unlike BuildCauseChain there is no pre-loop Error() call to protect, and the per-link guard below ends the walk before anything dereferences it */
    if nil == causeErr {
        return nil
    }

    if 0 >= maxDepth {
        maxDepth = 1
    }

    capacity := maxDepth
    if capacity > causeChainCapacityHint {
        capacity = causeChainCapacityHint
    }

    chain := make([]map[string]any, 0, capacity)
    hasAnyContext := false

    current := causeErr
    for depth := 0; depth < maxDepth && nil != current; depth++ {
        /* a typed-nil link ends the chain exactly as it ends BuildCauseChain, so the two walks stay aligned link for link */
        if true == isNilInterfaceValue(current) {
            break
        }

        /* @important assert on the immediate node, do not errors.As: a deep search jumps ahead to the nearest provider while the cursor advances one link at a time, so a plain wrapper in front of a provider would emit that provider's context once per intervening level. One entry per link, matching BuildCauseChain. The assertion is on ContextProvider rather than *Error so an HttpException — or any userland error carrying a context — contributes its context instead of a silent nil. */
        causeProvider, isProvider := current.(exceptioncontract.ContextProvider)
        if true == isProvider {
            causeContext := causeProvider.Context()
            if nil != causeContext && 0 < len(causeContext) {
                chain = append(chain, causeContext)
                hasAnyContext = true
            } else {
                chain = append(chain, nil)
            }
        } else {
            chain = append(chain, nil)
        }

        current = errors.Unwrap(current)
    }

    if false == hasAnyContext {
        return nil
    }

    return chain
}
