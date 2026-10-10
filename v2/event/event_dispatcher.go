package event

import (
    "fmt"
    "reflect"
    "runtime"
    "runtime/debug"
    "sort"
    "sync"
    "time"

    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewEventDispatcher(clock clockcontract.Clock) *EventDispatcher {
    if true == internal.IsNilInterface(clock) {
        exception.Panic(
            exception.NewError("clock may not be nil", nil, nil),
        )
    }

    return &EventDispatcher{
        listeners:               make(map[string][]listenerWithPriority),
        subscriberRegistrations: make(map[subscriberIdentity][]subscriberRegistration),
        clock:                   clock,
    }
}

type EventDispatcher struct {
    mutex                   sync.RWMutex
    listeners               map[string][]listenerWithPriority
    subscriberRegistrations map[subscriberIdentity][]subscriberRegistration
    clock                   clockcontract.Clock
    nextListenerId          uint64
    /* subscriberMutex serializes whole subscriber installations and removals against each other; it is always taken before mutex and never inside it, so a removal never interleaves with the installation it undoes. */
    subscriberMutex sync.Mutex
}

func (instance *EventDispatcher) AddListener(
    eventName string,
    listener eventcontract.EventListener,
    priority int,
) eventcontract.ListenerRegistration {
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name is required to add a listener", nil, nil),
        )
    }

    if nil == listener {
        exception.Panic(
            exception.NewError(
                "event listener is required to add a listener",
                exceptioncontract.Context{
                    "eventName": eventName,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    instance.nextListenerId++
    listenerId := instance.nextListenerId

    instance.listeners[eventName] = append(
        instance.listeners[eventName],
        listenerWithPriority{
            listener:   listener,
            listenerId: listenerId,
            priority:   priority,
        },
    )

    sort.SliceStable(
        instance.listeners[eventName],
        func(i int, j int) bool {
            if instance.listeners[eventName][i].priority == instance.listeners[eventName][j].priority {
                return instance.listeners[eventName][i].listenerId < instance.listeners[eventName][j].listenerId
            }

            return instance.listeners[eventName][i].priority > instance.listeners[eventName][j].priority
        },
    )

    instance.mutex.Unlock()

    return eventcontract.ListenerRegistration{
        EventName:  eventName,
        ListenerId: listenerId,
    }
}

/* MarkListenerRequired flags the registered listener so that, if another listener stops propagation before it runs, dispatch returns a RequiredListenerSkippedError and the caller can fail closed. An unknown registration is refused. The mark follows the registration, so a dispatch between the two sees the listener unmarked; register at boot, or do not dispatch the event until the mark is applied. */
func (instance *EventDispatcher) MarkListenerRequired(registration eventcontract.ListenerRegistration) {
    instance.markListenerFlag(registration, func(entry *listenerWithPriority) {
        entry.required = true
    })
}

/* MarkListenerMaySkipRequiredListeners flags the registered listener so that when it stops propagation it is allowed to skip required listeners behind it without failing dispatch — the explicit opt-out that restores the plain stop-and-proceed behavior for a listener that knowingly short-circuits. */
func (instance *EventDispatcher) MarkListenerMaySkipRequiredListeners(registration eventcontract.ListenerRegistration) {
    instance.markListenerFlag(registration, func(entry *listenerWithPriority) {
        entry.maySkipRequiredListeners = true
    })
}

func (instance *EventDispatcher) markListenerFlag(
    registration eventcontract.ListenerRegistration,
    apply func(entry *listenerWithPriority),
) {
    eventName := registration.EventName
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name is required to mark a listener", nil, nil),
        )
    }

    listenerId := registration.ListenerId
    if 0 == listenerId {
        exception.Panic(
            exception.NewError(
                "event listener id is required to mark a listener",
                exceptioncontract.Context{
                    "eventName": eventName,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    entries := instance.listeners[eventName]
    for index := range entries {
        if entries[index].listenerId == listenerId {
            apply(&entries[index])

            return
        }
    }

    exception.Panic(
        exception.NewError(
            "event listener registration is not registered",
            exceptioncontract.Context{
                "eventName":  eventName,
                "listenerId": listenerId,
            },
            nil,
        ),
    )
}

func (instance *EventDispatcher) RemoveListener(registration eventcontract.ListenerRegistration) bool {
    eventName := registration.EventName
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name is required to remove a listener", nil, nil),
        )
    }

    listenerId := registration.ListenerId
    if 0 == listenerId {
        exception.Panic(
            exception.NewError(
                "event listener id is required to remove a listener",
                exceptioncontract.Context{
                    "eventName": eventName,
                },
                nil,
            ),
        )
    }

    removedCount := instance.removeListenerById(
        eventName,
        listenerId,
    )
    if 0 == removedCount {
        return false
    }

    instance.mutex.Lock()
    for subscriberIdentityValue, registrationList := range instance.subscriberRegistrations {
        filtered := make([]subscriberRegistration, 0, len(registrationList))
        for _, registrationEntry := range registrationList {
            if eventName == registrationEntry.eventName && listenerId == registrationEntry.listenerId {
                continue
            }

            filtered = append(filtered, registrationEntry)
        }

        if 0 == len(filtered) {
            delete(instance.subscriberRegistrations, subscriberIdentityValue)
            continue
        }

        instance.subscriberRegistrations[subscriberIdentityValue] = filtered
    }
    instance.mutex.Unlock()

    return true
}

func (instance *EventDispatcher) AddSubscriber(subscriber eventcontract.EventSubscriber) {
    subscriberIdentityValue, subscriberType := requireEventSubscriberIdentity(
        subscriber,
        "add a subscriber",
    )

    /* every subscribed event is validated before a single listener is registered, so a malformed subscriber is never half-installed */
    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.Lock()
    _, alreadyRegistered := instance.subscriberRegistrations[subscriberIdentityValue]
    instance.mutex.Unlock()

    /* a subscriber that carries no fields occupies no memory and every zero-size allocation answers one address, so two instances of such a type are one identity here; the second registration is refused, or a later RemoveSubscriber for either instance would take both instances' listeners down */
    if true == alreadyRegistered {
        exception.Panic(
            exception.NewError(
                "event subscriber is already registered",
                exceptioncontract.Context{
                    "subscriberType": subscriberType,
                },
                nil,
            ),
        )
    }

    for _, planned := range plannedList {
        registration := instance.AddListener(
            planned.eventName,
            planned.listener,
            planned.priority,
        )

        instance.mutex.Lock()
        instance.subscriberRegistrations[subscriberIdentityValue] = append(
            instance.subscriberRegistrations[subscriberIdentityValue],
            subscriberRegistration{
                eventName:      planned.eventName,
                listenerId:     registration.ListenerId,
                subscriberType: subscriberType,
            },
        )
        instance.mutex.Unlock()
    }
}

func (instance *EventDispatcher) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    subscriberIdentityValue, _ := requireEventSubscriberIdentity(
        subscriber,
        "remove a subscriber",
    )

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.Lock()
    registrationList := instance.subscriberRegistrations[subscriberIdentityValue]
    delete(instance.subscriberRegistrations, subscriberIdentityValue)
    instance.mutex.Unlock()

    removedCount := 0
    for _, registration := range registrationList {
        removedCount = removedCount + instance.removeListenerById(
            registration.eventName,
            registration.listenerId,
        )
    }

    return removedCount
}

func (instance *EventDispatcher) Dispatch(runtimeInstance runtimecontract.Runtime, event eventcontract.Event) (eventcontract.Event, error) {
    return instance.dispatchSafely(
        runtimeInstance,
        event,
    )
}

func (instance *EventDispatcher) DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (eventcontract.Event, error) {
    event := NewEvent(
        eventName,
        payload,
        instance.clock,
    )

    return instance.Dispatch(
        runtimeInstance,
        event,
    )
}

/* RegisteredEvents reports a point-in-time view: a listener of a subscriber installation still running is live with no owner recorded until the installation finishes. Dispatch never depends on this view. */
func (instance *EventDispatcher) RegisteredEvents() []eventcontract.RegisteredEvent {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    ownerByEventNameAndId := make(map[string]map[uint64]string)

    for _, registrationList := range instance.subscriberRegistrations {
        for _, registration := range registrationList {
            byId, exists := ownerByEventNameAndId[registration.eventName]
            if false == exists {
                byId = make(map[uint64]string)
                ownerByEventNameAndId[registration.eventName] = byId
            }

            byId[registration.listenerId] = registration.subscriberType
        }
    }

    eventNameList := make([]string, 0, len(instance.listeners))
    for eventName := range instance.listeners {
        eventNameList = append(eventNameList, eventName)
    }

    sort.Strings(eventNameList)

    registeredEvents := make([]eventcontract.RegisteredEvent, 0, len(eventNameList))

    for _, eventName := range eventNameList {
        listenerList := instance.listeners[eventName]

        registeredListenerList := make([]eventcontract.RegisteredListener, 0, len(listenerList))

        for _, entry := range listenerList {
            source := eventcontract.RegisteredListenerSourceListener
            owner := "-"

            byId, exists := ownerByEventNameAndId[eventName]
            if true == exists {
                ownerValue, exists := byId[entry.listenerId]
                if true == exists {
                    source = eventcontract.RegisteredListenerSourceSubscriber
                    owner = ownerValue
                }
            }

            listenerId := fmt.Sprintf("%d", entry.listenerId)

            listenerName := listenerNameOf(entry.listener)

            registeredListenerList = append(
                registeredListenerList,
                eventcontract.RegisteredListener{
                    Priority:                 entry.priority,
                    Source:                   source,
                    Owner:                    owner,
                    ListenerId:               listenerId,
                    ListenerName:             listenerName,
                    Required:                 entry.required,
                    MaySkipRequiredListeners: entry.maySkipRequiredListeners,
                },
            )
        }

        registeredEvents = append(
            registeredEvents,
            eventcontract.RegisteredEvent{
                EventName: eventName,
                Listeners: registeredListenerList,
            },
        )
    }

    return registeredEvents
}

func (instance *EventDispatcher) dispatchSafely(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* an exit carries its code on the wrapper, and wrapping it in an ordinary error here would turn a deliberate exit code into the generic one the process boundary falls back to; logging.LogOnRecover passes it through for the same reason */
        exitValue, isExit := recoveredValue.(*exception.ExitError)
        if true == isExit && nil != exitValue {
            exception.Exit(exitValue)
        }

        exceptionValue, ok := recoveredValue.(*exception.Error)
        if true == ok && nil != exceptionValue {
            exception.Panic(exceptionValue)
        }

        eventName := "-"
        eventType := "-"

        /* the test reads through the interface, since a typed nil would dereference on Name() below, a second panic inside the recovery */
        if false == internal.IsNilInterface(eventValue) {
            eventName = eventValue.Name()

            eventTypeValue := reflect.TypeOf(eventValue)
            if nil != eventTypeValue {
                eventType = eventTypeValue.String()
            }
        }

        exception.Panic(
            exception.NewError(
                "event dispatch panicked",
                exceptioncontract.Context{
                    "eventName":      eventName,
                    "eventType":      eventType,
                    "recoveredValue": recoveredValue,
                    "panicStack":     string(debug.Stack()),
                },
                nil,
            ),
        )
    }()

    return instance.dispatch(
        runtimeInstance,
        eventValue,
    )
}

func (instance *EventDispatcher) dispatch(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    if true == internal.IsNilInterface(eventValue) {
        exception.Panic(
            exception.NewError("event may not be nil", nil, nil),
        )
    }

    eventName := eventValue.Name()
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name may not be empty", nil, nil),
        )
    }

    instance.mutex.RLock()
    listenerList := instance.listeners[eventName]
    listenerListSnapshot := append([]listenerWithPriority(nil), listenerList...)
    instance.mutex.RUnlock()

    listenerList = listenerListSnapshot

    logger := logging.LoggerMustFromRuntime(runtimeInstance)

    /* asked once per dispatch: the debug records below assemble a context map a journal above debug discards, and the listener name is resolved through listenerNameOf only where used. A logger that cannot answer reports enabled. */
    debugEnabled := logging.LevelEnabled(logger, loggingcontract.LevelDebug)

    dispatchStartedAt := time.Now()

    if true == debugEnabled {
        logger.Debug(
            "event dispatch started",
            loggingcontract.Context{
                "eventName":      eventName,
                "listenersCount": len(listenerList),
            },
        )
    }

    listenerIndex := 0
    stoppedByListener := eventcontract.EventListener(nil)
    stoppedByListenerMaySkip := false

    for listenerIndex = 0; listenerIndex < len(listenerList); listenerIndex++ {
        /* propagation is tested before each listener runs, so an event that arrives already stopped runs none of them */
        if true == eventValue.IsPropagationStopped() {
            break
        }

        entry := listenerList[listenerIndex]

        listenerStartedAt := time.Now()

        if true == debugEnabled {
            logger.Debug(
                "event listener started",
                loggingcontract.Context{
                    "eventName":        eventName,
                    "listenerName":     listenerNameOf(entry.listener),
                    "listenerPriority": entry.priority,
                },
            )
        }

        err := instance.callListenerSafely(
            runtimeInstance,
            eventName,
            eventValue,
            entry.listener,
            entry.priority,
            listenerStartedAt,
            logger,
        )
        if nil != err {
            listenerName := listenerNameOf(entry.listener)

            /* a failing listener ends the dispatch as decisively as a stop, so a required listener behind it is reported skipped ahead of the failure, which travels as the cause on both branches. The opt-out is not read here: MarkListenerMaySkipRequiredListeners licenses a chosen short-circuit, a failure is not one, and the kernel tells the two apart by the type of the error. */
            requiredErr := refuseSkippedRequiredListeners(
                eventName,
                listenerList[listenerIndex+1:],
                listenerName,
                false,
            )
            if nil != requiredErr {
                if true == eventValue.IsPropagationStopped() {
                    return eventValue, NewRequiredListenerSkippedErrorWithStoppedListenerFailure(eventName, listenerName, err)
                }

                return eventValue, NewRequiredListenerSkippedErrorWithCause(eventName, listenerName, err)
            }

            return eventValue, err
        }

        /* the listener travels rather than its name, which only the refusal below reads */
        stoppedByListener = entry.listener
        stoppedByListenerMaySkip = entry.maySkipRequiredListeners
    }

    if true == eventValue.IsPropagationStopped() {
        requiredErr := refuseSkippedRequiredListeners(
            eventName,
            listenerList[listenerIndex:],
            listenerNameOf(stoppedByListener),
            stoppedByListenerMaySkip,
        )
        if nil != requiredErr {
            return eventValue, requiredErr
        }

        if true == debugEnabled {
            logger.Debug(
                "event dispatch propagation stopped",
                loggingcontract.Context{
                    "eventName": eventName,
                },
            )
        }
    }

    if true == debugEnabled {
        logger.Debug(
            "event dispatch finished",
            loggingcontract.Context{
                "eventName":  eventName,
                "durationMs": time.Since(dispatchStartedAt).Milliseconds(),
            },
        )
    }

    return eventValue, nil
}

