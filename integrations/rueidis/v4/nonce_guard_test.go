package rueidis

import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/runtime"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestRedisNonceGuard_FirstUseThenReplay(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuard(client, NonceGuardConfig{KeyPrefix: "melody:nonce:test"})

    /* the nonce carries the clock so two runs of this package inside the remembered window cannot collide: the guard is doing its job when it reports a fixed nonce as already seen, and a test that reads that as a failure is testing the previous run */
    nonce := fmt.Sprintf("nonce-replay-%d", time.Now().UnixNano())

    seenFirst, firstErr := guard.Remember(newTokenStoreRuntime(), nonce, bound.Of(5*time.Second))
    if nil != firstErr {
        t.Fatalf("first remember: %v", firstErr)
    }

    if true == seenFirst {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    seenSecond, secondErr := guard.Remember(newTokenStoreRuntime(), nonce, bound.Of(5*time.Second))
    if nil != secondErr {
        t.Fatalf("second remember: %v", secondErr)
    }

    if false == seenSecond {
        t.Fatal("expected the replayed nonce to be reported as seen")
    }
}

func TestRedisNonceGuard_RefusesAnAbsentSubMillisecondOrUnlimitedTtl(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuard(client, NonceGuardConfig{KeyPrefix: "melody:nonce:test"})
    nonce := fmt.Sprintf("nonce-refused-%d", time.Now().UnixNano())

    for _, ttl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(time.Microsecond), bound.UnlimitedDuration()} {
        seen, rememberErr := guard.Remember(newTokenStoreRuntime(), nonce, ttl)
        if true == seen || nil == rememberErr || securitycontract.NonceTtlRule.Name != exception.LogContext(rememberErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the ttl %v to be refused naming the ttl, got seen=%t err=%v", ttl, seen, rememberErr)
        }
    }

    if seen, _ := guard.Remember(newTokenStoreRuntime(), nonce, bound.Of(time.Minute)); true == seen {
        t.Fatal("expected a refused ttl to store nothing")
    }
}

/* newWedgedNonceGuard hands back a guard over a client whose replies stop arriving from here on, so the Lua record of Remember runs against a store that accepts the command and never answers */
func newWedgedNonceGuard(t *testing.T, config NonceGuardConfig) (*NonceGuard, *gate) {
    t.Helper()

    config.KeyPrefix = "melody:nonce:test:wedge:" + t.Name()
    if true == config.CallTimeout.IsAbsent() {
        config.CallTimeout = bound.Of(50 * time.Millisecond)
    }

    client, guardGate := dialGated(t)
    guard := NewNonceGuard(client, config)

    if _, warmErr := guard.Remember(newTokenStoreRuntime(), "warm", bound.Of(time.Second)); nil != warmErr {
        t.Fatalf("warm-up remember: %v", warmErr)
    }

    guardGate.Wedge()

    return guard, guardGate
}

func TestRedisNonceGuard_RememberIsBoundedByTheCallTimeout(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t, NonceGuardConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, rememberErr := guard.Remember(newTokenStoreRuntime(), "record", bound.Of(5*time.Second))

        return rememberErr
    }))
}

/* a request that already carries a deadline TIGHTER than the call timeout keeps it: with the call timeout at a second, a runtime carrying ten milliseconds is refused in tens of milliseconds, where a cap that replaced the request context would wait the full second */
func TestRedisNonceGuard_RememberKeepsATighterRequestDeadline(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t, NonceGuardConfig{CallTimeout: bound.Of(time.Second)})

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    started := time.Now()
    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, rememberErr := guard.Remember(runtimeInstance, "tight", bound.Of(5*time.Second))

        return rememberErr
    }))

    if elapsed := time.Since(started); 500*time.Millisecond < elapsed {
        t.Fatalf("expected the request's own deadline to end the remember, it took %s", elapsed)
    }
}

func TestNewNonceGuard_RefusesACallTimeoutOutsideItsRuleNamingTheField(t *testing.T) {
    for _, callTimeout := range []bound.Duration{bound.UnlimitedDuration(), bound.Of(time.Duration(0)), bound.Of(time.Microsecond)} {
        expectPanicNamingField(t, "NonceGuardConfig.CallTimeout", func() {
            NewNonceGuard(fakeClient{}, NonceGuardConfig{CallTimeout: callTimeout})
        })
    }
}

func TestNewNonceGuard_KeepsAConfiguredCallTimeout(t *testing.T) {
    guard := NewNonceGuard(fakeClient{}, NonceGuardConfig{CallTimeout: bound.Of(750 * time.Millisecond)})

    if 750*time.Millisecond != guard.callTimeout {
        t.Fatalf("expected the configured call timeout, got %v", guard.callTimeout)
    }
}

func TestNewNonceGuard_DefaultCallTimeoutAndPrefix(t *testing.T) {
    guard := NewNonceGuard(fakeClient{}, NonceGuardConfig{})

    if defaultNonceGuardCallTimeout != guard.callTimeout {
        t.Fatalf("expected the default call timeout, got %v", guard.callTimeout)
    }

    if defaultNonceGuardPrefix != guard.keyPrefix {
        t.Fatalf("expected the default key prefix, got %q", guard.keyPrefix)
    }
}

func TestNewNonceGuard_HandsThePrefixThroughAndKeepsTheDefaultBudget(t *testing.T) {
    guard := NewNonceGuard(fakeClient{}, NonceGuardConfig{KeyPrefix: "melody:nonce:custom"})

    if "melody:nonce:custom" != guard.keyPrefix {
        t.Fatalf("expected the prefix to reach the guard, got %q", guard.keyPrefix)
    }

    if defaultNonceGuardCallTimeout != guard.callTimeout {
        t.Fatalf("expected the default call timeout beside a custom prefix, got %v", guard.callTimeout)
    }
}

func TestNewNonceGuard_AnEmptyPrefixKeepsTheDefault(t *testing.T) {
    guard := NewNonceGuard(fakeClient{}, NonceGuardConfig{KeyPrefix: ""})

    if defaultNonceGuardPrefix != guard.keyPrefix {
        t.Fatalf("expected an empty prefix to read as the default, got %q", guard.keyPrefix)
    }
}

func TestNonceGuard_AStoreFailureNamesNeitherTheNonceNorItsKey(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t, NonceGuardConfig{})

    const nonce = "2fa:alice:739204"

    for _, ttl := range []time.Duration{5 * time.Second, 0} {
        _, rememberErr := guard.Remember(newTokenStoreRuntime(), nonce, bound.Of(ttl))
        if nil == rememberErr {
            t.Fatalf("expected the wedged store to fail the call at ttl %s", ttl)
        }

        if true == strings.Contains(rememberErr.Error(), "739204") {
            t.Fatalf("the nonce leaked through the error at ttl %s: %q", ttl, rememberErr.Error())
        }

        for key, value := range exception.LogContext(rememberErr, exception.LogContextConfig{}) {
            if true == strings.Contains(fmt.Sprintf("%v", value), "739204") {
                t.Fatalf("the nonce leaked through log field %q at ttl %s: %v", key, ttl, value)
            }
        }
    }
}
