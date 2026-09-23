package exception

import (
    "errors"
    "fmt"
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

/* LogContext assembles the loggable context of an error: its message under "error", the context of the nearest ContextProvider in its chain, the cause chain walked from the error's own wrap link, and every extra map merged on top in order, later entries winning. */
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
    if true == chainHolds(err, &provider) && false == isNilInterfaceValue(provider) {
        errorContext := renderedContextOf(provider)
        for key, value := range errorContext {
            if "error" == key {
                continue
            }

            context[key] = value
        }
    }

    /* the chain is anchored on the top error's own wrap links, not on the nearest *Error a deep search would find: anchoring deep drops the context of every link above it. Links, plural, because a joined error has several of them and taking only the first would keep exactly the branch the reader already sees rendered in the message */
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
    if true == chainHolds(err, &provider) && false == isNilInterfaceValue(provider) {
        context = renderedContextOf(provider)
    }

    /* the message is rendered under a recover, as LogContext renders it: these doors run inside the kernel's recovery defers, where a second panic — an Error() that dereferences what the first panic left nil — would unwind past the 500 and the kernel.exception dispatch that defer exists to produce */
    return NewError(renderErrorText(err), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
    if true == isNilInterfaceValue(err) {
        return nil
    }

    var context exceptioncontract.Context

    var provider exceptioncontract.ContextProvider
    if true == chainHolds(err, &provider) && false == isNilInterfaceValue(provider) {
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
    if true == chainHolds(err, &provider) && false == isNilInterfaceValue(provider) {
        for key, value := range renderedContextOf(provider) {
            mergedContext[key] = value
        }
    }

    for key, value := range context {
        mergedContext[key] = value
    }

    return newWithLevel(renderErrorText(err), mergedContext, err, level)
}

/* renderedContextOf reads a provider's context under a recover, the way renderErrorText reads the message: LogContext runs inside recovery defers, and the From* constructors run on the same paths, so a foreign Context() that panics — typically on the very field that made the error worth raising — would otherwise raise a second panic past the recovery that is reporting the first. A panicking context costs the context and nothing else, with the panic value kept in its place. */
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

/* MarkLogged marks the nearest AlreadyLogged implementer in the chain — the depth IsAlreadyLogged reads the mark back from — and returns the error unchanged. The mark lives on that implementer whoever wraps it, so an error one value is shared by several readers is marked for all of them once one has filed it: the container's creation guard relies on exactly that, handing every caller coalesced on one failed creation a wrapper over the same failure so the owner's record is not filed once per waiter. The sharing ends with the creation — a later resolution runs the provider again and carries a fresh, unmarked failure. */
func MarkLogged(err error) error {
    if true == isNilInterfaceValue(err) {
        return err
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if true == chainHolds(err, &alreadyLoggedValue) && false == isNilInterfaceValue(alreadyLoggedValue) {
        markContained(alreadyLoggedValue)
    }

    return err
}

/* markContained leaves the mark under a recover: MarkLogged runs in the recovery defers that file a failure, and a foreign MarkAsLogged that panics would raise a second panic past the recovery reporting the first. A mark that could not be left costs a second record of the same failure, never the first. */
func markContained(alreadyLoggedValue exceptioncontract.AlreadyLogged) {
    defer func() {
        _ = recover()
    }()

    alreadyLoggedValue.MarkAsLogged()
}

/* Logged answers an error that reports itself already logged, and is what a writer returns after filing its record. An error whose chain carries an AlreadyLogged implementer is marked in place and handed back unchanged, so its identity — and every errors.Is and errors.As its readers perform on it — survives. An error whose chain carries none has nowhere for the mark to live: errors.New, fmt.Errorf and every runtime error make MarkLogged a silent no-op, and the next reader then files the same failure a second time. That error is wrapped in a marked melody error keeping it as its cause, so the mark the writer meant to leave is the mark the reader finds. The wrap cannot change how a status is resolved: it happens exactly when no HttpException is in the chain, which is exactly when the status was already going to be the generic one. */
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

/* IsAlreadyLogged reads the mark at the depth MarkLogged writes it. It is the single reader: reading the mark off a concrete *Error instead misses a marked HttpException and anything wrapping a marked error, which are then rendered a second time. */
func IsAlreadyLogged(err error) bool {
    if true == isNilInterfaceValue(err) {
        return false
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if false == chainHolds(err, &alreadyLoggedValue) || true == isNilInterfaceValue(alreadyLoggedValue) {
        return false
    }

    return reportsAlreadyLogged(alreadyLoggedValue)
}

/* reportsAlreadyLogged reads the mark under a recover, for the reason markContained leaves it under one: a foreign AlreadyLogged that panics answers not logged, which files the failure once more rather than losing it. */
func reportsAlreadyLogged(alreadyLoggedValue exceptioncontract.AlreadyLogged) (alreadyLogged bool) {
    defer func() {
        if nil != recover() {
            alreadyLogged = false
        }
    }()

    return alreadyLoggedValue.AlreadyLogged()
}

/* chainHolds is errors.As under a recover, and the one door through which this package searches a chain. errors.As calls the Unwrap and the As of every link it passes, and the chain is whatever a failure carried — a recovered panic value included — so a link whose Unwrap or As panics raised a second panic inside the defer that had already spent its recover. On the bare goroutine of a worker that is the end of the process, raised by the reader writing down why a single item failed. A chain that cannot be searched answers that it holds nothing: the reader keeps the message, rendered under its own recover, and loses what that link would have added. */
func chainHolds(err error, target any) (found bool) {
    defer func() {
        if nil != recover() {
            found = false
        }
    }()

    return errors.As(err, target)
}

/* unwrapPanicked stands in a walked chain for the links an Unwrap that panicked would have answered, so the record says that the chain was cut and by what rather than ending at the link as if it had no cause. It exists only in the chains this package renders, never in the error a caller holds. */
type unwrapPanicked struct {
    recoveredValue any
}

func (instance *unwrapPanicked) Error() string {
    return fmt.Sprintf("the links below could not be read, their Unwrap panicked: %v", instance.recoveredValue)
}

/* PanicCause reads a recovered panic value as the cause of the error a recovery boundary fabricates in its place. An error-shaped panic value belongs in the cause slot, not in a context slot: kept only in the context it collapses to its bare message at the render boundary — the json logger stringifies an error it finds in a context — so the context map and the cause chain of the very error that was raised reach no record at all, and the reason a write failed is gone while the stack that says where survives. A typed nil answers no cause, because its Error() would dereference a nil receiver at the first render, and a panic value that is not an error has no cause to give. */
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

/* causeChainCapacityHint bounds the pre-allocated capacity of the cause-chain builders; the walk still honours the caller's maxDepth. An unclamped maxDepth of math.MaxInt panics in makeslice. */
const causeChainCapacityHint = 8

/* causesOf answers the links below an error, in both the shapes the standard library defines: the single Unwrap() error a wrap produces, and the Unwrap() []error an errors.Join produces. Every reader here anchored on errors.Unwrap alone, which answers nothing at all for a joined error — so a failure that gathered what several replicas, several destinations or several rules had to say reached the record as one flattened line of text, with the context of every branch and every link beneath them gone. The writers this framework repaired are one producer of the shape; a joined error can arrive from any dependency and from any application, and it is the readers that were blind to all of them.

   The single form is tried first because it is the overwhelmingly common one and answers without allocating, and a link that carries both is a wrap whose own Unwrap wins, which is what errors.Is and errors.As do with it too.

   Both Unwrap forms are foreign code called from the recovery defers that file a failure, so they run under a recover, as Error and Context do: an Unwrap that panics answers one unwrapPanicked link in place of its causes, and the walk ends there with the reason written into the chain. */
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

/* walkCauseChain is the one walk both chains take, so they stay index-aligned by construction rather than by two hand-copied loops an edit could part. It is breadth-first over both unwrap shapes, so a chain of single wraps produces exactly the sequence it always did while a join contributes its branches side by side instead of ending the chain at its own link. maxDepth bounds the number of LINKS visited, not the depth of the tree, which is what keeps a wide join from costing more than a deep chain. */
func walkCauseChain(roots []error, maxDepth int, visit func(link error)) {
    pending := append([]error{}, roots...)
    visited := 0

    for 0 < len(pending) && visited < maxDepth {
        current := pending[0]
        pending = pending[1:]

        /* a typed-nil link is the nil its producer meant and contributes nothing */
        if true == isNilInterfaceValue(current) {
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

    /* the same walk BuildCauseChain takes, so the two chains stay index-aligned: an operator reading causeChain[2] finds its context at causeContextChain[2] whether the failure below was one wrap or a join of several */
    walkCauseChain(roots, maxDepth, func(link error) {
        /* the immediate node is asserted rather than searched with errors.As: a deep search emits the nearest provider's context once per intervening wrapper, while the cursor advances one link at a time */
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
