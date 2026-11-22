package event

import (
    "errors"
    "fmt"
    "reflect"
    "runtime"
    "runtime/debug"
    "sort"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* EventDispatcherConfig is what NewEventDispatcher builds over; the zero value stamps DispatchName with the system clock. */
type EventDispatcherConfig struct {
    /* Clock stamps the events DispatchName builds; nil takes the system clock, a typed nil is refused. */
    Clock clockcontract.Clock
}

func NewEventDispatcher(config EventDispatcherConfig) *EventDispatcher {
    clockInstance := config.Clock
    if nil == clockInstance {
        clockInstance = clock.NewSystemClock()
    } else if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("EventDispatcherConfig.Clock may not be a typed nil", nil, nil),
        )
    }

    return &EventDispatcher{
        listeners:      make(map[string][]listenerWithPriority),
        subscribers:    newSubscriberBookkeeping[subscriberRegistration](),
        clock:          clockInstance,
        priorityFloors: make(map[string]int),
    }
}

/* EventDispatcher holds the listeners of every event and the installations of every subscriber, read and written for the life of the process. The listeners and the listener counter are read and written under mutex; the subscriber installations are the bookkeeping's, which serializes whole installations and removals and whose locks are taken after mutex, never before it. */
type EventDispatcher struct {
    mutex          sync.RWMutex
    listeners      map[string][]listenerWithPriority
    subscribers    *subscriberBookkeeping[subscriberRegistration]
    clock          clockcontract.Clock
    nextListenerId uint64
    /* eventName -> the priority InstallFloorListener reserved; read and written under mutex */
    priorityFloors map[string]int
}

func (instance *EventDispatcher) AddListener(
    eventName string,
    listener eventcontract.EventListener,
    config eventcontract.ListenerConfig,
) eventcontract.ListenerRegistration {
    priority := config.Priority

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

    if reservedPriority, reserved := instance.priorityFloors[eventName]; true == reserved && priority <= reservedPriority {
        instance.mutex.Unlock()

        exception.Panic(
            exception.NewError(
                "an event listener at or below the event's reserved priority can never run, so it is refused; register it above that priority",
                exceptioncontract.Context{
                    "eventName":        eventName,
                    "priority":         priority,
                    "reservedPriority": reservedPriority,
                },
                nil,
            ),
        )
    }

    registration := instance.appendListenerLocked(eventName, listener, config)

    instance.mutex.Unlock()

    return registration
}

func (instance *EventDispatcher) appendListenerLocked(
    eventName string,
    listener eventcontract.EventListener,
    config eventcontract.ListenerConfig,
) eventcontract.ListenerRegistration {
    instance.nextListenerId++
    listenerId := instance.nextListenerId

    instance.listeners[eventName] = append(
        instance.listeners[eventName],
        listenerWithPriority{
            listener:                 listener,
            listenerId:               listenerId,
            priority:                 config.Priority,
            name:                     config.Name,
            required:                 config.Required,
            maySkipRequiredListeners: config.MaySkipRequiredListeners,
        },
    )

    sort.SliceStable(
        instance.listeners[eventName],
        func(leftIndex int, rightIndex int) bool {
            if instance.listeners[eventName][leftIndex].priority == instance.listeners[eventName][rightIndex].priority {
                return instance.listeners[eventName][leftIndex].listenerId < instance.listeners[eventName][rightIndex].listenerId
            }

            return instance.listeners[eventName][leftIndex].priority > instance.listeners[eventName][rightIndex].priority
        },
    )

    return eventcontract.ListenerRegistration{
        EventName:  eventName,
        ListenerId: listenerId,
    }
}

