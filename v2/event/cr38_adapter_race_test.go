package event

import (
    "sync"
    "testing"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func TestEventDispatcherAdapter_RegisteredEventsIsSafeForConcurrentReaders(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    listener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return nil
    }

    for index := 0; index < 200; index++ {
        _ = adapter.AddListener("e", listener, index)
    }

    var waitGroup sync.WaitGroup
    start := make(chan struct{})

    for worker := 0; worker < 16; worker++ {
        waitGroup.Add(1)

        go func() {
            defer waitGroup.Done()

            <-start

            for iteration := 0; iteration < 200; iteration++ {
                _ = adapter.RegisteredEvents()
            }
        }()
    }

    close(start)
    waitGroup.Wait()
}
