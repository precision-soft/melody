package cache

import (
    "context"
    "fmt"
    "reflect"
    "time"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
)

/* the cache sweep is the one thing in this package that reads a clock, so its doubles are shared by every test file that builds a backend. */
type cacheTestTicker struct {
    channel chan time.Time
}

func (instance *cacheTestTicker) Channel() <-chan time.Time {
    return instance.channel
}

/* the Ticker contract forbids closing the channel on Stop — a consumer selecting on a stopped ticker's channel would spin on the zero value from a closed one — and demands idempotence, so the fixture's Stop does nothing */
func (instance *cacheTestTicker) Stop() {}

type cacheTestClock struct {
    now time.Time
}

func (instance *cacheTestClock) Now() time.Time {
    return instance.now
}

func (instance *cacheTestClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &cacheTestTicker{
        channel: make(chan time.Time),
    }
}

func assignTestValue(target any, value any) error {
    targetValue := reflect.ValueOf(target)
    if reflect.Pointer != targetValue.Kind() || true == targetValue.IsNil() {
        return fmt.Errorf("the test target %T is not a non-nil pointer", target)
    }

    if nil == value {
        targetValue.Elem().SetZero()

        return nil
    }

    valueOf := reflect.ValueOf(value)
    if false == valueOf.Type().AssignableTo(targetValue.Elem().Type()) {
        return fmt.Errorf("the test value %T cannot be held by a %s target", value, targetValue.Elem().Type())
    }

    targetValue.Elem().Set(valueOf)

    return nil
}

func testRememberOperations(cacheInstance cachecontract.Cache, key string, callback func(ctx context.Context, item *RememberItem) (any, error)) rememberOperations {
    return newRememberOperations(cacheInstance, key, callback)
}

func unboxTestAnswer(value any) any {
    boxed, _ := value.(rememberedValue[any])

    return boxed.value
}
