package rueidis

import (
    "context"
    "fmt"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
)

func TestRedisNonceGuard_FirstUseThenReplay(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuardWithPrefix(client, "melody:nonce:test")

    nonce := fmt.Sprintf("nonce-replay-%d", time.Now().UnixNano())

    seenFirst, firstErr := guard.Remember(newTokenStoreRuntime(), nonce, 5*time.Second)
    if nil != firstErr {
        t.Fatalf("first remember: %v", firstErr)
    }

    if true == seenFirst {
        t.Fatal("expected the first use of a nonce to be unseen")
    }

    seenSecond, secondErr := guard.Remember(newTokenStoreRuntime(), nonce, 5*time.Second)
    if nil != secondErr {
        t.Fatalf("second remember: %v", secondErr)
    }

    if false == seenSecond {
        t.Fatal("expected the replayed nonce to be reported as seen")
    }
}

func TestRedisNonceGuard_NonPositiveTtlIsNotStored(t *testing.T) {
    client := newTokenStoreClient(t)
    guard := NewNonceGuardWithPrefix(client, "melody:nonce:test")

    seen, rememberErr := guard.Remember(newTokenStoreRuntime(), fmt.Sprintf("nonce-expired-%d", time.Now().UnixNano()), 0)
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if true == seen {
        t.Fatal("expected a non-positive ttl to report unseen without storing")
    }
}

func newWedgedNonceGuard(t *testing.T, options ...NonceGuardOption) (*NonceGuard, *gate) {
    t.Helper()

    client, guardGate := dialGated(t)
    guard := NewNonceGuardWithOptions(client, append([]NonceGuardOption{
        WithNonceGuardKeyPrefix("melody:nonce:test:wedge:" + t.Name()),
        WithNonceGuardCallTimeout(50 * time.Millisecond),
    }, options...)...)

    if _, warmErr := guard.Remember(newTokenStoreRuntime(), "warm", time.Second); nil != warmErr {
        t.Fatalf("warm-up remember: %v", warmErr)
    }

    guardGate.Wedge()

    return guard, guardGate
}

func TestRedisNonceGuard_RememberIsBoundedByTheCallTimeout(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, rememberErr := guard.Remember(newTokenStoreRuntime(), "record", 5*time.Second)

        return rememberErr
    }))
}

func TestRedisNonceGuard_ExistenceCheckIsBoundedByTheCallTimeout(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, rememberErr := guard.Remember(newTokenStoreRuntime(), "edge", 0)

        return rememberErr
    }))
}

func TestRedisNonceGuard_RememberKeepsATighterRequestDeadline(t *testing.T) {
    guard, _ := newWedgedNonceGuard(t, WithNonceGuardCallTimeout(time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    started := time.Now()
    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, rememberErr := guard.Remember(runtimeInstance, "tight", 5*time.Second)

        return rememberErr
    }))

    if elapsed := time.Since(started); 500*time.Millisecond < elapsed {
        t.Fatalf("expected the request's own deadline to end the remember, it took %s", elapsed)
    }
}

func TestWithNonceGuardCallTimeout_NonPositiveFallsBackToTheDefault(t *testing.T) {
    cases := map[string]time.Duration{
        "zero":     0,
        "negative": -1 * time.Second,
    }

    for name, timeout := range cases {
        t.Run(name, func(t *testing.T) {
            guard := &NonceGuard{callTimeout: defaultNonceGuardCallTimeout}

            WithNonceGuardCallTimeout(timeout)(guard)

            if defaultNonceGuardCallTimeout != guard.callTimeout {
                t.Fatalf("expected a %v call timeout to fall back to the default, got %v", timeout, guard.callTimeout)
            }
        })
    }
}

func TestWithNonceGuardCallTimeout_PositiveIsKept(t *testing.T) {
    guard := &NonceGuard{callTimeout: defaultNonceGuardCallTimeout}

    WithNonceGuardCallTimeout(750 * time.Millisecond)(guard)

    if 750*time.Millisecond != guard.callTimeout {
        t.Fatalf("expected a positive call timeout to be kept, got %v", guard.callTimeout)
    }
}

func TestNewNonceGuard_DefaultCallTimeoutAndPrefix(t *testing.T) {
    guard := NewNonceGuard(fakeClient{})

    if defaultNonceGuardCallTimeout != guard.callTimeout {
        t.Fatalf("expected the default call timeout, got %v", guard.callTimeout)
    }

    if defaultNonceGuardPrefix != guard.keyPrefix {
        t.Fatalf("expected the default key prefix, got %q", guard.keyPrefix)
    }
}

func TestNewNonceGuardWithPrefix_HandsThePrefixThroughAndKeepsTheDefaultBudget(t *testing.T) {
    guard := NewNonceGuardWithPrefix(fakeClient{}, "melody:nonce:custom")

    if "melody:nonce:custom" != guard.keyPrefix {
        t.Fatalf("expected the prefix to reach the guard, got %q", guard.keyPrefix)
    }

    if defaultNonceGuardCallTimeout != guard.callTimeout {
        t.Fatalf("expected the default call timeout beside a custom prefix, got %v", guard.callTimeout)
    }
}

func TestWithNonceGuardKeyPrefix_EmptyKeepsTheDefault(t *testing.T) {
    guard := &NonceGuard{keyPrefix: "melody:nonce:before"}

    WithNonceGuardKeyPrefix("")(guard)

    if defaultNonceGuardPrefix != guard.keyPrefix {
        t.Fatalf("expected an empty prefix to read as the default, got %q", guard.keyPrefix)
    }
}
