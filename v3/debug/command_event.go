package debug

import (
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* DeferredListener declares a listener the serving process wires and this process does not, so the listing names it instead of reporting an absence. */
type DeferredListener struct {
    EventName    string `json:"eventName"`
    Priority     int    `json:"priority"`
    ListenerName string `json:"listenerName"`
    Note         string `json:"note"`
}

type DeferredListenerProvider func() []DeferredListener

/* NewEventCommand builds the command with its deferred-listener declaration; the zero value declares nothing. */
func NewEventCommand(deferredListenerProvider DeferredListenerProvider) *EventCommand {
    return &EventCommand{
        deferredListenerProvider: deferredListenerProvider,
    }
}

type EventCommand struct {
    deferredListenerProvider DeferredListenerProvider
}

func (instance *EventCommand) Name() string {
    return "debug:events"
}

func (instance *EventCommand) Description() string {
    return "List registered events and listeners"
}

func (instance *EventCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
}

func (instance *EventCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    startedAt := time.Now()

    option := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Arguments(),
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    dispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())

    inspector, ok := dispatcher.(eventcontract.EventDispatcherInspector)
    if false == ok {
        envelope.AddWarning(
            "debug.notSupported",
            "event dispatcher does not support inspection",
            map[string]any{
                "dispatcherType": fmt.Sprintf("%T", dispatcher),
            },
        )

        if output.FormatTable == option.Format {
            builder := output.NewTableBuilder()
            builder.AddSummaryLine("EVENTS: 0 total")
            renderDeferredListenerBlock(builder, instance.deferredListeners())
            envelope.Table = builder.Build()
        } else {
            /* the declaration is the command's own, so it is rendered even when the dispatcher cannot be inspected */
            envelope.Data = eventListPayload{
                ListPayload: output.NewListPayload(
                    []eventListItem{},
                    0,
                    option.Limit,
                    option.Offset,
                ),
                ServingProcessListeners: instance.deferredListeners(),
            }
        }

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer(), envelope, option)
    }

    registeredEvents := inspector.RegisteredEvents()

    items := make([]eventListItem, 0, len(registeredEvents))

    listenerTotal := 0
    fromSubscriberTotal := 0

    /* the total counts distinct subscribers across the dispatcher, not per event */
    subscriberOwnerGlobalSet := make(map[string]struct{})

    for _, registeredEvent := range registeredEvents {
        listenerTotal = listenerTotal + len(registeredEvent.Listeners)

        priorities := make([]string, 0, len(registeredEvent.Listeners))

        fromSubscriberCount := 0
        subscriberOwnerSet := make(map[string]struct{})

        for _, listener := range registeredEvent.Listeners {
            priorities = append(priorities, fmt.Sprintf("%d", listener.Priority))

            if eventcontract.RegisteredListenerSourceSubscriber == listener.Source {
                fromSubscriberCount = fromSubscriberCount + 1

                if "-" != listener.Owner && "" != listener.Owner {
                    subscriberOwnerSet[listener.Owner] = struct{}{}
                    subscriberOwnerGlobalSet[listener.Owner] = struct{}{}
                }
            }
        }

        subscriberOwnerCount := len(subscriberOwnerSet)

        fromSubscriberTotal = fromSubscriberTotal + fromSubscriberCount

        items = append(
            items,
            eventListItem{
                EventName:            registeredEvent.EventName,
                ListenerCount:        len(registeredEvent.Listeners),
                FromSubscriberCount:  fromSubscriberCount,
                SubscriberOwnerCount: subscriberOwnerCount,
                Priorities:           strings.Join(priorities, ","),
            },
        )
    }

    sort.Slice(
        items,
        func(leftIndex int, rightIndex int) bool {
            return items[leftIndex].EventName < items[rightIndex].EventName
        },
    )

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "EVENTS: %d total | LISTENERS: %d total | FROM SUBSCRIBERS: %d total | SUBSCRIBERS: %d total",
            total,
            listenerTotal,
            fromSubscriberTotal,
            len(subscriberOwnerGlobalSet),
        )

        if len(items) != total {
            summary = fmt.Sprintf(
                "%s | %d shown",
                summary,
                len(items),
            )
        }

        builder.AddSummaryLine(summary)

        block := builder.AddBlock(
            "EVENTS",
            []string{"event", "listeners", "from subscribers", "subscribers", "priorities"},
        )

        for _, item := range items {
            block.AddRow(
                item.EventName,
                fmt.Sprintf("%d", item.ListenerCount),
                fmt.Sprintf("%d", item.FromSubscriberCount),
                fmt.Sprintf("%d", item.SubscriberOwnerCount),
                item.Priorities,
            )
        }

        if true == option.Verbose {
            verboseBlock := builder.AddBlock(
                "LISTENERS",
                []string{"event", "order", "priority", "required", "source", "owner", "listener"},
            )

            for _, registeredEvent := range selectSortedRegisteredEvents(registeredEvents, items, option.Order) {
                verboseBlock.AddRow(output.TableRowSeparatorToken)

                /* the rows keep the dispatcher's slice order, which is the dispatch order */
                for index, listener := range registeredEvent.Listeners {
                    eventCell := ""
                    if 0 == index {
                        eventCell = registeredEvent.EventName
                    }

                    verboseBlock.AddRow(
                        eventCell,
                        fmt.Sprintf("%d", index+1),
                        fmt.Sprintf("%d", listener.Priority),
                        renderRequiredListenerMark(listener),
                        listener.Source,
                        listener.Owner,
                        listener.ListenerName,
                    )
                }

                verboseBlock.AddRow(output.TableRowSeparatorToken)
            }
        }

        renderDeferredListenerBlock(builder, instance.deferredListeners())

        envelope.Table = builder.Build()
    } else {
        eventsPayload := output.NewListPayload(
            items,
            total,
            option.Limit,
            option.Offset,
        )

        if true == option.Verbose {
            /* the listener detail, with its required and may-skip marks, is carried in the json document too */
            envelope.Data = eventListVerbosePayload{
                Events:                  eventsPayload,
                Listeners:               collectListenerListItems(registeredEvents, items, option.Order),
                ServingProcessListeners: instance.deferredListeners(),
            }
        } else {
            envelope.Data = eventListPayload{
                ListPayload:             eventsPayload,
                ServingProcessListeners: instance.deferredListeners(),
            }
        }
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer(), envelope, option)
}

