package debug

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type eventCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            EventName     string `json:"eventName"`
            ListenerCount int    `json:"listenerCount"`
        } `json:"items"`
        Total  int `json:"total"`
        Limit  int `json:"limit"`
        Offset int `json:"offset"`
    } `json:"data"`
}

func newEventTestRuntime(eventCount int) *testRuntime {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    for index := 0; index < eventCount; index++ {
        dispatcher.AddListener(
            fmt.Sprintf("event.%02d", index),
            func(
                runtimeInstance runtimecontract.Runtime,
                eventInstance eventcontract.Event,
            ) error {
                return nil
            },
            0,
        )
    }

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )

    return newTestRuntime(serviceContainer)
}

func decodeEventCommandEnvelope(t *testing.T, rendered string) eventCommandTestEnvelope {
    t.Helper()

    envelope := eventCommandTestEnvelope{}

    decodeErr := json.Unmarshal([]byte(rendered), &envelope)
    if nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    return envelope
}

func TestEventCommand_AppliesLimitAndOffsetToTheRenderedItems(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        runtimeInstance,
        []string{"--format=json", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 2 != len(envelope.Data.Items) {
        t.Fatalf("expected 2 items for --limit=2, got %d", len(envelope.Data.Items))
    }
    if "event.04" != envelope.Data.Items[0].EventName {
        t.Fatalf("expected the window to start at --offset=4, got %q", envelope.Data.Items[0].EventName)
    }
    if "event.05" != envelope.Data.Items[1].EventName {
        t.Fatalf("expected the second windowed item, got %q", envelope.Data.Items[1].EventName)
    }
}

func TestEventCommand_ReturnsNoItemsForAnOffsetPastTheTotal(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        runtimeInstance,
        []string{"--format=json", "--offset=25"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 0 != len(envelope.Data.Items) {
        t.Fatalf("expected no items past the total, got %d", len(envelope.Data.Items))
    }
}

func TestEventCommand_ReturnsEveryItemWithoutALimit(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        runtimeInstance,
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    if 10 != len(envelope.Data.Items) {
        t.Fatalf("expected 10 items without a limit, got %d", len(envelope.Data.Items))
    }
}

func TestEventCommand_WalksEveryItemExactlyOnceWhenPaging(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        rendered, runErr := runDebugCommand(
            &EventCommand{},
            runtimeInstance,
            []string{"--format=json", "--limit=3", fmt.Sprintf("--offset=%d", offset)},
        )
        if nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        envelope := decodeEventCommandEnvelope(t, rendered)

        if 0 == len(envelope.Data.Items) {
            break
        }

        for _, item := range envelope.Data.Items {
            seen[item.EventName] = seen[item.EventName] + 1
        }

        offset = offset + 3
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct events, got %d", len(seen))
    }

    for eventName, count := range seen {
        if 1 != count {
            t.Fatalf("event %q was returned %d times while paging", eventName, count)
        }
    }
}

func TestEventCommand_AppliesTheSameWindowInTheTableFormat(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        runtimeInstance,
        []string{"--format=table", "--table-width=400", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "EVENTS")

    if 2 != len(row) {
        t.Fatalf("expected 2 table rows for --limit=2, got %d, rendered %q", len(row), rendered)
    }
    if "event.04" != row[0][0] {
        t.Fatalf("expected the table window to start at --offset=4, got %q", row[0][0])
    }
    if "event.05" != row[1][0] {
        t.Fatalf("expected the second windowed table row, got %q", row[1][0])
    }
}

func TestEventCommand_LimitsTheVerboseListenerBlockToTheWindowedEvents(t *testing.T) {
    runtimeInstance := newEventTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        runtimeInstance,
        []string{"--format=table", "--table-width=400", "--limit=1", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "LISTENERS")

    if 0 == len(row) {
        t.Fatalf("expected the verbose listener block to be rendered, rendered %q", rendered)
    }

    listedEventName := map[string]struct{}{}
    for _, cell := range row {
        if "" == cell[0] {
            continue
        }

        listedEventName[cell[0]] = struct{}{}
    }

    if 1 != len(listedEventName) {
        t.Fatalf("expected the listener block to cover 1 event for --limit=1, got %v", listedEventName)
    }

    _, isWindowedEventListed := listedEventName["event.00"]
    if false == isWindowedEventListed {
        t.Fatalf("expected the listener block to cover the windowed event, got %v", listedEventName)
    }
}

func eventCommandNameList(t *testing.T, arguments []string) []string {
    t.Helper()

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTestRuntime(10),
        arguments,
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    name := make([]string, 0, len(envelope.Data.Items))
    for _, item := range envelope.Data.Items {
        name = append(name, item.EventName)
    }

    return name
}

func assertEventNameList(t *testing.T, expected []string, actual []string) {
    t.Helper()

    if len(expected) != len(actual) {
        t.Fatalf("expected %v, got %v", expected, actual)
    }

    for position, value := range expected {
        if value != actual[position] {
            t.Fatalf("expected %v, got %v", expected, actual)
        }
    }
}

func TestEventCommand_KeepsTheAscendingOrderByDefault(t *testing.T) {
    assertEventNameList(
        t,
        []string{"event.00", "event.01", "event.02"},
        eventCommandNameList(t, []string{"--format=json", "--limit=3"}),
    )
}

func TestEventCommand_ReversesTheItemsForADescendingOrder(t *testing.T) {
    ascending := eventCommandNameList(t, []string{"--format=json", "--order=asc"})
    descending := eventCommandNameList(t, []string{"--format=json", "--order=desc"})

    if len(ascending) != len(descending) {
        t.Fatalf("expected %d items, got %d", len(ascending), len(descending))
    }

    for position, value := range ascending {
        if value != descending[len(descending)-1-position] {
            t.Fatalf("expected the descending order to be the reverse of %v, got %v", ascending, descending)
        }
    }
}

func TestEventCommand_AppliesTheDescendingOrderBeforeTheWindow(t *testing.T) {
    assertEventNameList(
        t,
        []string{"event.09", "event.08", "event.07"},
        eventCommandNameList(t, []string{"--format=json", "--order=desc", "--limit=3"}),
    )
}

func TestEventCommand_WalksEveryItemExactlyOnceWhenPagingDescending(t *testing.T) {
    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        name := eventCommandNameList(
            t,
            []string{"--format=json", "--order=desc", "--limit=4", fmt.Sprintf("--offset=%d", offset)},
        )

        if 0 == len(name) {
            break
        }

        for _, value := range name {
            seen[value] = seen[value] + 1
        }

        offset = offset + 4
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct events, got %d", len(seen))
    }

    for value, count := range seen {
        if 1 != count {
            t.Fatalf("event %q was returned %d times while paging descending", value, count)
        }
    }
}

