package debug

import (
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type EventCommand struct {
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
            envelope.Table = builder.Build()
        } else {
            envelope.Data = output.NewListPayload(
                []eventListItem{},
                0,
                option.Limit,
                option.Offset,
            )
        }

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer, envelope, option)
    }

    registeredEvents := inspector.RegisteredEvents()

    items := make([]eventListItem, 0, len(registeredEvents))

    listenerTotal := 0
    fromSubscriberTotal := 0
    subscriberOwnerTotal := 0

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
                }
            }
        }

        subscriberOwnerCount := len(subscriberOwnerSet)

        fromSubscriberTotal = fromSubscriberTotal + fromSubscriberCount
        subscriberOwnerTotal = subscriberOwnerTotal + subscriberOwnerCount

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

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        builder.AddSummaryLine(
            fmt.Sprintf(
                "EVENTS: %d total | LISTENERS: %d total | FROM SUBSCRIBERS: %d total | SUBSCRIBERS: %d total",
                len(items),
                listenerTotal,
                fromSubscriberTotal,
                subscriberOwnerTotal,
            ),
        )

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
                []string{"event", "priority", "source", "owner", "listener"},
            )

            sortedRegisteredEvents := make([]eventcontract.RegisteredEvent, 0, len(registeredEvents))
            for _, registeredEvent := range registeredEvents {
                sortedRegisteredEvents = append(sortedRegisteredEvents, registeredEvent)
            }

            sort.Slice(
                sortedRegisteredEvents,
                func(leftIndex int, rightIndex int) bool {
                    return sortedRegisteredEvents[leftIndex].EventName < sortedRegisteredEvents[rightIndex].EventName
                },
            )

            for _, registeredEvent := range sortedRegisteredEvents {
                verboseBlock.AddRow(output.TableRowSeparatorToken)

                sortedListeners := make([]eventcontract.RegisteredListener, 0, len(registeredEvent.Listeners))
                for _, listener := range registeredEvent.Listeners {
                    sortedListeners = append(sortedListeners, listener)
                }

                sort.Slice(
                    sortedListeners,
                    func(leftIndex int, rightIndex int) bool {
                        leftListener := sortedListeners[leftIndex]
                        rightListener := sortedListeners[rightIndex]

                        if leftListener.Priority != rightListener.Priority {
                            return leftListener.Priority > rightListener.Priority
                        }

                        if leftListener.Source != rightListener.Source {
                            return leftListener.Source < rightListener.Source
                        }

                        if leftListener.Owner != rightListener.Owner {
                            return leftListener.Owner < rightListener.Owner
                        }

                        if leftListener.ListenerId != rightListener.ListenerId {
                            return leftListener.ListenerId < rightListener.ListenerId
                        }

                        return leftListener.ListenerName < rightListener.ListenerName
                    },
                )

                for index, listener := range sortedListeners {
                    eventCell := ""
                    if 0 == index {
                        eventCell = registeredEvent.EventName
                    }

                    verboseBlock.AddRow(
                        eventCell,
                        fmt.Sprintf("%d", listener.Priority),
                        listener.Source,
                        listener.Owner,
                        listener.ListenerName,
                    )
                }

                verboseBlock.AddRow(output.TableRowSeparatorToken)
            }
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = output.NewListPayload(
            items,
            len(items),
            option.Limit,
            option.Offset,
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
}

type eventListItem struct {
    EventName            string `json:"eventName"`
    ListenerCount        int    `json:"listenerCount"`
    FromSubscriberCount  int    `json:"fromSubscriberCount"`
    SubscriberOwnerCount int    `json:"subscriberOwnerCount"`
    Priorities           string `json:"priorities"`
}

var _ clicontract.Command = (*EventCommand)(nil)
