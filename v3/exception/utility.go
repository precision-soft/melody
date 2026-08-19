package exception

import (
    "errors"
    "reflect"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

func LogContext(err error, extra ...exceptioncontract.Context) exceptioncontract.Context {
    if nil == err {
        if 0 == len(extra) || nil == extra[0] {
            return nil
        }

        mergedContext := make(exceptioncontract.Context, len(extra[0]))
        for key, value := range extra[0] {
            mergedContext[key] = value
        }

        return mergedContext
    }

    context := exceptioncontract.Context{
        "error": err.Error(),
    }

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) {
        errorContext := provider.Context()
        for key, value := range errorContext {
            if "error" == key {
                continue
            }

            context[key] = value
        }
    }

    var exceptionValue *Error
    if true == errors.As(err, &exceptionValue) && nil != exceptionValue {
        causeErr := exceptionValue.CauseErr()
        if nil != causeErr {
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
    }

    if 0 == len(extra) || nil == extra[0] {
        return context
    }

    for key, value := range extra[0] {
        context[key] = value
    }

    return context
}

func FromError(err error) *Error {
    if nil == err {
        return nil
    }

    exceptionError, ok := err.(*Error)
    if true == ok {
        return exceptionError
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) {
        context = provider.Context()
    }

    return NewError(err.Error(), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
    if nil == err {
        return nil
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) {
        context = provider.Context()
    }

    return newWithLevel(err.Error(), context, err, level)
}

func FromErrorWithLevelAndContext(err error, level loggingcontract.Level, context exceptioncontract.Context) *Error {
    if nil == err {
        return nil
    }

    mergedContext := make(exceptioncontract.Context)

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) {
        for key, value := range provider.Context() {
            mergedContext[key] = value
        }
    }

    for key, value := range context {
        mergedContext[key] = value
    }

    return newWithLevel(err.Error(), mergedContext, err, level)
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

/* causeChainCapacityHint bounds the pre-allocated slice capacity for the cause-chain builders. The walk still honours the caller's maxDepth, but the up-front allocation must not be driven by an unclamped caller value: a maxDepth meaning "unlimited" (for example math.MaxInt) would otherwise panic in makeslice, and merely large values would eagerly allocate gigabytes for a short chain. */
const causeChainCapacityHint = 8

func BuildCauseChain(causeErr error, maxDepth int) []string {
    if nil == causeErr {
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
        chain = append(chain, current.Error())
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
        /* assert on the immediate node, do not errors.As: a deep search jumps ahead to the nearest *Error while the cursor advances one link at a time, so a plain wrapper in front of an *Error would emit that *Error's context once per intervening level. One entry per link, matching BuildCauseChain. */
        causeException, isException := current.(*Error)
        if true == isException && nil != causeException {
            causeContext := causeException.Context()
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
