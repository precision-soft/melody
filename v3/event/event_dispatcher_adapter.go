package event

import (
    "fmt"
    "reflect"
    "runtime"
    "sort"
    "sync"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewEventDispatcherAdapter(
    eventDispatcher eventcontract.EventDispatcher,
) *EventDispatcherAdapter {
    if true == internal.IsNilInterface(eventDispatcher) {
        exception.Panic(
            exception.NewError("event dispatcher may not be nil", nil, nil),
        )
    }

    return &EventDispatcherAdapter{
        eventDispatcher:         eventDispatcher,
        listenerRegistrations:   make(map[string][]adapterListenerRegistration),
        subscriberRegistrations: make(map[uint64][]eventcontract.ListenerRegistration),
    }
}

type EventDispatcherAdapter struct {
    mutex                   sync.RWMutex
    eventDispatcher         eventcontract.EventDispatcher
    listenerRegistrations   map[string][]adapterListenerRegistration
    subscriberRegistrations map[uint64][]eventcontract.ListenerRegistration

    /* nextSubscriberId issues the identity AddSubscriber answers with, this bookkeeping's own; the wrapped dispatcher issues its own for the same installation. */
    nextSubscriberId uint64
    /* subscriberMutex serializes whole subscriber installations and removals against each other; it is always taken before mutex and never inside it, so a removal never interleaves with the installation it undoes. */
    subscriberMutex sync.Mutex
}

func (instance *EventDispatcherAdapter) AddListener(eventName string, listener eventcontract.EventListener, priority int) eventcontract.ListenerRegistration {
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

    return instance.addListenerRegistration(
        eventName,
        listener,
        priority,
        eventcontract.RegisteredListenerSourceListener,
        "-",
    )
}

func (instance *EventDispatcherAdapter) RemoveListener(registration eventcontract.ListenerRegistration) bool {
    removed := instance.eventDispatcher.RemoveListener(registration)

    /* the bookkeeping is scrubbed whether or not the wrapped dispatcher still held the listener, so no record outlives it */
    instance.mutex.Lock()
    listenerList, exists := instance.listenerRegistrations[registration.EventName]
    if true == exists {
        filtered := make([]adapterListenerRegistration, 0, len(listenerList))
        for _, entry := range listenerList {
            if entry.registration.ListenerId == registration.ListenerId {
                continue
            }

            filtered = append(filtered, entry)
        }

        if 0 == len(filtered) {
            delete(instance.listenerRegistrations, registration.EventName)
        } else {
            instance.listenerRegistrations[registration.EventName] = filtered
        }
    }

    for subscriberId, registrationList := range instance.subscriberRegistrations {
        filtered := make([]eventcontract.ListenerRegistration, 0, len(registrationList))
        for _, entry := range registrationList {
            if registration.EventName == entry.EventName && registration.ListenerId == entry.ListenerId {
                continue
            }

            filtered = append(filtered, entry)
        }

        if 0 == len(filtered) {
            delete(instance.subscriberRegistrations, subscriberId)
            continue
        }

        instance.subscriberRegistrations[subscriberId] = filtered
    }
    instance.mutex.Unlock()

    return removed
}

func (instance *EventDispatcherAdapter) AddSubscriber(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration {
    subscriberType := requireEventSubscriber(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.Lock()
    instance.nextSubscriberId++
    subscriberId := instance.nextSubscriberId
    instance.mutex.Unlock()

    for _, planned := range plannedList {
        registration := instance.addListenerRegistration(
            planned.eventName,
            planned.listener,
            planned.priority,
            eventcontract.RegisteredListenerSourceSubscriber,
            subscriberType,
        )

        instance.mutex.Lock()
        instance.subscriberRegistrations[subscriberId] = append(
            instance.subscriberRegistrations[subscriberId],
            registration,
        )
        instance.mutex.Unlock()
    }

    /* the listeners were installed through the wrapped dispatcher one by one above, so it is not asked to install the subscriber; the registration answered is this bookkeeping's, the one RemoveSubscriber takes */
    return eventcontract.SubscriberRegistration{SubscriberId: subscriberId}
}

func (instance *EventDispatcherAdapter) RemoveSubscriber(registration eventcontract.SubscriberRegistration) int {
    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.Lock()
    registrationList := instance.subscriberRegistrations[registration.SubscriberId]
    delete(instance.subscriberRegistrations, registration.SubscriberId)
    instance.mutex.Unlock()

    removedCount := 0
    for _, listenerRegistration := range registrationList {
        if true == instance.RemoveListener(listenerRegistration) {
            removedCount++
        }
    }

    return removedCount
}

func (instance *EventDispatcherAdapter) Dispatch(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    return instance.eventDispatcher.Dispatch(runtimeInstance, eventValue)
}

func (instance *EventDispatcherAdapter) DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (eventcontract.Event, error) {
    return instance.eventDispatcher.DispatchName(runtimeInstance, eventName, payload)
}

/* MarkListenerRequired forwards to the wrapped dispatcher and records the mark for inspection. A wrapped dispatcher that cannot mark required listeners is refused, since the adapter answers the RequiredListenerRegistrar probe itself and an absorbed mark would leave the guarantee unarmed. */
func (instance *EventDispatcherAdapter) MarkListenerRequired(registration eventcontract.ListenerRegistration) {
    instance.requireRegistrar().MarkListenerRequired(registration)

    instance.markListenerRegistration(
        registration,
        func(entry *adapterListenerRegistration) {
            entry.required = true
        },
    )
}

/* MarkListenerMaySkipRequiredListeners forwards to the wrapped dispatcher and records the mark for inspection, refusing a wrapped dispatcher that cannot mark required listeners for the reason MarkListenerRequired gives. */
func (instance *EventDispatcherAdapter) MarkListenerMaySkipRequiredListeners(registration eventcontract.ListenerRegistration) {
    instance.requireRegistrar().MarkListenerMaySkipRequiredListeners(registration)

    instance.markListenerRegistration(
        registration,
        func(entry *adapterListenerRegistration) {
            entry.maySkipRequiredListeners = true
        },
    )
}

func (instance *EventDispatcherAdapter) requireRegistrar() eventcontract.RequiredListenerRegistrar {
    registrar, ok := instance.eventDispatcher.(eventcontract.RequiredListenerRegistrar)
    if false == ok {
        exception.Panic(
            exception.NewError(
                "the wrapped event dispatcher cannot mark required listeners",
                exceptioncontract.Context{
                    "eventDispatcherType": internal.StringifyType(instance.eventDispatcher),
                },
                nil,
            ),
        )
    }

    return registrar
}

func (instance *EventDispatcherAdapter) markListenerRegistration(
    registration eventcontract.ListenerRegistration,
    apply func(entry *adapterListenerRegistration),
) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    entries := instance.listenerRegistrations[registration.EventName]
    for index := range entries {
        if entries[index].registration.ListenerId == registration.ListenerId {
            apply(&entries[index])

            return
        }
    }
}

/* RegisteredEvents reports a point-in-time view, which a concurrent registration or removal is observed mid-step in; dispatch never depends on it. It covers what was registered through the adapter: a listener added directly on the wrapped dispatcher is live but absent here. */
func (instance *EventDispatcherAdapter) RegisteredEvents() []eventcontract.RegisteredEvent {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    eventNameList := make([]string, 0, len(instance.listenerRegistrations))
    for eventName := range instance.listenerRegistrations {
        eventNameList = append(eventNameList, eventName)
    }

    sort.Strings(eventNameList)

    registeredEvents := make([]eventcontract.RegisteredEvent, 0, len(eventNameList))

    for _, eventName := range eventNameList {
        /* a copy is sorted, since the map-owned slice is shared by concurrent readers under RLock */
        registeredSlice := instance.listenerRegistrations[eventName]
        listenerList := make([]adapterListenerRegistration, len(registeredSlice))
        copy(listenerList, registeredSlice)

        /* equal priorities break the tie by the wrapped dispatcher's listener id, as dispatch does; an adapter-side counter is issued under another lock and could report an order dispatch never uses */
        sort.SliceStable(
            listenerList,
            func(i int, j int) bool {
                if listenerList[i].priority == listenerList[j].priority {
                    return listenerList[i].registration.ListenerId < listenerList[j].registration.ListenerId
                }

                return listenerList[i].priority > listenerList[j].priority
            },
        )

        registeredListenerList := make([]eventcontract.RegisteredListener, 0, len(listenerList))

        for _, entry := range listenerList {
            listenerId := fmt.Sprintf("%d", entry.registration.ListenerId)

            listenerName := "-"
            function := runtime.FuncForPC(entry.listenerProgramCounter)
            if nil != function {
                listenerName = function.Name()
            }

            registeredListenerList = append(
                registeredListenerList,
                eventcontract.RegisteredListener{
                    Priority:                 entry.priority,
                    Source:                   entry.source,
                    Owner:                    entry.owner,
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

func (instance *EventDispatcherAdapter) addListenerRegistration(
    eventName string,
    listener eventcontract.EventListener,
    priority int,
    source string,
    owner string,
) eventcontract.ListenerRegistration {
    listenerProgramCounter := reflect.ValueOf(listener).Pointer()

    /* the listener receives the dispatched event itself, as from the plain dispatcher, so a custom event type stays type-assertable and a field a listener writes reaches the caller; the wrapper only carries the inspection metadata */
    wrappedListener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return listener(runtimeInstance, eventValue)
    }

    registration := instance.eventDispatcher.AddListener(
        eventName,
        wrappedListener,
        priority,
    )

    instance.mutex.Lock()
    instance.listenerRegistrations[eventName] = append(
        instance.listenerRegistrations[eventName],
        adapterListenerRegistration{
            registration:           registration,
            priority:               priority,
            source:                 source,
            owner:                  owner,
            listenerProgramCounter: listenerProgramCounter,
        },
    )
    instance.mutex.Unlock()

    return registration
}

type adapterListenerRegistration struct {
    registration             eventcontract.ListenerRegistration
    priority                 int
    source                   string
    owner                    string
    listenerProgramCounter   uintptr
    required                 bool
    maySkipRequiredListeners bool
}

var _ eventcontract.EventDispatcher = (*EventDispatcherAdapter)(nil)
var _ eventcontract.EventDispatcherInspector = (*EventDispatcherAdapter)(nil)
var _ eventcontract.RequiredListenerRegistrar = (*EventDispatcherAdapter)(nil)
