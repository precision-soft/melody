package event

import (
    nethttp "net/http"
    "sync"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    /* StreamCapPerUser is the number of event streams one account holds open at once */
    StreamCapPerUser = 4

    /* StreamCapPerProcess is the number of event streams this process holds open at once, whoever holds them */
    StreamCapPerProcess = 256
)

/* StreamSlots counts the event streams held open, per account and in total. A stream holds a connection and a descriptor for its whole life, and the request budget counts it once, at open, so without a count one account could hold every descriptor of the process. */
type StreamSlots struct {
    mutex      sync.Mutex
    perUserCap int
    totalCap   int
    perUser    map[string]int
    total      int
}

func NewStreamSlots(perUserCap int, totalCap int) *StreamSlots {
    return &StreamSlots{
        perUserCap: perUserCap,
        totalCap:   totalCap,
        perUser:    map[string]int{},
    }
}

/* Acquire takes a slot for the account, or answers false when the account or the process holds its cap. The release it answers gives the slot back once, however many times it is called. */
func (instance *StreamSlots) Acquire(userId string) (func(), bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if instance.totalCap <= instance.total || instance.perUserCap <= instance.perUser[userId] {
        return nil, false
    }

    instance.perUser[userId] = instance.perUser[userId] + 1
    instance.total = instance.total + 1

    var releaseOnce sync.Once

    return func() {
        releaseOnce.Do(func() {
            instance.release(userId)
        })
    }, true
}

func (instance *StreamSlots) release(userId string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.total = instance.total - 1

    if 1 >= instance.perUser[userId] {
        delete(instance.perUser, userId)

        return
    }

    instance.perUser[userId] = instance.perUser[userId] - 1
}

/* StreamSlotMiddleware counts, in the same slots as the event stream door, the connections of the route named, which streams the same hub through a handler this application does not write: the websocket integration's. The slot is taken before that handler accepts the upgrade, past which a refusal would be discarded, and given back when the handler returns, at the end of the connection. Every other route passes through untouched. */
func StreamSlotMiddleware(slots *StreamSlots, routeName string) melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            if routeName != request.RouteName() {
                return next(runtimeInstance, writer, request)
            }

            release, slotTaken := acquireStreamSlot(runtimeInstance, slots)
            if false == slotTaken {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusTooManyRequests, "too many open event streams"), nil
            }
            defer release()

            return next(runtimeInstance, writer, request)
        }
    }
}
