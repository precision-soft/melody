package event

import (
    "fmt"
    "reflect"
    "runtime"
    "sort"
    "sync"

    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewEventDispatcherAdapter(
    eventDispatcher eventcontract.EventDispatcher,
    clock clockcontract.Clock,
) *EventDispatcherAdapter {
    if nil == eventDispatcher {
        exception.Panic(
            exception.NewError("event dispatcher may not be nil", nil, nil),
        )
    }

    if nil == clock {
        exception.Panic(
            exception.NewError("clock may not be nil", nil, nil),
        )
    }

    return &EventDispatcherAdapter{
        eventDispatcher:         eventDispatcher,
        clock:                   clock,
        listenerRegistrations:   make(map[string][]adapterListenerRegistration),
        subscriberRegistrations: make(map[uintptr][]eventcontract.ListenerRegistration),
    }
}

type EventDispatcherAdapter struct {
    mutex                   sync.RWMutex
    eventDispatcher         eventcontract.EventDispatcher
    clock                   clockcontract.Clock
    listenerRegistrations   map[string][]adapterListenerRegistration
    subscriberRegistrations map[uintptr][]eventcontract.ListenerRegistration
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
    if false == removed {
        return false
    }

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

    for subscriberPointer, registrationList := range instance.subscriberRegistrations {
        filtered := make([]eventcontract.ListenerRegistration, 0, len(registrationList))
        for _, entry := range registrationList {
            if registration.EventName == entry.EventName && registration.ListenerId == entry.ListenerId {
                continue
            }

            filtered = append(filtered, entry)
        }

        if 0 == len(filtered) {
            delete(instance.subscriberRegistrations, subscriberPointer)
            continue
        }

        instance.subscriberRegistrations[subscriberPointer] = filtered
    }
    instance.mutex.Unlock()

    return true
}

func (instance *EventDispatcherAdapter) AddSubscriber(subscriber eventcontract.EventSubscriber) {
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

    subscriberPointer := eventSubscriberPointer(subscriber)
    if 0 == subscriberPointer {
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

    subscriberType := reflect.TypeOf(subscriber).String()

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

            registration := instance.addListenerRegistration(
                eventName,
                listener,
                subscribedEvent.Priority(),
                eventcontract.RegisteredListenerSourceSubscriber,
                subscriberType,
            )

            instance.mutex.Lock()
            instance.subscriberRegistrations[subscriberPointer] = append(
                instance.subscriberRegistrations[subscriberPointer],
                registration,
            )
            instance.mutex.Unlock()
        }
    }
}

func (instance *EventDispatcherAdapter) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    if nil == subscriber {
        exception.Panic(
            exception.NewError("event subscriber may not be nil", nil, nil),
        )
    }

    subscriberPointer := eventSubscriberPointer(subscriber)
    if 0 == subscriberPointer {
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
    registrationList := instance.subscriberRegistrations[subscriberPointer]
    delete(instance.subscriberRegistrations, subscriberPointer)
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
        listenerList := instance.listenerRegistrations[eventName]

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
                    Priority:     entry.priority,
                    Source:       entry.source,
                    Owner:        entry.owner,
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

    instance.mutex.Lock()
    instance.nextRegistrationIndex++
    registrationIndex := instance.nextRegistrationIndex
    instance.mutex.Unlock()

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
            registrationIndex:      registrationIndex,
        },
    )
    instance.mutex.Unlock()

    return registration
}

type adapterListenerRegistration struct {
    registration           eventcontract.ListenerRegistration
    priority               int
    source                 string
    owner                  string
    listenerProgramCounter uintptr
    registrationIndex      uint64
}

var _ eventcontract.EventDispatcher = (*EventDispatcherAdapter)(nil)
var _ eventcontract.EventDispatcherInspector = (*EventDispatcherAdapter)(nil)