func TestEventCommand_ReversesTheVerboseListenerBlockWithTheEvents(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTestRuntime(10),
        []string{"--format=table", "--table-width=400", "--order=desc", "--limit=1", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "LISTENERS")

    listedEventName := map[string]struct{}{}
    for _, cell := range row {
        if "" == cell[0] {
            continue
        }

        listedEventName[cell[0]] = struct{}{}
    }

    if 1 != len(listedEventName) {
        t.Fatalf("expected the listener block to cover 1 event for --limit=1, got %v", listedEventName)
    }

    _, isWindowedEventListed := listedEventName["event.09"]
    if false == isWindowedEventListed {
        t.Fatalf("expected the listener block to follow the descending window, got %v", listedEventName)
    }
}

/* debugTestSubscriber owns listeners on two events, so the distinct-subscriber total can be told apart from the per-event sum */
type debugTestSubscriber struct {
}

func (instance *debugTestSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    listener := func(
        runtimeInstance runtimecontract.Runtime,
        eventInstance eventcontract.Event,
    ) error {
        return nil
    }

    return map[string][]eventcontract.SubscribedEvent{
        "event.alpha": {event.NewSubscribedEvent(listener, 0)},
        "event.beta":  {event.NewSubscribedEvent(listener, 0)},
    }
}

