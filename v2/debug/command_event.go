package debug

import (
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* DeferredListener declares a listener the SERVING process wires and this process does not: the composition root knows which registrations are gated on the process shape, and the command renders them beside the dispatcher's own so an operator asking "is access control wired?" is not answered with an absence that means "not in this process". */
type DeferredListener struct {
    EventName    string `json:"eventName"`
    Priority     int    `json:"priority"`
    ListenerName string `json:"listenerName"`
    Note         string `json:"note"`
}

type DeferredListenerProvider func() []DeferredListener

/* NewEventCommand builds the command with the declaration channel; the zero-value command stays valid and simply declares nothing */
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
    commandContext *clicontract.CommandContext,
) error {
    startedAt := time.Now()

    option := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Args().Slice(),
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
            /* the declaration of what a serving process wires travels on this branch too. It does not come from the dispatcher — the command holds it — so a dispatcher that cannot be inspected costs the listing and nothing else, and "is access control wired?" keeps being answered by a declaration rather than by an absence that means "not in this process". */
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

        return output.Render(commandContext.Writer, envelope, option)
    }

    registeredEvents := inspector.RegisteredEvents()

    items := make([]eventListItem, 0, len(registeredEvents))

    listenerTotal := 0
    fromSubscriberTotal := 0

    /* the summary total counts distinct subscribers across the whole dispatcher: summing the per-event distinct counts reported one subscriber listening on three events as three subscribers, a number nothing in the process matches */
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

                /* the rows keep the dispatcher's own slice order — which IS the dispatch order, held sorted at insertion — so the verbose detail and the priorities column above it read the same run; re-sorting here once inverted listeners that share a priority, because the id was compared as text */
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
            /* the listener detail — including the required and may-skip marks that say whether the fail-closed guarantee is armed — existed only in the table format, so a machine consumer of the json document could never learn it at any verbosity */
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

    return output.Render(commandContext.Writer, envelope, option)
}

/* selectSortedRegisteredEvents keeps the listener detail on the same window as the event listing, otherwise --limit lists three events and every listener in the application, and orders it by event name the way the listing is ordered — the requested direction included, since --order=desc used to reverse the listing and leave the detail ascending, so the two halves of one document contradicted each other. The direction is applied to the EVENTS, never to the flattened listeners: inside one event the rows carry the dispatcher's own slice order, which IS the dispatch order, and reversing that would make the order column lie. */
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

/* collectListenerListItems flattens the windowed listener detail for the json document, in the order the table prints it — the dispatcher's own dispatch order, carried by the order field. */
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

/* renderRequiredListenerMark answers what a listener's required-listener marks mean for the dispatch: whether it is protected from being skipped, or is allowed to skip the ones that are. Without the column an unarmed fail-closed guarantee looks exactly like an armed one. */
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

/* eventListPayload is the default-verbosity json document: the listing embedded, so data.items stays exactly where every consumer keyed it, with the declaration beside it. The declaration cannot wait for --verbose the way the listener detail does — it exists so that "is access control wired?" is not answered with an absence meaning "not in this process", and the table has printed it at every verbosity since the verdict that introduced it, so a consumer auditing the wiring through the json document read a list the two security listeners were simply missing from. Reparenting the payload under --verbose is documented design and stays; the declaration is not part of it. */
type eventListPayload struct {
    output.ListPayload[eventListItem]
    ServingProcessListeners []DeferredListener `json:"servingProcessListeners,omitempty"`
}

/* renderDeferredListenerBlock is called from both branches, the inspectable dispatcher's and the one that only warns: the declaration is the command's own and does not depend on being able to list anything. */
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
