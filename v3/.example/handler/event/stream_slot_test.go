package event

import (
    "fmt"
    nethttp "net/http"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestStreamSlots_RefusesPastTheAccountsCapAndAdmitsAnotherAccount(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, StreamCapPerProcess)

    for range StreamCapPerUser {
        if _, taken := slots.Acquire("user-1"); false == taken {
            t.Fatal("expected a slot below the account's cap")
        }
    }

    if _, taken := slots.Acquire("user-1"); true == taken {
        t.Fatal("expected the slot past the account's cap refused")
    }

    if _, taken := slots.Acquire("user-2"); false == taken {
        t.Fatal("expected another account admitted")
    }
}

func TestStreamSlots_RefusesPastTheProcessCap(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, StreamCapPerProcess)

    for index := range StreamCapPerProcess {
        if _, taken := slots.Acquire(fmt.Sprintf("user-%d", index)); false == taken {
            t.Fatalf("expected slot %d below the process cap", index)
        }
    }

    if _, taken := slots.Acquire("user-new"); true == taken {
        t.Fatal("expected the slot past the process cap refused")
    }
}

/* a release gives one slot back, once, however often it is called */
func TestStreamSlots_AReleaseGivesTheSlotBackOnce(t *testing.T) {
    slots := NewStreamSlots(2, StreamCapPerProcess)

    release, _ := slots.Acquire("user-1")
    if _, taken := slots.Acquire("user-1"); false == taken {
        t.Fatal("expected the second slot")
    }

    release()
    release()

    if _, taken := slots.Acquire("user-1"); false == taken {
        t.Fatal("expected the released slot taken again")
    }

    if _, taken := slots.Acquire("user-1"); true == taken {
        t.Fatal("expected a release called twice to give back one slot")
    }
}

func TestStreamSlots_ConcurrentAcquisitionsKeepTheCap(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, StreamCapPerProcess)

    var mutex sync.Mutex
    taken := 0

    var waitGroup sync.WaitGroup
    for range 200 {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            if _, ok := slots.Acquire("user-1"); true == ok {
                mutex.Lock()
                taken = taken + 1
                mutex.Unlock()
            }
        }()
    }
    waitGroup.Wait()

    if StreamCapPerUser != taken {
        t.Fatalf("expected %d slots taken, got %d", StreamCapPerUser, taken)
    }
}

const countedRouteName = "example.websocket"

/* countedRequest is an authenticated reader's request matched to the route given */
func countedRequest(t *testing.T, routeName string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    request, runtimeInstance := streamRequestAs(t, "/ws", []string{entity.RoleUser, entity.RoleEditor})
    request.Attributes().Set(melodyhttp.RouteAttributeName, routeName)

    return request, runtimeInstance
}

/* the counted route holds its slot for as long as its handler runs, that is the connection's life, and gives it back when the handler returns */
func TestStreamSlotMiddleware_HoldsTheSlotForTheConnectionsLife(t *testing.T) {
    slots := NewStreamSlots(1, StreamCapPerProcess)
    request, runtimeInstance := countedRequest(t, countedRouteName)

    heldDuringTheConnection := false
    next := func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        _, taken := slots.Acquire("reader")
        heldDuringTheConnection = false == taken

        return nil, nil
    }

    if _, handlerErr := StreamSlotMiddleware(slots, countedRouteName)(next)(runtimeInstance, &recordingResponseWriter{}, request); nil != handlerErr {
        t.Fatalf("the counted route failed: %v", handlerErr)
    }

    if false == heldDuringTheConnection {
        t.Fatal("expected the connection to hold the account's slot while its handler ran")
    }

    if _, taken := slots.Acquire("reader"); false == taken {
        t.Fatal("expected the slot given back once the handler returned")
    }
}

/* past the account's cap the counted route is refused 429 before its handler accepts anything */
func TestStreamSlotMiddleware_RefusesPastTheCapBeforeTheHandler(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, StreamCapPerProcess)
    for range StreamCapPerUser {
        slots.Acquire("reader")
    }

    request, runtimeInstance := countedRequest(t, countedRouteName)

    handlerRan := false
    next := func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handlerRan = true

        return nil, nil
    }

    response, handlerErr := StreamSlotMiddleware(slots, countedRouteName)(next)(runtimeInstance, &recordingResponseWriter{}, request)
    if nil != handlerErr || nil == response || nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected the connection past the cap refused with 429, got %v, %v", response, handlerErr)
    }

    if true == handlerRan {
        t.Fatal("expected the handler never reached past the cap")
    }
}

/* every other route passes through, whatever the slots hold */
func TestStreamSlotMiddleware_LeavesEveryOtherRouteAlone(t *testing.T) {
    slots := NewStreamSlots(1, 1)
    slots.Acquire("reader")

    request, runtimeInstance := countedRequest(t, "example.products.list")

    handlerRan := false
    next := func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handlerRan = true

        return nil, nil
    }

    if _, handlerErr := StreamSlotMiddleware(slots, countedRouteName)(next)(runtimeInstance, &recordingResponseWriter{}, request); nil != handlerErr || false == handlerRan {
        t.Fatalf("expected another route to reach its handler, ran=%v err=%v", handlerRan, handlerErr)
    }
}

/* a release gives the process's place back as well as the account's */
func TestStreamSlots_AReleaseGivesTheProcessPlaceBack(t *testing.T) {
    slots := NewStreamSlots(StreamCapPerUser, 1)

    release, _ := slots.Acquire("user-1")
    release()

    if _, taken := slots.Acquire("user-2"); false == taken {
        t.Fatal("expected the released process place taken by another account")
    }
}