/* listenerNameOf answers the qualified function name of a listener, and the dash for one the runtime cannot name or for nil. It is called only where the name is used, since the reflection behind it runs per listener per dispatch. */
func listenerNameOf(listener eventcontract.EventListener) string {
    if nil == listener {
        return "-"
    }

    function := runtime.FuncForPC(reflect.ValueOf(listener).Pointer())
    if nil == function {
        return "-"
    }

    return function.Name()
}

/* refuseSkippedRequiredListeners answers the error an early end of the dispatch owes, by a stop or a failure alike, when a required listener sits among those it skipped, so the dispatch fails closed; the listener that ended it may be explicitly allowed to skip them. */
func refuseSkippedRequiredListeners(
    eventName string,
    skippedListenerList []listenerWithPriority,
    stoppedByListenerName string,
    stoppedByListenerMaySkip bool,
) error {
    if true == stoppedByListenerMaySkip {
        return nil
    }

    for _, skippedEntry := range skippedListenerList {
        if true == skippedEntry.required {
            return NewRequiredListenerSkippedError(
                eventName,
                stoppedByListenerName,
            )
        }
    }

    return nil
}

func (instance *EventDispatcher) callListenerSafely(
    runtimeInstance runtimecontract.Runtime,
    eventName string,
    eventValue eventcontract.Event,
    listener eventcontract.EventListener,
    priority int,
    listenerStartedAt time.Time,
    logger loggingcontract.Logger,
) (returnedErr error) {
    eventType := reflect.TypeOf(eventValue).String()
    listenerType := internal.StringifyType(listener)

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* the exit code lives on the wrapper, and folding it into a listener error would leave a deliberate exit as an ordinary request failure with the code gone */
        exitValue, isExit := recoveredValue.(*exception.ExitError)
        if true == isExit && nil != exitValue {
            exception.Exit(exitValue)
        }

        durationMs := time.Since(listenerStartedAt).Milliseconds()

        baseContext := internal.NewEventListenerContext(
            eventName,
            eventType,
            listenerNameOf(listener),
            listenerType,
            priority,
            durationMs,
        )

        exceptionContext := internal.NewEventListenerPanicContext(
            baseContext,
            recoveredValue,
            fmt.Sprintf("%T", recoveredValue),
            string(debug.Stack()),
        )

        /* an error-shaped panic value travels as the cause, so its context and chain reach the record; a typed nil reads as no cause */
        var panicCause error
        recoveredErr, isRecoveredError := recoveredValue.(error)
        if true == isRecoveredError && false == internal.IsNilInterface(recoveredErr) {
            panicCause = recoveredErr
        }

        exceptionErr := exception.NewError(
            "event listener panicked",
            exceptionContext,
            panicCause,
        )

        /* a panic value already logged by whoever raised it is not logged twice; the record goes through LogError, and the wrapper is marked only after it is written */
        if false == recoveredValueIsAlreadyLogged(recoveredValue) {
            logging.LogError(logger, exceptionErr)
        }
        _ = exception.MarkLogged(exceptionErr)

        returnedErr = exceptionErr
    }()

    listenerErr := listener(runtimeInstance, eventValue)

    /* the test reads through the interface, since a typed-nil error from a listener reports success, and reading it as a failure would fail the request closed */
    if true == internal.IsNilInterface(listenerErr) {
        return nil
    }

    durationMs := time.Since(listenerStartedAt).Milliseconds()

    exceptionContext := internal.NewEventListenerContext(
        eventName,
        eventType,
        listenerNameOf(listener),
        listenerType,
        priority,
        durationMs,
    )

    /* the failure travels unlogged and unmarked by this site, so the caller's single record, which renders the cause chain, names it; the context rides on the error. A failure its producer already logged passes the mark to the wrapper. */
    wrapperErr := exception.NewError(
        "event listener returned error",
        exceptionContext,
        listenerErr,
    )

    if true == exception.IsAlreadyLogged(listenerErr) {
        _ = exception.MarkLogged(wrapperErr)
    }

    return wrapperErr
}