/* InstallFloorListener registers the listener and refuses every later registration on the event at or below config.Priority, under one lock, so nothing registers between the two. An event already holding a floor keeps it, and the listener is not added. */
func (instance *EventDispatcher) InstallFloorListener(
    eventName string,
    listener eventcontract.EventListener,
    config eventcontract.ListenerConfig,
) (eventcontract.ListenerRegistration, bool) {
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name is required to install a floor listener", nil, nil),
        )
    }

    if nil == listener {
        exception.Panic(
            exception.NewError(
                "event listener is required to install a floor listener",
                exceptioncontract.Context{
                    "eventName": eventName,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, reserved := instance.priorityFloors[eventName]; true == reserved {
        return eventcontract.ListenerRegistration{}, false
    }

    registration := instance.appendListenerLocked(eventName, listener, config)

    instance.priorityFloors[eventName] = config.Priority

    return registration, true
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

    instance.subscribers.dropListener(func(registrationEntry subscriberRegistration) bool {
        return eventName == registrationEntry.eventName && listenerId == registrationEntry.listenerId
    })

    return true
}

/* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them, the only handle on that installation. Installing one subscriber twice produces two independent installations, and a value subscriber is installable, since the installation is filed under an id the dispatcher issues. A nil subscriber is refused. */
func (instance *EventDispatcher) AddSubscriber(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration {
    return instance.subscribers.addSubscriber(subscriber, instance.installSubscriberListener, instance.removeSubscriberListener)
}

/* RemoveSubscriber removes the listeners one AddSubscriber call installed and answers how many. An unknown registration, one already removed or the zero value, removes nothing and answers zero. */
func (instance *EventDispatcher) RemoveSubscriber(registration eventcontract.SubscriberRegistration) int {
    return instance.subscribers.removeSubscriber(registration, instance.removeSubscriberListener)
}

/* installSubscriberListener installs one planned listener of a subscriber through AddListener and answers the record the bookkeeping keeps for it. */
func (instance *EventDispatcher) installSubscriberListener(planned plannedSubscriberRegistration, subscriberType string) subscriberRegistration {
    registration := instance.AddListener(
        planned.eventName,
        planned.listener,
        planned.config,
    )

    return subscriberRegistration{
        eventName:      planned.eventName,
        listenerId:     registration.ListenerId,
        subscriberType: subscriberType,
    }
}

/* removeSubscriberListener removes the listener one record names and answers how many it removed. */
func (instance *EventDispatcher) removeSubscriberListener(entry subscriberRegistration) int {
    return instance.removeListenerById(entry.eventName, entry.listenerId)
}

/* Dispatch runs the listeners of the event's name in priority order until propagation is stopped or a listener fails, a returned error and a recovered panic alike: the first failure ends the dispatch. The answer is that failure itself, or the refusal of a required listener the dispatch ended before, with the failure as its cause. */
func (instance *EventDispatcher) Dispatch(runtimeInstance containercontract.Runtime, event eventcontract.Event) (eventcontract.Event, error) {
    return instance.dispatchSafely(
        runtimeInstance,
        event,
    )
}

func (instance *EventDispatcher) DispatchName(runtimeInstance containercontract.Runtime, eventName string, payload any) (eventcontract.Event, error) {
    event := NewEvent(
        eventName,
        payload,
        instance.clock.Now(),
    )

    return instance.Dispatch(
        runtimeInstance,
        event,
    )
}

/* RegisteredEvents reports a point-in-time view: a listener of an installation still running answers with no owner until it finishes. Dispatch never depends on it. */
func (instance *EventDispatcher) RegisteredEvents() []eventcontract.RegisteredEvent {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    ownerByEventNameAndId := make(map[string]map[uint64]string)

    instance.subscribers.eachRecord(func(registration subscriberRegistration) {
        byId, exists := ownerByEventNameAndId[registration.eventName]
        if false == exists {
            byId = make(map[uint64]string)
            ownerByEventNameAndId[registration.eventName] = byId
        }

        byId[registration.listenerId] = registration.subscriberType
    })

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

            listenerName := entryNameOf(entry)

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

func (instance *EventDispatcher) dispatchSafely(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* an exit carries its code on the wrapper, so it passes through unwrapped, as logging.LogOnRecover passes it */
        if exitValue, isExit := internal.RecoveredExitError(recoveredValue); true == isExit {
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
                /* the recovered value is the cause too, so an error-shaped panic keeps its context and cause chain in the record, which a context slot alone flattens to the bare message */
                exception.PanicCause(recoveredValue),
            ),
        )
    }()

    return instance.dispatch(
        runtimeInstance,
        eventValue,
    )
}

func (instance *EventDispatcher) dispatch(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    if true == internal.IsNilInterface(eventValue) {
        exception.Panic(
            exception.NewError("event may not be nil", nil, nil),
        )
    }

    eventName := eventValue.Name()
    if "" == eventName {
        exception.Panic(
            exception.NewError("event name may not be empty", exceptioncontract.Context{"eventType": reflect.TypeOf(eventValue).String()}, nil),
        )
    }

    instance.mutex.RLock()
    listenerList := instance.listeners[eventName]
    listenerListSnapshot := append([]listenerWithPriority(nil), listenerList...)
    instance.mutex.RUnlock()

    listenerList = listenerListSnapshot

    logger := logging.LoggerMustFromRuntime(runtimeInstance)

    /* asked once per dispatch: the debug records below assemble a context map a journal above debug discards, and the listener name is resolved through listenerNameOf only where used */
    debugEnabled := logger.Enabled(loggingcontract.LevelDebug)

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
    /* the entry travels rather than its name, which only a refusal reads */
    lastRanEntry := listenerWithPriority{}
    stoppedByListenerMaySkip := false
    failedIndex := -1
    failedEntry := listenerWithPriority{}
    failureErrs := make([]error, 0)

    for listenerIndex = 0; listenerIndex < len(listenerList); listenerIndex++ {
        /* propagation is tested before each listener runs, so an event that arrives already stopped runs none of them */
        if true == isEventPropagationStopped(eventValue) {
            break
        }

        entry := listenerList[listenerIndex]

        listenerStartedAt := time.Now()

        if true == debugEnabled {
            logger.Debug(
                "event listener started",
                loggingcontract.Context{
                    "eventName":        eventName,
                    "listenerName":     entryNameOf(entry),
                    "listenerPriority": entry.priority,
                },
            )
        }

        err := instance.callListenerSafely(
            runtimeInstance,
            eventName,
            eventValue,
            entry,
            listenerStartedAt,
            logger,
        )

        lastRanEntry = entry
        /* the opt-out licenses a chosen short-circuit; a listener that failed and stopped made no such choice */
        stoppedByListenerMaySkip = nil == err && true == entry.maySkipRequiredListeners

        if nil == err {
            continue
        }

        /* the first failure ends the dispatch */
        failureErrs = append(failureErrs, err)
        failedIndex = listenerIndex
        failedEntry = entry

        break
    }

    if -1 != failedIndex {
        skippedListenerList := listenerList[failedIndex+1:]

        failureErr := joinListenerErrors(failureErrs)

        if true == debugEnabled {
            logger.Debug(
                "event dispatch ended by a listener failure",
                loggingcontract.Context{
                    "eventName":          eventName,
                    "failedListenerName": entryNameOf(failedEntry),
                    "skippedCount":       len(skippedListenerList),
                },
            )
        }

        /* a failure ends the dispatch as a stop does, and the opt-out is never read for it */
        requiredErr := refuseSkippedRequiredListeners(eventName, skippedListenerList, entryNameOf(failedEntry), false, failureErr)
        if nil != requiredErr {
            return eventValue, requiredErr
        }

        return eventValue, failureErr
    }

    if true == isEventPropagationStopped(eventValue) {
        requiredErr := refuseSkippedRequiredListeners(
            eventName,
            listenerList[listenerIndex:],
            entryNameOf(lastRanEntry),
            stoppedByListenerMaySkip,
            nil,
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

/* joinListenerErrors answers nil for no failure, the failure itself for one, so its type reaches the caller unchanged, and a listenerFailures joining every failure in run order for several. */
func joinListenerErrors(listenerErrs []error) error {
    if 0 == len(listenerErrs) {
        return nil
    }

    if 1 == len(listenerErrs) {
        return listenerErrs[0]
    }

    return &listenerFailures{failures: append([]error(nil), listenerErrs...)}
}

/* listenerFailures joins several listener failures as errors.Join does and reads already logged only when every failure does, so a logged panic beside an unlogged error does not silence the caller's record of the second one. */
type listenerFailures struct {
    failures []error
}

func (instance *listenerFailures) Error() string {
    return errors.Join(instance.failures...).Error()
}

func (instance *listenerFailures) Unwrap() []error {
    return instance.failures
}

func (instance *listenerFailures) AlreadyLogged() bool {
    for _, failure := range instance.failures {
        if false == exception.IsAlreadyLogged(failure) {
            return false
        }
    }

    return true
}

func (instance *listenerFailures) MarkAsLogged() {
    for _, failure := range instance.failures {
        _ = exception.MarkLogged(failure)
    }
}

func isEventPropagationStopped(eventValue eventcontract.Event) bool {
    if true == eventValue.IsPropagationStopped() {
        return true
    }

    stopper, isStopper := eventValue.Payload().(eventcontract.PropagationStopper)

    return true == isStopper && false == internal.IsNilInterface(stopper) && true == stopper.IsPropagationStopped()
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

/* entryNameOf answers the name a registration declared, and the listener's qualified function name when it declared none. */
func entryNameOf(entry listenerWithPriority) string {
    if "" != entry.name {
        return entry.name
    }

    return listenerNameOf(entry.listener)
}

/* refuseSkippedRequiredListeners answers the error an early end of the dispatch owes, by a stop or a failure alike, when a required listener sits among those it skipped, so the dispatch fails closed; the listener that ended it may be explicitly allowed to skip them. The cause is the dispatch's failure, nil when it was a stop. */
func refuseSkippedRequiredListeners(
    eventName string,
    skippedListenerList []listenerWithPriority,
    stoppedByListenerName string,
    stoppedByListenerMaySkip bool,
    cause error,
) error {
    if true == stoppedByListenerMaySkip {
        return nil
    }

    for _, skippedEntry := range skippedListenerList {
        if true == skippedEntry.required {
            return NewRequiredListenerSkippedError(
                eventName,
                stoppedByListenerName,
                cause,
            )
        }
    }

    return nil
}

func (instance *EventDispatcher) callListenerSafely(
    runtimeInstance containercontract.Runtime,
    eventName string,
    eventValue eventcontract.Event,
    entry listenerWithPriority,
    listenerStartedAt time.Time,
    logger loggingcontract.Logger,
) (returnedErr error) {
    listener := entry.listener
    priority := entry.priority
    eventType := reflect.TypeOf(eventValue).String()
    listenerType := internal.StringifyType(listener)

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* the exit code lives on the wrapper, so an exit is not folded into a listener error */
        if exitValue, isExit := internal.RecoveredExitError(recoveredValue); true == isExit {
            exception.Exit(exitValue)
        }

        durationMs := time.Since(listenerStartedAt).Milliseconds()

        baseContext := internal.NewEventListenerContext(
            eventName,
            eventType,
            entryNameOf(entry),
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
        entryNameOf(entry),
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
    name                     string
    required                 bool
    maySkipRequiredListeners bool
}

type subscriberRegistration struct {
    eventName      string
    listenerId     uint64
    subscriberType string
}

var _ eventcontract.EventDispatcher = (*EventDispatcher)(nil)

/* requireEventSubscriber refuses a subscriber that cannot be installed and answers the type name its installation is filed under, for inspection. The nil test reads through the interface, since SubscribedEvents would dereference a typed nil. A value subscriber is installable, since the installation is filed under an id the dispatcher issues. */
func requireEventSubscriber(
    subscriber eventcontract.EventSubscriber,
    action string,
) string {
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

    return reflect.TypeOf(subscriber).String()
}

/* planSubscriberRegistrations validates every subscribed event and answers the registrations to install, so that a malformed entry is refused before any listener of the same subscriber is live. */
func planSubscriberRegistrations(subscriber eventcontract.EventSubscriber) []plannedSubscriberRegistration {
    subscriberType := reflect.TypeOf(subscriber).String()

    subscribedEvents := subscriber.SubscribedEvents()
    if nil == subscribedEvents {
        exception.Panic(
            exception.NewError("subscribed events may not be nil", exceptioncontract.Context{"subscriberType": subscriberType}, nil),
        )
    }

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
                exception.NewError("event name may not be empty", exceptioncontract.Context{"subscriberType": subscriberType}, nil),
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
                    exceptioncontract.Context{"eventName": eventName, "subscriberType": subscriberType},
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
                    config:    subscribedEvent.Config(),
                },
            )
        }
    }

    return plannedList
}

type plannedSubscriberRegistration struct {
    eventName string
    listener  eventcontract.EventListener
    config    eventcontract.ListenerConfig
}
