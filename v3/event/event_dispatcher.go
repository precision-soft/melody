package event

import (
    "fmt"
    "reflect"
    "runtime"
    "runtime/debug"
    "sort"
    "sync"
    "time"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewEventDispatcher(clock clockcontract.Clock) *EventDispatcher {
    if nil == clock {
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

/* MarkListenerRequired flags the registered listener so that if another listener stops event propagation before it runs, dispatch returns an error and the caller can fail closed. A no-op if the registration is unknown (for example already removed). */
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
    if nil == subscriber {
        exception.Panic(
            exception.NewError("event subscriber may not be nil", nil, nil),
        )
    }

    subscribedEvents := subscriber.SubscribedEvents()
    if nil == subscribedEvents {
        exception.Panic(
            exception.NewError("subscribed events may not be nil", nil, nil),
        )
    }

    subscriberIdentityValue := eventSubscriberIdentity(subscriber)
    if 0 == subscriberIdentityValue.pointer {
        exception.Panic(
            exception.NewError(
                "event subscriber pointer is required to add a subscriber",
                exceptioncontract.Context{
                    "subscriberType": reflect.TypeOf(subscriber).String(),
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

        for index, subscribedEvent := range subscribedEventList {
            if nil == subscribedEvent {
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

            registration := instance.AddListener(
                eventName,
                listener,
                subscribedEvent.Priority(),
            )

            subscriberType := reflect.TypeOf(subscriber).String()

            instance.mutex.Lock()
            instance.subscriberRegistrations[subscriberIdentityValue] = append(
                instance.subscriberRegistrations[subscriberIdentityValue],
                subscriberRegistration{
                    eventName:      eventName,
                    listenerId:     registration.ListenerId,
                    subscriberType: subscriberType,
                },
            )
            instance.mutex.Unlock()
        }
    }
}

func (instance *EventDispatcher) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    if nil == subscriber {
        exception.Panic(
            exception.NewError("event subscriber may not be nil", nil, nil),
        )
    }

    subscriberIdentityValue := eventSubscriberIdentity(subscriber)
    if 0 == subscriberIdentityValue.pointer {
        exception.Panic(
            exception.NewError(
                "event subscriber pointer is required to remove a subscriber",
                exceptioncontract.Context{
                    "subscriberType": reflect.TypeOf(subscriber).String(),
                },
                nil,
            ),
        )
    }

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

            listenerName := "-"
            listenerProgramCounter := reflect.ValueOf(entry.listener).Pointer()
            function := runtime.FuncForPC(listenerProgramCounter)
            if nil != function {
                listenerName = function.Name()
            }

            registeredListenerList = append(
                registeredListenerList,
                eventcontract.RegisteredListener{
                    Priority:     entry.priority,
                    Source:       source,
                    Owner:        owner,
                    ListenerId:   listenerId,
                    ListenerName: listenerName,
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

        exceptionValue, ok := recoveredValue.(*exception.Error)
        if true == ok && nil != exceptionValue {
            exception.Panic(exceptionValue)
        }

        eventName := "-"
        eventType := "-"

        if nil != eventValue {
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
    if nil == eventValue {
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

    dispatchStartedAt := time.Now()

    logger.Debug(
        "event dispatch started",
        loggingcontract.Context{
            "eventName":      eventName,
            "listenersCount": len(listenerList),
        },
    )

    for index, entry := range listenerList {
        listenerStartedAt := time.Now()

        listenerName := "-"
        listenerProgramCounter := reflect.ValueOf(entry.listener).Pointer()
        function := runtime.FuncForPC(listenerProgramCounter)
        if nil != function {
            listenerName = function.Name()
        }

        logger.Debug(
            "event listener started",
            loggingcontract.Context{
                "eventName":        eventName,
                "listenerName":     listenerName,
                "listenerPriority": entry.priority,
            },
        )

        err := instance.callListenerSafely(
            runtimeInstance,
            eventName,
            eventValue,
            entry.listener,
            listenerName,
            entry.priority,
            listenerStartedAt,
            logger,
        )
        if nil != err {
            return eventValue, err
        }

        if true == eventValue.IsPropagationStopped() {
            /* @important a listener that stops propagation before a required listener behind it has run would silently skip that listener (for example the security access-control listener), so the caller would proceed as if it had run — fail closed by returning an error instead, unless the stopping listener is explicitly allowed to skip required listeners. Both marks default off, so an unmarked dispatch behaves exactly as before. */
            if false == entry.maySkipRequiredListeners {
                for _, laterEntry := range listenerList[index+1:] {
                    if true == laterEntry.required {
                        return eventValue, exception.NewError(
                            "event propagation stopped before a required listener ran",
                            exceptioncontract.Context{
                                "eventName":         eventName,
                                "stoppedByListener": listenerName,
                            },
                            nil,
                        )
                    }
                }
            }

            logger.Debug(
                "event dispatch propagation stopped",
                loggingcontract.Context{
                    "eventName": eventName,
                },
            )

            break
        }
    }

    logger.Debug(
        "event dispatch finished",
        loggingcontract.Context{
            "eventName":  eventName,
            "durationMs": time.Since(dispatchStartedAt).Milliseconds(),
        },
    )

    return eventValue, nil
}

func (instance *EventDispatcher) callListenerSafely(
    runtimeInstance runtimecontract.Runtime,
    eventName string,
    eventValue eventcontract.Event,
    listener eventcontract.EventListener,
    listenerName string,
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

        durationMs := time.Since(listenerStartedAt).Milliseconds()

        baseContext := internal.NewEventListenerContext(
            eventName,
            eventType,
            listenerName,
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

        exceptionErr := exception.NewError(
            "event listener panicked",
            exceptionContext,
            nil,
        )
        _ = exception.MarkLogged(exceptionErr)

        logger.Error(
            "event listener panicked",
            exceptionContext,
        )

        returnedErr = exceptionErr
    }()

    listenerErr := listener(runtimeInstance, eventValue)
    if nil == listenerErr {
        return nil
    }

    durationMs := time.Since(listenerStartedAt).Milliseconds()

    exceptionContext := internal.NewEventListenerContext(
        eventName,
        eventType,
        listenerName,
        listenerType,
        priority,
        durationMs,
    )

    exceptionErr := exception.NewError(
        "event listener returned error",
        exceptionContext,
        listenerErr,
    )
    _ = exception.MarkLogged(exceptionErr)

    logger.Error(
        "event listener error",
        exceptionContext,
    )

    return exceptionErr
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

func eventSubscriberIdentity(subscriber eventcontract.EventSubscriber) subscriberIdentity {
    if nil == subscriber {
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
