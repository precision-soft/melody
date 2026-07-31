package event

import (
    "fmt"
    "reflect"
    "runtime"
    "sort"
    "sync"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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
        subscriberRegistrations: make(map[subscriberIdentity][]eventcontract.ListenerRegistration),
    }
}

type EventDispatcherAdapter struct {
    mutex                   sync.RWMutex
    eventDispatcher         eventcontract.EventDispatcher
    listenerRegistrations   map[string][]adapterListenerRegistration
    subscriberRegistrations map[subscriberIdentity][]eventcontract.ListenerRegistration
    nextRegistrationIndex   uint64
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

    /* @important the bookkeeping is scrubbed whether or not the wrapped dispatcher still held the listener. Returning early on false left the adapter's own record of a listener that no longer exists — reported by RegisteredEvents forever and removable by nothing, since every retry takes the same early return */
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

    for subscriberIdentityValue, registrationList := range instance.subscriberRegistrations {
        filtered := make([]eventcontract.ListenerRegistration, 0, len(registrationList))
        for _, entry := range registrationList {
            if registration.EventName == entry.EventName && registration.ListenerId == entry.ListenerId {
                continue
            }

            filtered = append(filtered, entry)
        }

        if 0 == len(filtered) {
            delete(instance.subscriberRegistrations, subscriberIdentityValue)
            continue
        }

        instance.subscriberRegistrations[subscriberIdentityValue] = filtered
    }
    instance.mutex.Unlock()

    return removed
}

func (instance *EventDispatcherAdapter) AddSubscriber(subscriber eventcontract.EventSubscriber) {
    subscriberIdentityValue, subscriberType := requireEventSubscriberIdentity(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.mutex.Lock()
    _, alreadyRegistered := instance.subscriberRegistrations[subscriberIdentityValue]
    instance.mutex.Unlock()

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
        registration := instance.addListenerRegistration(
            planned.eventName,
            planned.listener,
            planned.priority,
            eventcontract.RegisteredListenerSourceSubscriber,
            subscriberType,
        )

        instance.mutex.Lock()
        instance.subscriberRegistrations[subscriberIdentityValue] = append(
            instance.subscriberRegistrations[subscriberIdentityValue],
            registration,
        )
        instance.mutex.Unlock()
    }
}

func (instance *EventDispatcherAdapter) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    subscriberIdentityValue, _ := requireEventSubscriberIdentity(
        subscriber,
        "remove a subscriber",
    )

    instance.mutex.Lock()
    registrationList := instance.subscriberRegistrations[subscriberIdentityValue]
    delete(instance.subscriberRegistrations, subscriberIdentityValue)
    instance.mutex.Unlock()

    removedCount := 0
    for _, registration := range registrationList {
        if true == instance.RemoveListener(registration) {
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

/* MarkListenerRequired forwards to the wrapped dispatcher and records the mark for inspection. A wrapped dispatcher that cannot mark required listeners is refused rather than absorbed: callers probe for RequiredListenerRegistrar precisely to learn whether the guarantee is available, and the adapter satisfies that probe on its own behalf, so swallowing the mark here would answer the probe yes and leave the fail-closed guarantee unarmed. */
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
        /* @important sort a copy: the map-owned slice is shared, and RLock permits concurrent readers that would otherwise race sorting the same backing array */
        registeredSlice := instance.listenerRegistrations[eventName]
        listenerList := make([]adapterListenerRegistration, len(registeredSlice))
        copy(listenerList, registeredSlice)

        sort.SliceStable(
            listenerList,
            func(i int, j int) bool {
                if listenerList[i].priority == listenerList[j].priority {
                    return listenerList[i].registrationIndex < listenerList[j].registrationIndex
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

    wrappedListener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        contractEvent := NewEventFromEvent(eventValue)

        listenerErr := listener(runtimeInstance, contractEvent)

        if true == contractEvent.IsPropagationStopped() {
            eventValue.StopPropagation()
        }

        return listenerErr
    }

    registration := instance.eventDispatcher.AddListener(
        eventName,
        wrappedListener,
        priority,
    )

    /* @important the ordering index is taken after the wrapped dispatcher issued its listener id, not before. Taken first, two concurrent registrations could receive the two counters in opposite orders, and since dispatch breaks ties between equal priorities by listener id while RegisteredEvents breaks them by this index, the inspector would report an execution order the dispatch never uses — permanently. */
    instance.mutex.Lock()
    instance.nextRegistrationIndex++
    registrationIndex := instance.nextRegistrationIndex

    instance.listenerRegistrations[eventName] = append(
        instance.listenerRegistrations[eventName],
        adapterListenerRegistration{
            registration:           registration,
            priority:               priority,
            source:                 source,
            owner:                  owner,
            listenerProgramCounter: listenerProgramCounter,
            registrationIndex:      registrationIndex,
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
    registrationIndex        uint64
    required                 bool
    maySkipRequiredListeners bool
}

var _ eventcontract.EventDispatcher = (*EventDispatcherAdapter)(nil)
var _ eventcontract.EventDispatcherInspector = (*EventDispatcherAdapter)(nil)
var _ eventcontract.RequiredListenerRegistrar = (*EventDispatcherAdapter)(nil)
