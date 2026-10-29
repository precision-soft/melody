package contract

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveTtl_RefusesAnAbsentTtlAndOneBelowTheMinimumNamingIt(t *testing.T) {
    for _, refusedTtl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(-time.Second), bound.Of(time.Second - time.Nanosecond)} {
        expiry, err := ResolveTtl(refusedTtl)
        if nil == err {
            t.Fatalf("expected the ttl %s to be refused, got the expiry %s", refusedTtl, expiry)
        }

        if "ttl" != exception.LogContext(err)["field"] {
            t.Fatalf("expected the refusal of %s to name the ttl, got %v", refusedTtl, exception.LogContext(err))
        }
    }
}

func TestResolveTtl_AnswersTheValueAndZeroForUnlimited(t *testing.T) {
    expiry, err := ResolveTtl(bound.Of(time.Second))
    if nil != err || time.Second != expiry {
        t.Fatalf("expected the minimum ttl to be applied as it is, got %s and %v", expiry, err)
    }

    unlimitedExpiry, unlimitedErr := ResolveTtl(bound.UnlimitedDuration())
    if nil != unlimitedErr || 0 != unlimitedExpiry {
        t.Fatalf("expected unlimited to answer no expiry, got %s and %v", unlimitedExpiry, unlimitedErr)
    }
}
