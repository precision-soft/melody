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
        subscriberIdByIdentity:  make(map[subscriberIdentity]uint64),
        subscriberIdentityById:  make(map[uint64]subscriberIdentity),
    }
}

type EventDispatcherAdapter struct {
    mutex                   sync.RWMutex
    eventDispatcher         eventcontract.EventDispatcher
    listenerRegistrations   map[string][]adapterListenerRegistration
    subscriberRegistrations map[uint64][]eventcontract.ListenerRegistration
    /* subscriberIdByIdentity files each installation AddSubscriber made under the subscriber's pointer, and subscriberIdentityById answers the pointer of such an installation, so RemoveSubscriber finds it and an emptied installation leaves both. */
    subscriberIdByIdentity map[subscriberIdentity]uint64
    subscriberIdentityById map[uint64]subscriberIdentity

    /* nextSubscriberId issues the identity AddSubscriberWithRegistration answers with, this bookkeeping's own; the wrapped dispatcher issues its own for the same installation. */
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
            instance.forgetSubscriberIdentity(subscriberId)
            continue
        }

        instance.subscriberRegistrations[subscriberId] = filtered
    }
    instance.mutex.Unlock()

    return removed
}

/* AddSubscriber installs every listener the subscriber declares, filed under the subscriber's pointer, refusing a nil or a value subscriber and a second installation of one pointer for the reasons EventDispatcher.AddSubscriber gives. */
func (instance *EventDispatcherAdapter) AddSubscriber(subscriber eventcontract.EventSubscriber) {
    subscriberIdentityValue, subscriberType := requireEventSubscriberIdentity(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.RLock()
    _, alreadyRegistered := instance.subscriberIdByIdentity[subscriberIdentityValue]
    instance.mutex.RUnlock()

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

    instance.installSubscriber(plannedList, subscriberType, &subscriberIdentityValue)
}

/* RemoveSubscriber removes every listener AddSubscriber installed for the subscriber's pointer and answers how many; a pointer AddSubscriber did not install removes nothing and answers zero. */
func (instance *EventDispatcherAdapter) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    subscriberIdentityValue, _ := requireEventSubscriberIdentity(
        subscriber,
        "remove a subscriber",
    )

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.RLock()
    subscriberId, exists := instance.subscriberIdByIdentity[subscriberIdentityValue]
    instance.mutex.RUnlock()

    if false == exists {
        return 0
    }

    return instance.uninstallSubscriber(subscriberId)
}

func (instance *EventDispatcherAdapter) AddSubscriberWithRegistration(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration {
    subscriberType := requireEventSubscriber(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    subscriberId := instance.installSubscriber(plannedList, subscriberType, nil)

    /* the registration answered is this bookkeeping's, the one RemoveSubscriberRegistration takes */
    return eventcontract.SubscriberRegistration{SubscriberId: subscriberId}
}

func (instance *EventDispatcherAdapter) RemoveSubscriberRegistration(registration eventcontract.SubscriberRegistration) int {
    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    return instance.uninstallSubscriber(registration.SubscriberId)
}

/* installSubscriber issues an installation id and installs the planned listeners under it, filing the id under the subscriber's pointer when one is given. The caller holds subscriberMutex. */
func (instance *EventDispatcherAdapter) installSubscriber(
    plannedList []plannedSubscriberRegistration,
    subscriberType string,
    subscriberIdentityValue *subscriberIdentity,
) uint64 {
    instance.mutex.Lock()
    instance.nextSubscriberId++
    subscriberId := instance.nextSubscriberId
    if nil != subscriberIdentityValue {
        instance.subscriberIdByIdentity[*subscriberIdentityValue] = subscriberId
        instance.subscriberIdentityById[subscriberId] = *subscriberIdentityValue
    }
    instance.mutex.Unlock()

    /* the listeners are installed through the wrapped dispatcher one by one, so it is not asked to install the subscriber */
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

    return subscriberId
}

/* uninstallSubscriber removes the listeners of one installation and answers how many. The caller holds subscriberMutex. */
func (instance *EventDispatcherAdapter) uninstallSubscriber(subscriberId uint64) int {
    instance.mutex.Lock()
    registrationList := instance.subscriberRegistrations[subscriberId]
    delete(instance.subscriberRegistrations, subscriberId)
    instance.forgetSubscriberIdentity(subscriberId)
    instance.mutex.Unlock()

    removedCount := 0
    for _, listenerRegistration := range registrationList {
        if true == instance.RemoveListener(listenerRegistration) {
            removedCount++
        }
    }

    return removedCount
}

/* forgetSubscriberIdentity drops the pointer an installation was filed under, so the pointer can be installed again. The caller holds mutex. */
func (instance *EventDispatcherAdapter) forgetSubscriberIdentity(subscriberId uint64) {
    subscriberIdentityValue, exists := instance.subscriberIdentityById[subscriberId]
    if false == exists {
        return
    }

    delete(instance.subscriberIdentityById, subscriberId)
    delete(instance.subscriberIdByIdentity, subscriberIdentityValue)
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

    /* the listener itself is registered, so the dispatcher's records name its function and it receives the dispatched event as from the plain dispatcher; the adapter keeps the inspection metadata beside it */
    registration := instance.eventDispatcher.AddListener(
        eventName,
        listener,
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
var _ eventcontract.SubscriberRegistrar = (*EventDispatcherAdapter)(nil)
var _ eventcontract.EventDispatcherInspector = (*EventDispatcherAdapter)(nil)
var _ eventcontract.RequiredListenerRegistrar = (*EventDispatcherAdapter)(nil)