/* recoveredValueIsAlreadyLogged reads the mark at the depth MarkLogged writes it, as the returned-error path and the logging package's recover helpers do. */
func recoveredValueIsAlreadyLogged(recoveredValue any) bool {
    recoveredErr, isError := recoveredValue.(error)
    if true == isError && false == internal.IsNilInterface(recoveredErr) {
        return exception.IsAlreadyLogged(recoveredErr)
    }

    alreadyLogged, ok := recoveredValue.(exceptioncontract.AlreadyLogged)
    if false == ok || true == internal.IsNilInterface(alreadyLogged) {
        return false
    }

    return alreadyLogged.AlreadyLogged()
}

func (instance *EventDispatcher) removeListenerById(eventName string, listenerId uint64) int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    listenerList, exists := instance.listeners[eventName]
    if false == exists {
        return 0
    }

    filtered := make([]listenerWithPriority, 0, len(listenerList))
    removedCount := 0

    for _, entry := range listenerList {
        if listenerId == entry.listenerId {
            removedCount++
            continue
        }

        filtered = append(filtered, entry)
    }

    if 0 == len(filtered) {
        delete(instance.listeners, eventName)
        return removedCount
    }

    instance.listeners[eventName] = filtered

    return removedCount
}

type listenerWithPriority struct {
    listener                 eventcontract.EventListener
    listenerId               uint64
    priority                 int
    required                 bool
    maySkipRequiredListeners bool
}

