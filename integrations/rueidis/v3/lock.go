package rueidis

import (
    "crypto/rand"
    "encoding/hex"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/redis/rueidis"
)

var lockAcquireScript = rueidis.NewLuaScript(`local current = redis.call("get", KEYS[1])
if current == false or current == ARGV[1] then
    if tonumber(ARGV[2]) > 0 then
        redis.call("set", KEYS[1], ARGV[1], "PX", tonumber(ARGV[2]))
    else
        redis.call("set", KEYS[1], ARGV[1])
    end
    return 1
end
return 0`)

var lockReleaseScript = rueidis.NewLuaScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)

var lockRefreshScript = rueidis.NewLuaScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)

func NewLocker(client rueidis.Client) *Locker {
    if nil == client {
        exception.Panic(exception.NewError("redis lock client is nil", nil, nil))
    }

    return &Locker{
        client: client,
    }
}

type Locker struct {
    client rueidis.Client
}

func (instance *Locker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &redisLock{
        client: instance.client,
        name:   name,
        ttl:    ttl,
        token:  newLockToken(),
    }
}

type redisLock struct {
    client rueidis.Client
    name   string
    ttl    time.Duration
    token  string
}

func (instance *redisLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    milliseconds := "0"
    if 0 < instance.ttl {
        milliseconds = strconv.FormatInt(floorPositiveMilliseconds(instance.ttl), 10)
    }

    result := lockAcquireScript.Exec(runtimeInstance.Context(), instance.client, []string{instance.name}, []string{instance.token, milliseconds})

    acquired, resultErr := result.AsInt64()
    if nil != resultErr {
        return false, exception.NewError("redis lock acquire failed", map[string]any{"name": instance.name}, resultErr)
    }

    return 1 == acquired, nil
}

func (instance *redisLock) Release(runtimeInstance runtimecontract.Runtime) error {
    result := lockReleaseScript.Exec(runtimeInstance.Context(), instance.client, []string{instance.name}, []string{instance.token})
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis lock release failed", map[string]any{"name": instance.name}, resultErr)
    }

    return nil
}

func (instance *redisLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 >= ttl {
        return exception.NewError("redis lock refresh ttl must be positive", map[string]any{"name": instance.name}, nil)
    }

    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(ttl), 10)

    result := lockRefreshScript.Exec(runtimeInstance.Context(), instance.client, []string{instance.name}, []string{instance.token, milliseconds})

    refreshed, resultErr := result.AsInt64()
    if nil != resultErr {
        return exception.NewError("redis lock refresh failed", map[string]any{"name": instance.name}, resultErr)
    }

    if 0 == refreshed {
        return exception.NewError("redis lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

func floorPositiveMilliseconds(ttl time.Duration) int64 {
    milliseconds := ttl.Milliseconds()
    if 0 == milliseconds {
        return 1
    }

    return milliseconds
}

func newLockToken() string {
    buffer := make([]byte, 16)

    _, readErr := rand.Read(buffer)
    if nil != readErr {
        exception.Panic(exception.NewError("could not generate a lock token", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*redisLock)(nil)
