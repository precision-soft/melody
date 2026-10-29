package contract

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveLeaseTtl_AnAbsentTtlHoldsForThirtySeconds(t *testing.T) {
    ttl, err := ResolveLeaseTtl(bound.Duration{})
    if nil != err || 30*time.Second != ttl {
        t.Fatalf("expected an absent ttl to hold for 30s, got %s and %v", ttl, err)
    }
}

func TestResolveLeaseTtl_AnswersTheMinimumAsItIs(t *testing.T) {
    ttl, err := ResolveLeaseTtl(bound.Of(time.Millisecond))
    if nil != err || time.Millisecond != ttl {
        t.Fatalf("expected the 1ms minimum to be applied as it is, got %s and %v", ttl, err)
    }
}

func TestResolveLeaseTtl_RefusesAnUnlimitedTtlAndOneBelowTheMinimumNamingIt(t *testing.T) {
    for _, refusedTtl := range []bound.Duration{bound.UnlimitedDuration(), bound.Of(time.Duration(0)), bound.Of(-time.Second), bound.Of(time.Millisecond - time.Nanosecond)} {
        ttl, err := ResolveLeaseTtl(refusedTtl)
        if nil == err {
            t.Fatalf("expected the ttl %s to be refused, got %s", refusedTtl, ttl)
        }

        if "Locker.CreateLock.ttl" != exception.LogContext(err)["field"] {
            t.Fatalf("expected the refusal of %s to name the ttl, got %v", refusedTtl, exception.LogContext(err))
        }
    }
}

func TestLockTtlRule_AdmitsUnlimitedForTheSessionLockers(t *testing.T) {
    resolved, err := bound.Resolve(bound.UnlimitedDuration(), LockTtlRule)
    if nil != err || false == resolved.Unlimited {
        t.Fatalf("expected the rule to admit unlimited, got %+v and %v", resolved, err)
    }
}

func TestResolveLeaseRefreshTtl_RefusesNamingTheRefreshTtl(t *testing.T) {
    for _, refusedTtl := range []bound.Duration{bound.UnlimitedDuration(), bound.Of(time.Duration(0))} {
        _, err := ResolveLeaseRefreshTtl(refusedTtl)
        if nil == err || "Lock.Refresh.ttl" != exception.LogContext(err)["field"] {
            t.Fatalf("expected the refresh ttl %s to be refused naming Lock.Refresh.ttl, got %v", refusedTtl, err)
        }
    }

    if ttl, err := ResolveLeaseRefreshTtl(bound.Duration{}); nil != err || 30*time.Second != ttl {
        t.Fatalf("expected an absent refresh ttl to hold for 30s, got %s and %v", ttl, err)
    }
}