type subscriberRegistration struct {
    eventName      string
    listenerId     uint64
    subscriberType string
}

type subscriberIdentity struct {
    pointer        uintptr
    subscriberType reflect.Type
}

var _ eventcontract.EventDispatcher = (*EventDispatcher)(nil)
var _ eventcontract.EventDispatcherInspector = (*EventDispatcher)(nil)
var _ eventcontract.RequiredListenerRegistrar = (*EventDispatcher)(nil)

/* requireEventSubscriberIdentity answers the identity a subscriber is filed under, refusing everything that cannot be filed. The nil test reads through the interface, since a typed nil passes a plain comparison and would dereference inside its own SubscribedEvents call before the identity guard names the mistake. */
func requireEventSubscriberIdentity(
    subscriber eventcontract.EventSubscriber,
    action string,
) (subscriberIdentity, string) {
    if true == internal.IsNilInterface(subscriber) {
        exception.Panic(
            exception.NewError(
                "event subscriber may not be nil",
                exceptioncontract.Context{
                    "action": action,
                },
                nil,
            ),
        )
    }

    subscriberType := reflect.TypeOf(subscriber).String()

    subscriberIdentityValue := eventSubscriberIdentity(subscriber)
    if 0 == subscriberIdentityValue.pointer {
        exception.Panic(
            exception.NewError(
                "event subscriber pointer is required to "+action,
                exceptioncontract.Context{
                    "subscriberType": subscriberType,
                },
                nil,
            ),
        )
    }

    return subscriberIdentityValue, subscriberType
}