/* selectSortedRegisteredEvents windows the listener detail like the event listing and orders it by event name in the requested direction. The direction applies to the events; inside one event the rows keep the dispatch order. */
func selectSortedRegisteredEvents(
    registeredEvents []eventcontract.RegisteredEvent,
    items []eventListItem,
    order output.SortOrder,
) []eventcontract.RegisteredEvent {
    selectedEventNames := make(map[string]struct{}, len(items))
    for _, item := range items {
        selectedEventNames[item.EventName] = struct{}{}
    }

    sortedRegisteredEvents := make([]eventcontract.RegisteredEvent, 0, len(selectedEventNames))
    for _, registeredEvent := range registeredEvents {
        _, isSelected := selectedEventNames[registeredEvent.EventName]
        if false == isSelected {
            continue
        }

        sortedRegisteredEvents = append(sortedRegisteredEvents, registeredEvent)
    }

    sort.Slice(
        sortedRegisteredEvents,
        func(leftIndex int, rightIndex int) bool {
            return sortedRegisteredEvents[leftIndex].EventName < sortedRegisteredEvents[rightIndex].EventName
        },
    )

    output.ApplySortOrder(sortedRegisteredEvents, order)

    return sortedRegisteredEvents
}

/* collectListenerListItems flattens the windowed listener detail for the json document, in dispatch order. */
func collectListenerListItems(
    registeredEvents []eventcontract.RegisteredEvent,
    items []eventListItem,
    order output.SortOrder,
) []eventListenerListItem {
    listenerItems := make([]eventListenerListItem, 0, len(items))

    for _, registeredEvent := range selectSortedRegisteredEvents(registeredEvents, items, order) {
        for index, listener := range registeredEvent.Listeners {
            listenerItems = append(
                listenerItems,
                eventListenerListItem{
                    EventName:                registeredEvent.EventName,
                    Order:                    index + 1,
                    Priority:                 listener.Priority,
                    Required:                 listener.Required,
                    MaySkipRequiredListeners: listener.MaySkipRequiredListeners,
                    Source:                   listener.Source,
                    Owner:                    listener.Owner,
                    ListenerName:             listener.ListenerName,
                },
            )
        }
    }

    return listenerItems
}

/* renderRequiredListenerMark answers whether a listener is protected from being skipped, or may skip the ones that are. */
func renderRequiredListenerMark(listener eventcontract.RegisteredListener) string {
    if true == listener.Required {
        if true == listener.MaySkipRequiredListeners {
            return "yes (may skip)"
        }

        return "yes"
    }

    if true == listener.MaySkipRequiredListeners {
        return "may skip"
    }

    return "no"
}

type eventListItem struct {
    EventName            string `json:"eventName"`
    ListenerCount        int    `json:"listenerCount"`
    FromSubscriberCount  int    `json:"fromSubscriberCount"`
    SubscriberOwnerCount int    `json:"subscriberOwnerCount"`
    Priorities           string `json:"priorities"`
}

type eventListenerListItem struct {
    EventName                string `json:"eventName"`
    Order                    int    `json:"order"`
    Priority                 int    `json:"priority"`
    Required                 bool   `json:"required"`
    MaySkipRequiredListeners bool   `json:"maySkipRequiredListeners"`
    Source                   string `json:"source"`
    Owner                    string `json:"owner"`
    ListenerName             string `json:"listenerName"`
}

type eventListVerbosePayload struct {
    Events                  output.ListPayload[eventListItem] `json:"events"`
    Listeners               []eventListenerListItem           `json:"listeners"`
    ServingProcessListeners []DeferredListener                `json:"servingProcessListeners,omitempty"`
}

/* eventListPayload is the default-verbosity json document: the listing embedded, so data.items keeps its place, with the deferred-listener declaration beside it at every verbosity. */
type eventListPayload struct {
    output.ListPayload[eventListItem]
    ServingProcessListeners []DeferredListener `json:"servingProcessListeners,omitempty"`
}

/* renderDeferredListenerBlock is called from both branches, since the declaration does not depend on inspecting the dispatcher. */
func renderDeferredListenerBlock(builder *output.TableBuilder, deferredListeners []DeferredListener) {
    if 0 == len(deferredListeners) {
        return
    }

    deferredBlock := builder.AddBlock(
        "SERVING-PROCESS LISTENERS",
        []string{"event", "priority", "listener", "note"},
    )

    for _, deferredListener := range deferredListeners {
        deferredBlock.AddRow(
            deferredListener.EventName,
            fmt.Sprintf("%d", deferredListener.Priority),
            deferredListener.ListenerName,
            deferredListener.Note,
        )
    }
}

func (instance *EventCommand) deferredListeners() []DeferredListener {
    if nil == instance.deferredListenerProvider {
        return nil
    }

    return instance.deferredListenerProvider()
}

var _ clicontract.Command = (*EventCommand)(nil)
