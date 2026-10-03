package exception

import (
    "errors"
    "fmt"
    "reflect"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* isNilInterfaceValue duplicates internal.IsNilInterface, since that package imports this one. */
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
        errorContext := renderedContextOf(provider)
        for key, value := range errorContext {
            if "error" == key {
                continue
            }

            context[key] = value
        }
    }

    /* anchored on the top error's own wrap links, all of them for a joined error, so no link's context above the nearest *Error is dropped */
    causeErrs := causesOf(err)
    if 0 < len(causeErrs) {
        _, hasCause := context["cause"]
        _, hasCauseChain := context["causeChain"]

        if false == hasCause || false == hasCauseChain {
            causeChain := buildCauseChainFromRoots(causeErrs, 8)
            if 0 < len(causeChain) {
                if false == hasCause {
                    context["cause"] = causeChain[0]
                }
                if false == hasCauseChain {
                    context["causeChain"] = causeChain
                }
            } else if false == hasCause {
                context["cause"] = renderErrorText(causeErrs[0])
            }
        }

        _, hasCauseContextChain := context["causeContextChain"]
        if false == hasCauseContextChain {
            causeContextChain := buildCauseContextChainFromRoots(causeErrs, 8)
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
        context = renderedContextOf(provider)
    }

    /* rendered under a recover: these doors run inside the kernel's recovery defers, where a second panic would unwind past the 500 */
    return NewError(renderErrorText(err), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
    if nil == err || true == isNilInterfaceValue(err) {
        return nil
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        context = renderedContextOf(provider)
    }

    return newWithLevel(renderErrorText(err), context, err, level)
}

func FromErrorWithLevelAndContext(err error, level loggingcontract.Level, context exceptioncontract.Context) *Error {
    if nil == err || true == isNilInterfaceValue(err) {
        return nil
    }

    mergedContext := make(exceptioncontract.Context)

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == isNilInterfaceValue(provider) {
        for key, value := range renderedContextOf(provider) {
            mergedContext[key] = value
        }
    }

    for key, value := range context {
        mergedContext[key] = value
    }

    return newWithLevel(renderErrorText(err), mergedContext, err, level)
}

/* renderedContextOf reads a provider's context under a recover, since LogContext runs inside recovery defers; a panicking Context costs the context alone, with the panic value in its place. */
func renderedContextOf(provider exceptioncontract.ContextProvider) (context exceptioncontract.Context) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        context = exceptioncontract.Context{
            "contextPanicked": fmt.Sprintf("%v", recoveredValue),
        }
    }()

    return provider.Context()
}

/* renderErrorText renders an error's text under a recover, since LogContext runs inside recovery defers where a panicking Error would unwind past the recovery reporting the first failure. */
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

/* MarkLogged marks the nearest AlreadyLogged implementer in the chain, the depth IsAlreadyLogged reads, and returns the error unchanged. Every reader sharing that implementer sees the mark. */
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

/* Logged answers an error that reports itself already logged. A chain carrying an AlreadyLogged implementer is marked in place and returned unchanged; one carrying none is wrapped in a marked melody error that keeps it as its cause. The wrap happens only when no HttpException is in the chain, so the resolved status cannot change. */
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

/* IsAlreadyLogged reads the mark at the depth MarkLogged writes it, and is its single reader. */
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

/* PanicCause answers a recovered panic value as the cause of the error a recovery boundary fabricates, so its context and cause chain reach the record. A typed nil and a value that is not an error answer no cause. */
func PanicCause(recoveredValue any) error {
    recoveredErr, isRecoveredError := recoveredValue.(error)
    if false == isRecoveredError || true == isNilInterfaceValue(recoveredErr) {
        return nil
    }

    return recoveredErr
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

/* causeChainCapacityHint bounds the pre-allocated capacity of the cause-chain builders; maxDepth still bounds the walk. */
const causeChainCapacityHint = 8

/* causesOf answers the links below an error in both standard shapes, the Unwrap() error of a wrap and the Unwrap() []error of a join, dropping nil and typed-nil links. The single form is tried first, so a link carrying both is read by its own Unwrap, as errors.Is and errors.As read it. */
func causesOf(err error) []error {
    if singleUnwrapper, isSingleUnwrapper := err.(interface{ Unwrap() error }); true == isSingleUnwrapper {
        causeErr := singleUnwrapper.Unwrap()
        if nil == causeErr || true == isNilInterfaceValue(causeErr) {
            return nil
        }

        return []error{causeErr}
    }

    multiUnwrapper, isMultiUnwrapper := err.(interface{ Unwrap() []error })
    if false == isMultiUnwrapper {
        return nil
    }

    causeErrs := make([]error, 0, len(multiUnwrapper.Unwrap()))
    for _, causeErr := range multiUnwrapper.Unwrap() {
        if nil == causeErr || true == isNilInterfaceValue(causeErr) {
            continue
        }

        causeErrs = append(causeErrs, causeErr)
    }

    if 0 == len(causeErrs) {
        return nil
    }

    return causeErrs
}

func BuildCauseChain(causeErr error, maxDepth int) []string {
    if nil == causeErr || true == isNilInterfaceValue(causeErr) {
        return nil
    }

    if 0 >= maxDepth {
        return []string{renderErrorText(causeErr)}
    }

    return buildCauseChainFromRoots([]error{causeErr}, maxDepth)
}

func buildCauseChainFromRoots(roots []error, maxDepth int) []string {
    capacity := maxDepth
    if capacity > causeChainCapacityHint {
        capacity = causeChainCapacityHint
    }

    chain := make([]string, 0, capacity)

    /* the walk is breadth-first over both unwrap shapes, so a chain of single wraps reads link by link and a join contributes its branches side by side; maxDepth bounds the number of links rendered, not the depth of the tree */
    pending := append([]error{}, roots...)

    for 0 < len(pending) && len(chain) < maxDepth {
        current := pending[0]
        pending = pending[1:]

        /* a typed-nil link contributes nothing */
        if nil == current || true == isNilInterfaceValue(current) {
            continue
        }

        chain = append(chain, renderErrorText(current))

        pending = append(pending, causesOf(current)...)
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

    return buildCauseContextChainFromRoots([]error{causeErr}, maxDepth)
}

func buildCauseContextChainFromRoots(roots []error, maxDepth int) []map[string]any {
    capacity := maxDepth
    if capacity > causeChainCapacityHint {
        capacity = causeChainCapacityHint
    }

    chain := make([]map[string]any, 0, capacity)
    hasAnyContext := false

    /* the same breadth-first walk BuildCauseChain performs, so the two chains stay index-aligned: an operator reading causeChain[2] finds its context at causeContextChain[2] whether the failure below was one wrap or a join of several */
    pending := append([]error{}, roots...)

    for 0 < len(pending) && len(chain) < maxDepth {
        current := pending[0]
        pending = pending[1:]

        /* a typed-nil link is the nil its producer meant and contributes nothing */
        if nil == current || true == isNilInterfaceValue(current) {
            continue
        }

        /* the immediate node is asserted rather than searched, so a provider's context is not repeated once per intervening wrapper */
        causeProvider, isProvider := current.(exceptioncontract.ContextProvider)
        if true == isProvider {
            causeContext := renderedContextOf(causeProvider)
            if nil != causeContext && 0 < len(causeContext) {
                chain = append(chain, causeContext)
                hasAnyContext = true
            } else {
                chain = append(chain, nil)
            }
        } else {
            chain = append(chain, nil)
        }

        pending = append(pending, causesOf(current)...)
    }

    if false == hasAnyContext {
        return nil
    }

    return chain
}
