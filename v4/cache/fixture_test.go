package cache

import (
    "context"
    "fmt"
    "reflect"
    "time"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

type contextlessTestRuntime struct {
    containercontract.Runtime
}

func (instance *contextlessTestRuntime) Context() context.Context {
    return nil
}

const refusedRuntimeMessage = "the runtime is nil or carries no context"

func refusedTestRuntimes() []containercontract.Runtime {
    var typedNil *contextlessTestRuntime

    return []containercontract.Runtime{nil, typedNil, &contextlessTestRuntime{}}
}

type cacheTestTicker struct {
    channel chan time.Time
}

func (instance *cacheTestTicker) Channel() <-chan time.Time {
    return instance.channel
}

/* the Ticker contract forbids closing the channel on Stop and demands idempotence, so Stop does nothing */
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

func (instance *cacheTestClock) NewTimer(duration time.Duration) clockcontract.Timer {
    return clock.NewSystemClock().NewTimer(duration)
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

func testRememberOperations(cacheInstance cachecontract.Cache, key string, callback func(runtimeInstance containercontract.Runtime, item *RememberItem) (any, error)) rememberOperations {
    return newRememberOperations(cacheInstance, key, callback)
}

func unboxTestAnswer(value any) any {
    boxed, _ := value.(rememberedValue[any])

    return boxed.value
}
