package messagebus

import (
    "sync"
    "testing"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestHandlerLocator_ConcurrentRegisterAndLookup(t *testing.T) {
    locator := NewHandlerLocator()

    var waitGroup sync.WaitGroup

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
                return nil
            })
        }()
    }

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for _, handler := range locator.HandlersFor(taskCreated{}) {
                _ = handler
            }
        }()
    }

    waitGroup.Wait()
}
