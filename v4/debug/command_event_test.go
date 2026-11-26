package debug

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

type eventCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            EventName     string `json:"eventName"`
            ListenerCount int    `json:"listenerCount"`
        } `json:"items"`
        Total  int `json:"total"`
        Limit  bound.Count `json:"limit"`
        Offset bound.Count `json:"offset"`
    } `json:"data"`
}

func newEventTestRuntime(eventCount int) *testRuntime {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    for index := 0; index < eventCount; index++ {
        dispatcher.AddListener(
            fmt.Sprintf("event.%02d", index),
            func(
                runtimeInstance containercontract.Runtime,
                eventInstance eventcontract.Event,
            ) error {
                return nil
            },
            eventcontract.ListenerConfig{},
        )
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
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

type debugTestSubscriber struct {
}

func (instance *debugTestSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    listener := func(
        runtimeInstance containercontract.Runtime,
        eventInstance eventcontract.Event,
    ) error {
        return nil
    }

    return map[string][]eventcontract.SubscribedEvent{
        "event.alpha": {event.NewSubscribedEvent(listener, eventcontract.ListenerConfig{})},
        "event.beta":  {event.NewSubscribedEvent(listener, eventcontract.ListenerConfig{})},
    }
}

func TestEventCommand_CountsASubscriberOnceAcrossEvents(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    dispatcher.AddSubscriber(&debugTestSubscriber{})

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
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

func TestEventCommand_CarriesTheListenerDetailInTheVerboseJsonFormat(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    dispatcher.AddListener(
        "event.guarded",
        func(
            runtimeInstance containercontract.Runtime,
            eventInstance eventcontract.Event,
        ) error {
            return nil
        },
        eventcontract.ListenerConfig{Priority: 7, Failure: eventcontract.ListenerFailureConfig{Required: true}},
    )

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
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
                Config    struct {
                    Priority int `json:"priority"`
                    Failure  struct {
                        Required bool `json:"required"`
                    } `json:"failure"`
                } `json:"config"`
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
    if "event.guarded" != listenerEntry.EventName || 7 != listenerEntry.Config.Priority || true != listenerEntry.Config.Failure.Required {
        t.Fatalf("unexpected listener entry %+v", listenerEntry)
    }
}

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

func TestEventCommand_VerboseListsListenersInDispatchOrder(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    registrationPattern := []string{"A", "A", "B", "B", "A", "A", "B", "B", "A", "B", "A"}
    for _, mark := range registrationPattern {
        if "A" == mark {
            dispatcher.AddListener("event.order", eventOrderProbeListenerAlpha, eventcontract.ListenerConfig{})
        } else {
            dispatcher.AddListener("event.order", eventOrderProbeListenerBeta, eventcontract.ListenerConfig{})
        }
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "LISTENERS")
    if 11 != len(row) {
        t.Fatalf("expected eleven listener rows, got %d in %q", len(row), rendered)
    }

    expectedSuffixes := []string{"Alpha", "Alpha", "Beta", "Beta", "Alpha", "Alpha", "Beta", "Beta", "Alpha", "Beta", "Alpha"}

    for index, cell := range row {
        if fmt.Sprintf("%d", index+1) != cell[1] {
            t.Fatalf("expected the order column to carry the dispatch rank, got %q at row %d", cell[1], index)
        }

        if false == strings.HasSuffix(cell[8], expectedSuffixes[index]) {
            t.Fatalf("expected row %d to carry the listener registered %s, got %q", index, expectedSuffixes[index], cell[8])
        }
    }
}

func eventOrderProbeListenerAlpha(
    runtimeInstance containercontract.Runtime,
    eventInstance eventcontract.Event,
) error {
    return nil
}

func eventOrderProbeListenerBeta(
    runtimeInstance containercontract.Runtime,
    eventInstance eventcontract.Event,
) error {
    return nil
}

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
            Config: eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{Required: expectedEntry.required, MaySkipRequiredListeners: expectedEntry.maySkip}},
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

func TestEventCommand_RendersTheDeclaredServingProcessListeners(t *testing.T) {
    command := NewEventCommand(func() []DeferredListener {
        return []DeferredListener{
            {EventName: "kernel.request", Priority: 50, ListenerName: "security resolution listener", Note: "registered only in the http serving process"},
        }
    })

    rendered, runErr := runDebugCommand(
        command,
        newEventTestRuntime(1),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "SERVING-PROCESS LISTENERS")
    if 1 != len(row) {
        t.Fatalf("expected the declared block, got %d rows in %q", len(row), rendered)
    }

    if "kernel.request" != row[0][0] || "50" != row[0][1] || "security resolution listener" != row[0][2] {
        t.Fatalf("expected the declaration columns, got %v", row[0])
    }

    verboseRendered, verboseErr := runDebugCommand(
        command,
        newEventTestRuntime(1),
        []string{"--format=json", "--verbose"},
    )
    if nil != verboseErr {
        t.Fatalf("expected no error, got %v", verboseErr)
    }

    if false == strings.Contains(verboseRendered, "servingProcessListeners") {
        t.Fatalf("expected the declaration in the verbose json document, got %q", verboseRendered)
    }

    bareRendered, bareErr := runDebugCommand(
        &EventCommand{},
        newEventTestRuntime(1),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != bareErr {
        t.Fatalf("expected no error, got %v", bareErr)
    }

    if true == strings.Contains(bareRendered, "SERVING-PROCESS LISTENERS") {
        t.Fatalf("expected the zero-value command to declare nothing, got %q", bareRendered)
    }
}

func TestEventCommand_DeclaresTheServingProcessListenersAtEveryVerbosity(t *testing.T) {
    command := NewEventCommand(func() []DeferredListener {
        return []DeferredListener{
            {EventName: "kernel.request", Priority: 50, ListenerName: "security resolution listener", Note: "registered only in the http serving process"},
        }
    })

    rendered, runErr := runDebugCommand(command, newEventTestRuntime(2), []string{"--format=json"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Items []struct {
                EventName string `json:"eventName"`
            } `json:"items"`
            Total                   int                `json:"total"`
            ServingProcessListeners []DeferredListener `json:"servingProcessListeners"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 2 != len(decoded.Data.Items) || 2 != decoded.Data.Total {
        t.Fatalf("expected the listing to keep data.items, got %q", rendered)
    }

    if 1 != len(decoded.Data.ServingProcessListeners) {
        t.Fatalf("expected the declaration beside the listing, got %q", rendered)
    }

    if "security resolution listener" != decoded.Data.ServingProcessListeners[0].ListenerName {
        t.Fatalf("expected the declared listener, got %#v", decoded.Data.ServingProcessListeners[0])
    }

    bareRendered, bareErr := runDebugCommand(&EventCommand{}, newEventTestRuntime(1), []string{"--format=json"})
    if nil != bareErr {
        t.Fatalf("expected no error, got %v", bareErr)
    }

    if true == strings.Contains(bareRendered, "servingProcessListeners") {
        t.Fatalf("expected no declaration key when nothing is declared, got %q", bareRendered)
    }
}

func TestEventCommand_DescendingOrderReachesTheListenerDetail(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTestRuntime(3),
        []string{"--format=json", "--verbose", "--order=desc"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Events struct {
                Items []struct {
                    EventName string `json:"eventName"`
                } `json:"items"`
            } `json:"events"`
            Listeners []struct {
                EventName string `json:"eventName"`
                Order     int    `json:"order"`
            } `json:"listeners"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 3 != len(decoded.Data.Events.Items) || 3 != len(decoded.Data.Listeners) {
        t.Fatalf("expected three events with one listener each, got %q", rendered)
    }

    for index, item := range decoded.Data.Events.Items {
        if item.EventName != decoded.Data.Listeners[index].EventName {
            t.Fatalf("expected the detail to follow the listing, got %s beside %s in %q", decoded.Data.Listeners[index].EventName, item.EventName, rendered)
        }
    }

    if "event.02" != decoded.Data.Listeners[0].EventName {
        t.Fatalf("expected the detail to start at the last event name, got %s", decoded.Data.Listeners[0].EventName)
    }

    if 1 != decoded.Data.Listeners[0].Order {
        t.Fatalf("expected the dispatch rank to stay ascending inside an event, got %d", decoded.Data.Listeners[0].Order)
    }
}


func newEventTagsTestRuntime() containercontract.Runtime {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    dispatcher.DeclareEvent("event.declared", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cache", "release"}})
    dispatcher.DeclareEvent("event.unheard", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"audit"}})
    dispatcher.AddListener("event.declared", eventOrderProbeListenerAlpha, eventcontract.ListenerConfig{Priority: 20, Tags: []string{"release"}})
    dispatcher.AddListener("event.declared", eventOrderProbeListenerBeta, eventcontract.ListenerConfig{Priority: 10})

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )

    return newTestRuntime(serviceContainer)
}

func TestEventCommand_VerboseTableCarriesTheTagsAndTheDeclarations(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTagsTestRuntime(),
        []string{"--format=table", "--table-width=400", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "LISTENERS")
    if 2 != len(row) {
        t.Fatalf("expected two listener rows, got %d in %q", len(row), rendered)
    }

    if "release" != row[0][4] || "yes" != row[0][5] || "-" != row[1][4] || "-" != row[1][5] {
        t.Fatalf("expected the tags and after-failure columns, got %v", row)
    }

    declarationRow := debugTableBlockRow(rendered, "DECLARED EVENTS")
    if 2 != len(declarationRow) || "event.declared" != declarationRow[0][0] || "cache,release" != declarationRow[0][1] || "event.unheard" != declarationRow[1][0] || "audit" != declarationRow[1][1] {
        t.Fatalf("expected both declarations listed, the one with no listener included, got %v in %q", declarationRow, rendered)
    }

    if false == strings.Contains(rendered, "tags to run after failure") {
        t.Fatalf("expected the declarations headed by their field, got %q", rendered)
    }
}

func TestEventCommand_VerboseJsonCarriesTheTagsAndTheDeclarations(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &EventCommand{},
        newEventTagsTestRuntime(),
        []string{"--format=json", "--verbose"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Listeners []struct {
                Config struct {
                    Tags []string `json:"tags"`
                } `json:"config"`
                RunsAfterFailure bool `json:"runsAfterFailure"`
            } `json:"listeners"`
            Declarations []struct {
                EventName string `json:"eventName"`
                Config    struct {
                    TagsToRunAfterFailure []string `json:"tagsToRunAfterFailure"`
                } `json:"config"`
            } `json:"declarations"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 2 != len(decoded.Data.Listeners) || false == decoded.Data.Listeners[0].RunsAfterFailure || 1 != len(decoded.Data.Listeners[0].Config.Tags) || true == decoded.Data.Listeners[1].RunsAfterFailure {
        t.Fatalf("expected the listener tags and marks, got %+v", decoded.Data.Listeners)
    }

    if 2 != len(decoded.Data.Declarations) || "event.declared" != decoded.Data.Declarations[0].EventName || 2 != len(decoded.Data.Declarations[0].Config.TagsToRunAfterFailure) {
        t.Fatalf("expected the declarations, got %+v", decoded.Data.Declarations)
    }

    if false == strings.Contains(rendered, `"tags":[]`) {
        t.Fatalf("expected an untagged listener's tags as an empty list, got %q", rendered)
    }
}

func TestRenderRunsAfterFailureMark_NamesTheReplaceDoor(t *testing.T) {
    for _, expectedEntry := range []struct {
        runsAfterFailure bool
        replaces         bool
        expected         string
    }{
        {true, true, "yes (may replace)"},
        {true, false, "yes"},
        {false, true, "-"},
        {false, false, "-"},
    } {
        rendered := renderRunsAfterFailureMark(eventcontract.RegisteredListener{
            RunsAfterFailure: expectedEntry.runsAfterFailure,
            Config:           eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{ReplacesResponseAfterFailure: expectedEntry.replaces}},
        })
        if expectedEntry.expected != rendered {
            t.Fatalf("expected %q for %+v, got %q", expectedEntry.expected, expectedEntry, rendered)
        }
    }
}

func TestEventCommand_VerboseJsonCarriesTheReplaceDoor(t *testing.T) {
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})
    dispatcher.DeclareEvent("event.declared", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"release"}})
    dispatcher.AddListener("event.declared", eventOrderProbeListenerAlpha, eventcontract.ListenerConfig{Tags: []string{"release"}, Failure: eventcontract.ListenerFailureConfig{ReplacesResponseAfterFailure: true}})

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runDebugCommand(&EventCommand{}, newTestRuntime(serviceContainer), []string{"--format=json", "--verbose"})
    if nil != runErr || false == strings.Contains(rendered, `"replacesResponseAfterFailure":true`) {
        t.Fatalf("expected the door in the json listing, got %v %q", runErr, rendered)
    }
}
