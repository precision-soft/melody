package output

import (
    "strconv"
    "sync"
    "testing"
)

func TestApplicationVersion_SetAndGet(t *testing.T) {
    SetApplicationVersion("1.2.3")

    if "1.2.3" != getApplicationVersion() {
        t.Fatalf("unexpected version")
    }
}

func TestApplicationVersion_DefaultIsEmpty(t *testing.T) {
    applicationVersion.Store("")

    if "" != getApplicationVersion() {
        t.Fatalf("expected empty version")
    }
}

func TestApplicationVersion_ConcurrentSetGet(t *testing.T) {
    var waitGroup sync.WaitGroup
    iterations := 100

    for workerIndex := 0; workerIndex < 4; workerIndex++ {
        waitGroup.Add(1)
        go func(workerId int) {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                SetApplicationVersion(strconv.Itoa(workerId) + "." + strconv.Itoa(index))
            }
        }(workerIndex)
    }

    for readerIndex := 0; readerIndex < 4; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                _ = getApplicationVersion()
            }
        }()
    }

    waitGroup.Wait()
}