/* @info the summary total counts distinct subscribers across the dispatcher: summing the per-event counts reported one subscriber on two events as two subscribers */
func TestEventCommand_CountsASubscriberOnceAcrossEvents(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    dispatcher.AddSubscriber(&debugTestSubscriber{})

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newTestRuntime(serviceContainer),
        []string{},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "SUBSCRIBERS: 1 total") {
        t.Fatalf("expected one distinct subscriber, got %q", rendered)
    }
}

/* @info the listener detail — including the required and may-skip marks — must be reachable in the machine format: it existed only in the table, so a json consumer could never learn whether the fail-closed guarantee is armed */
func TestEventCommand_CarriesTheListenerDetailInTheVerboseJsonFormat(t *testing.T) {
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    registration := dispatcher.AddListener(
        "event.guarded",
        func(
            runtimeInstance runtimecontract.Runtime,
            eventInstance eventcontract.Event,
        ) error {
            return nil
        },
        7,
    )
    dispatcher.MarkListenerRequired(registration)

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Events struct {
                Total int `json:"total"`
            } `json:"events"`
            Listeners []struct {
                EventName string `json:"eventName"`
                Priority  int    `json:"priority"`
                Required  bool   `json:"required"`
            } `json:"listeners"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != decoded.Data.Events.Total {
        t.Fatalf("expected one event, got %d", decoded.Data.Events.Total)
    }

    if 1 != len(decoded.Data.Listeners) {
        t.Fatalf("expected one listener entry, got %v", decoded.Data.Listeners)
    }

    listenerEntry := decoded.Data.Listeners[0]
    if "event.guarded" != listenerEntry.EventName || 7 != listenerEntry.Priority || true != listenerEntry.Required {
        t.Fatalf("unexpected listener entry %+v", listenerEntry)
    }
}

/* @info without --verbose the json document keeps its previous list shape */
func TestEventCommand_KeepsThePlainJsonShapeWithoutVerbose(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTestRuntime(2),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    if 2 != envelope.Data.Total {
        t.Fatalf("expected the list payload at the data root, got %q", rendered)
    }
}

/* dispatcherWithoutInspection is an event dispatcher that implements the dispatch contract and nothing else: the inspection contract is optional, and a userland dispatcher that does not carry it is exactly what this command has to survive. */
type dispatcherWithoutInspection struct{}

func (instance *dispatcherWithoutInspection) AddListener(
    eventName string,
    listener eventcontract.EventListener,
    priority int,
) eventcontract.ListenerRegistration {
    return eventcontract.ListenerRegistration{EventName: eventName}
}

func (instance *dispatcherWithoutInspection) RemoveListener(registration eventcontract.ListenerRegistration) bool {
    return false
}

func (instance *dispatcherWithoutInspection) AddSubscriber(subscriber eventcontract.EventSubscriber) {
}

func (instance *dispatcherWithoutInspection) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    return 0
}

func (instance *dispatcherWithoutInspection) Dispatch(
    runtimeInstance runtimecontract.Runtime,
    eventInstance eventcontract.Event,
) (eventcontract.Event, error) {
    return eventInstance, nil
}

func (instance *dispatcherWithoutInspection) DispatchName(
    runtimeInstance runtimecontract.Runtime,
    eventName string,
    payload any,
) (eventcontract.Event, error) {
    return nil, nil
}

var _ eventcontract.EventDispatcher = (*dispatcherWithoutInspection)(nil)

func newRuntimeWithDispatcherWithoutInspection() *testRuntime {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return &dispatcherWithoutInspection{}, nil
        },
    )

    return newTestRuntime(serviceContainer)
}

/* @info a dispatcher that does not implement the inspection contract cannot be listed, and the command says so through a warning instead of failing or printing an empty list that reads as "no listeners are registered" — the difference matters because the answer decides whether an operator goes looking for a wiring mistake */
func TestEventCommand_DispatcherWithoutInspection_WarnsInsteadOfReportingNoEvents(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newRuntimeWithDispatcherWithoutInspection(),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "debug.notSupported") {
        t.Fatalf("expected the warning code, got %q", rendered)
    }

    if false == strings.Contains(rendered, "does not support inspection") {
        t.Fatalf("expected the warning message, got %q", rendered)
    }

    if false == strings.Contains(rendered, "dispatcherWithoutInspection") {
        t.Fatalf("expected the dispatcher type to be named, got %q", rendered)
    }

    envelope := decodeEventCommandEnvelope(t, rendered)

    if 0 != envelope.Data.Total {
        t.Fatalf("expected a total of zero beside the warning, got %d", envelope.Data.Total)
    }
}

/* @info the same refusal in the table format prints the empty summary rather than a table of nothing */
func TestEventCommand_DispatcherWithoutInspection_PrintsTheEmptySummaryInTheTableFormat(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newRuntimeWithDispatcherWithoutInspection(),
        []string{},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "EVENTS: 0 total") {
        t.Fatalf("expected the empty summary, got %q", rendered)
    }

    if false == strings.Contains(rendered, "does not support inspection") {
        t.Fatalf("expected the warning beside it, got %q", rendered)
    }
}

/* @info the listener order is the dispatch order, and every tiebreak below the priority exists so that two runs of the command print the same table: registration order is not stable across boots, so a tiebreak that never fires would leave the report shuffling under the operator */
func TestSortRegisteredListeners_OrdersByPriorityThenSourceOwnerIdAndName(t *testing.T) {
    listeners := []eventcontract.RegisteredListener{
        {Priority: 0, Source: "subscriber", Owner: "b", ListenerId: "2", ListenerName: "z"},
        {Priority: 0, Source: "listener", Owner: "b", ListenerId: "2", ListenerName: "a"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "9", ListenerName: "a"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "1", ListenerName: "b"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "1", ListenerName: "a"},
        {Priority: 10, Source: "subscriber", Owner: "z", ListenerId: "99", ListenerName: "z"},
    }

    sorted := sortRegisteredListeners(listeners)

    expectedOrderList := []eventcontract.RegisteredListener{
        {Priority: 10, Source: "subscriber", Owner: "z", ListenerId: "99", ListenerName: "z"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "1", ListenerName: "a"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "1", ListenerName: "b"},
        {Priority: 0, Source: "listener", Owner: "a", ListenerId: "9", ListenerName: "a"},
        {Priority: 0, Source: "listener", Owner: "b", ListenerId: "2", ListenerName: "a"},
        {Priority: 0, Source: "subscriber", Owner: "b", ListenerId: "2", ListenerName: "z"},
    }

    for index, listener := range sorted {
        if expectedOrderList[index] != listener {
            t.Fatalf("unexpected order at %d: %+v in %+v", index, listener, sorted)
        }
    }

    if "1" != sorted[1].ListenerId || "a" != sorted[1].Owner {
        t.Fatalf("expected the lowest owner and id first among equals, got %+v", sorted[1])
    }

    if "subscriber" != sorted[len(sorted)-1].Source {
        t.Fatalf("expected the subscriber source to sort after the listener source, got %+v", sorted[len(sorted)-1])
    }

    /* the input must not be reordered under the caller: the report reads the dispatcher's own slice */
    if "subscriber" != listeners[0].Source || 0 != listeners[0].Priority {
        t.Fatalf("expected the caller's slice to keep its order, got %+v", listeners[0])
    }
}

/* @info the column answers whether the fail-closed dispatch guarantee is armed for a listener, and the four combinations say four different things; collapsing any two of them makes an unarmed guarantee look exactly like an armed one */
func TestRenderRequiredListenerMark_TellsTheFourCombinationsApart(t *testing.T) {
    expectedList := []struct {
        required bool
        maySkip  bool
        expected string
    }{
        {true, false, "yes"},
        {true, true, "yes (may skip)"},
        {false, true, "may skip"},
        {false, false, "no"},
    }

    for _, expectedEntry := range expectedList {
        rendered := renderRequiredListenerMark(eventcontract.RegisteredListener{
            Required:                 expectedEntry.required,
            MaySkipRequiredListeners: expectedEntry.maySkip,
        })

        if expectedEntry.expected != rendered {
            t.Fatalf(
                "required=%v maySkip=%v: expected %q, got %q",
                expectedEntry.required,
                expectedEntry.maySkip,
                expectedEntry.expected,
                rendered,
            )
        }
    }
}
