package exception

import (
    "fmt"
    "reflect"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* isNilInterfaceValue duplicates internal.IsNilInterface, since that package imports this one */
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
    if true == isNilInterfaceValue(err) {
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
    if true == ChainAs(err, &provider) && false == isNilInterfaceValue(provider) {
        errorContext := renderedContextOf(provider)
        for key, value := range errorContext {
            if "error" == key {
                continue
            }

            context[key] = value
        }
    }

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
    if true == isNilInterfaceValue(err) {
        return nil
    }

    exceptionError, ok := err.(*Error)
    if true == ok {
        return exceptionError
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == ChainAs(err, &provider) && false == isNilInterfaceValue(provider) {
        context = renderedContextOf(provider)
    }

    return NewError(renderErrorText(err), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
    if true == isNilInterfaceValue(err) {
        return nil
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == ChainAs(err, &provider) && false == isNilInterfaceValue(provider) {
        context = renderedContextOf(provider)
    }

    return newWithLevel(renderErrorText(err), context, err, level)
}

func FromErrorWithLevelAndContext(err error, level loggingcontract.Level, context exceptioncontract.Context) *Error {
    if true == isNilInterfaceValue(err) {
        return nil
    }

    mergedContext := make(exceptioncontract.Context)

    var provider exceptioncontract.ContextProvider
    if true == ChainAs(err, &provider) && false == isNilInterfaceValue(provider) {
        for key, value := range renderedContextOf(provider) {
            mergedContext[key] = value
        }
    }

    for key, value := range context {
        mergedContext[key] = value
    }

    return newWithLevel(renderErrorText(err), mergedContext, err, level)
}

func renderedContextOf(provider exceptioncontract.ContextProvider) (context exceptioncontract.Context) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        context = exceptioncontract.Context{
            "contextPanicked": describeRecoveredValue(recoveredValue),
        }
    }()

    return provider.Context()
}

/* rendered under a recover: LogContext runs inside recovery defers, where a panicking Error would unwind past the recovery */
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

func MarkLogged(err error) error {
    if true == isNilInterfaceValue(err) {
        return err
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if true == ChainAs(err, &alreadyLoggedValue) && false == isNilInterfaceValue(alreadyLoggedValue) {
        markContained(alreadyLoggedValue)
    }

    return err
}

func markContained(alreadyLoggedValue exceptioncontract.AlreadyLogged) {
    defer func() {
        _ = recover()
    }()

    alreadyLoggedValue.MarkAsLogged()
}

/* Logged marks the chain's AlreadyLogged implementer in place; a chain carrying none is wrapped in a marked melody error. */
func Logged(err error) error {
    if true == isNilInterfaceValue(err) {
        return err
    }

    _ = MarkLogged(err)

    if true == IsAlreadyLogged(err) {
        return err
    }

    return MarkLogged(FromError(err))
}

func IsAlreadyLogged(err error) bool {
    if true == isNilInterfaceValue(err) {
        return false
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if false == ChainAs(err, &alreadyLoggedValue) || true == isNilInterfaceValue(alreadyLoggedValue) {
        return false
    }

    return reportsAlreadyLogged(alreadyLoggedValue)
}

func reportsAlreadyLogged(alreadyLoggedValue exceptioncontract.AlreadyLogged) (alreadyLogged bool) {
    defer func() {
        if nil != recover() {
            alreadyLogged = false
        }
    }()

    return alreadyLoggedValue.AlreadyLogged()
}

type unwrapPanicked struct {
    recoveredValue any
}

func (instance *unwrapPanicked) Error() string {
    return fmt.Sprintf("the links below could not be read, their Unwrap panicked: %v", instance.recoveredValue)
}

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

const causeChainCapacityHint = 8

func causesOf(err error) (causeErrs []error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        causeErrs = []error{&unwrapPanicked{recoveredValue: recoveredValue}}
    }()

    if singleUnwrapper, isSingleUnwrapper := err.(interface{ Unwrap() error }); true == isSingleUnwrapper {
        causeErr := singleUnwrapper.Unwrap()
        if true == isNilInterfaceValue(causeErr) {
            return nil
        }

        return []error{causeErr}
    }

    multiUnwrapper, isMultiUnwrapper := err.(interface{ Unwrap() []error })
    if false == isMultiUnwrapper {
        return nil
    }

    branches := multiUnwrapper.Unwrap()

    causeErrs = make([]error, 0, len(branches))
    for _, causeErr := range branches {
        if true == isNilInterfaceValue(causeErr) {
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
    if true == isNilInterfaceValue(causeErr) {
        return nil
    }

    if 0 >= maxDepth {
        return []string{renderErrorText(causeErr)}
    }

    return buildCauseChainFromRoots([]error{causeErr}, maxDepth)
}

/* walkCauseChain keeps both chains index-aligned; maxDepth bounds the links visited, not the depth of the tree */
func walkCauseChain(roots []error, maxDepth int, visit func(link error)) {
    pending := append([]error{}, roots...)
    visited := 0
    links := newChainLinkSet()

    for 0 < len(pending) && visited < maxDepth {
        current := pending[0]
        pending = pending[1:]

        if true == isNilInterfaceValue(current) || true == links.seen(current) {
            continue
        }

        visit(current)
        visited++

        pending = append(pending, causesOf(current)...)
    }
}

func causeChainCapacity(maxDepth int) int {
    if maxDepth > causeChainCapacityHint {
        return causeChainCapacityHint
    }

    return maxDepth
}

func buildCauseChainFromRoots(roots []error, maxDepth int) []string {
    chain := make([]string, 0, causeChainCapacity(maxDepth))

    walkCauseChain(roots, maxDepth, func(link error) {
        chain = append(chain, renderErrorText(link))
    })

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
    chain := make([]map[string]any, 0, causeChainCapacity(maxDepth))
    hasAnyContext := false

    walkCauseChain(roots, maxDepth, func(link error) {
        causeProvider, isProvider := link.(exceptioncontract.ContextProvider)
        if false == isProvider {
            chain = append(chain, nil)

            return
        }

        causeContext := renderedContextOf(causeProvider)
        if nil == causeContext || 0 == len(causeContext) {
            chain = append(chain, nil)

            return
        }

        chain = append(chain, causeContext)
        hasAnyContext = true
    })

    if false == hasAnyContext {
        return nil
    }

    return chain
}

/* describeRecoveredValue does what internal.DescribeRecoveredValue does for the packages that can import it */
func describeRecoveredValue(value any) (text string) {
    defer func() {
        if nil == recover() {
            return
        }

        text = fmt.Sprintf("a value of type %T whose rendering panicked", value)
    }()

    return fmt.Sprintf("%v", value)
}
