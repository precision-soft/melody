package exception

import (
    "errors"
    "fmt"
    "reflect"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* isNilInterfaceValue reports whether value is an interface holding a typed nil. It duplicates internal.IsNilInterface because that package imports this one. */
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

/* LogContext assembles the loggable context of an error: its message under "error", the context of the nearest ContextProvider in its chain, the cause chain walked from the error's own wrap link, and every extra map merged on top in order, later entries winning. */
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
        "error": renderErrorText(err),
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

    /* the chain is anchored on the top error's own wrap link, not on the nearest *Error a deep search would find: anchoring deep drops the context of every link above it */
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
                context["cause"] = renderErrorText(causeErr)
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

/* renderErrorText produces the loggable text of an error under a recover: LogContext runs inside recovery defers, where a value whose Error() panics — often on the very nil field that made it panic-worthy in the first place — would raise a second panic past the recovery that is reporting the first, and the connection was reset with the original failure reaching no record at all. The container teardown's close-error rendering makes the same trade for the same reason. */
func renderErrorText(err error) (text string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        text = fmt.Sprintf("error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}

/* MarkLogged marks the nearest AlreadyLogged implementer in the chain — the depth IsAlreadyLogged reads the mark back from — and returns the error unchanged. */
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

/* Logged answers an error that reports itself already logged, and is what a writer returns after filing its record. An error whose chain carries an AlreadyLogged implementer is marked in place and handed back unchanged, so its identity — and every errors.Is and errors.As its readers perform on it — survives. An error whose chain carries none has nowhere for the mark to live: errors.New, fmt.Errorf and every runtime error make MarkLogged a silent no-op, and the next reader then files the same failure a second time. That error is wrapped in a marked melody error keeping it as its cause, so the mark the writer meant to leave is the mark the reader finds. The wrap cannot change how a status is resolved: it happens exactly when no HttpException is in the chain, which is exactly when the status was already going to be the generic one. */
func Logged(err error) error {
    if nil == err || true == isNilInterfaceValue(err) {
        return err
    }

    _ = MarkLogged(err)

    if true == IsAlreadyLogged(err) {
        return err
    }

    return MarkLogged(FromError(err))
}

/* IsAlreadyLogged reads the mark at the depth MarkLogged writes it. It is the single reader: reading the mark off a concrete *Error instead misses a marked HttpException and anything wrapping a marked error, which are then rendered a second time. */
func IsAlreadyLogged(err error) bool {
    if nil == err || true == isNilInterfaceValue(err) {
        return false
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if false == errors.As(err, &alreadyLoggedValue) || true == isNilInterfaceValue(alreadyLoggedValue) {
        return false
    }

    return alreadyLoggedValue.AlreadyLogged()
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

/* causeChainCapacityHint bounds the pre-allocated capacity of the cause-chain builders; the walk still honours the caller's maxDepth. An unclamped maxDepth of math.MaxInt panics in makeslice. */
const causeChainCapacityHint = 8

func BuildCauseChain(causeErr error, maxDepth int) []string {
    if nil == causeErr || true == isNilInterfaceValue(causeErr) {
        return nil
    }

    if 0 >= maxDepth {
        return []string{renderErrorText(causeErr)}
    }

    capacity := maxDepth
    if capacity > causeChainCapacityHint {
        capacity = causeChainCapacityHint
    }

    chain := make([]string, 0, capacity)

    current := causeErr
    for depth := 0; depth < maxDepth && nil != current; depth++ {
        /* a typed-nil link is the nil its producer meant and ends the chain */
        if true == isNilInterfaceValue(current) {
            break
        }

        chain = append(chain, renderErrorText(current))
        current = errors.Unwrap(current)
    }

    return chain
}

func BuildCauseContextChain(causeErr error, maxDepth int) []map[string]any {
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
        /* a typed-nil link is the nil its producer meant and ends the chain */
        if true == isNilInterfaceValue(current) {
            break
        }

        /* the immediate node is asserted rather than searched with errors.As: a deep search emits the nearest provider's context once per intervening wrapper, while the cursor advances one link at a time */
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