/* planSubscriberRegistrations validates every subscribed event and answers the registrations to install, so that a malformed entry is refused before any listener of the same subscriber is live. */
func planSubscriberRegistrations(subscriber eventcontract.EventSubscriber) []plannedSubscriberRegistration {
    subscribedEvents := subscriber.SubscribedEvents()
    if nil == subscribedEvents {
        exception.Panic(
            exception.NewError("subscribed events may not be nil", nil, nil),
        )
    }

    subscriberType := reflect.TypeOf(subscriber).String()

    if 0 == len(subscribedEvents) {
        exception.Panic(
            exception.NewError(
                "event subscriber declares no subscribed events",
                exceptioncontract.Context{
                    "subscriberType": subscriberType,
                },
                nil,
            ),
        )
    }

    eventNameList := make([]string, 0, len(subscribedEvents))
    for eventName := range subscribedEvents {
        if "" == eventName {
            exception.Panic(
                exception.NewError("event name may not be empty", nil, nil),
            )
        }

        eventNameList = append(eventNameList, eventName)
    }

    sort.Strings(eventNameList)

    plannedList := make([]plannedSubscriberRegistration, 0, len(eventNameList))

    for _, eventName := range eventNameList {
        subscribedEventList := subscribedEvents[eventName]
        if nil == subscribedEventList {
            exception.Panic(
                exception.NewError(
                    "subscribed event list may not be nil",
                    exceptioncontract.Context{"eventName": eventName},
                    nil,
                ),
            )
        }

        /* an event name mapped to no subscribed events would register nothing while reporting success */
        if 0 == len(subscribedEventList) {
            exception.Panic(
                exception.NewError(
                    "subscribed event list may not be empty",
                    exceptioncontract.Context{
                        "eventName":      eventName,
                        "subscriberType": subscriberType,
                    },
                    nil,
                ),
            )
        }

        for index, subscribedEvent := range subscribedEventList {
            if true == internal.IsNilInterface(subscribedEvent) {
                exception.Panic(
                    exception.NewError(
                        "subscribed event may not be nil",
                        exceptioncontract.Context{
                            "eventName": eventName,
                            "index":     index,
                        },
                        nil,
                    ),
                )
            }

            listener := subscribedEvent.Listener()
            if nil == listener {
                exception.Panic(
                    exception.NewError(
                        "subscribed event listener is required",
                        exceptioncontract.Context{
                            "eventName": eventName,
                            "index":     index,
                        },
                        nil,
                    ),
                )
            }

            plannedList = append(
                plannedList,
                plannedSubscriberRegistration{
                    eventName: eventName,
                    listener:  listener,
                    priority:  subscribedEvent.Priority(),
                },
            )
        }
    }

    return plannedList
}

type plannedSubscriberRegistration struct {
    eventName string
    listener  eventcontract.EventListener
    priority  int
}

func eventSubscriberIdentity(subscriber eventcontract.EventSubscriber) subscriberIdentity {
    if true == internal.IsNilInterface(subscriber) {
        return subscriberIdentity{}
    }

    subscriberValue := reflect.ValueOf(subscriber)
    if reflect.Ptr != subscriberValue.Kind() {
        return subscriberIdentity{}
    }

    return subscriberIdentity{
        pointer:        subscriberValue.Pointer(),
        subscriberType: subscriberValue.Type(),
    }
}
