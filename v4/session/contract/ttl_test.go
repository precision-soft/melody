package contract

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveTtl_RefusesAnAbsentTtl(t *testing.T) {
    expiry, err := ResolveTtl(bound.Duration{})
    if nil == err || "the session ttl is required" != err.Error() {
        t.Fatalf("expected an absent ttl to be refused, got the expiry %s and %v", expiry, err)
    }

    if "ttl" != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name the ttl, got %v", exception.LogContext(err, exception.LogContextConfig{}))
    }
}

func TestResolveTtl_AnswersZeroForUnlimited(t *testing.T) {
    expiry, err := ResolveTtl(bound.UnlimitedDuration())
    if nil != err || 0 != expiry {
        t.Fatalf("expected unlimited to answer no expiry, got %s and %v", expiry, err)
    }
}

func TestResolveTtl_RefusesBelowTheMinimumAndAdmitsIt(t *testing.T) {
    expiry, err := ResolveTtl(bound.Of(time.Second - time.Nanosecond))
    if nil == err {
        t.Fatalf("expected a ttl under a second to be refused, got the expiry %s", expiry)
    }

    if "ttl" != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name the ttl, got %v", exception.LogContext(err, exception.LogContextConfig{}))
    }

    admitted, admittedErr := ResolveTtl(bound.Of(time.Second))
    if nil != admittedErr || time.Second != admitted {
        t.Fatalf("expected the minimum ttl to be applied as it is, got %s and %v", admitted, admittedErr)
    }
}
