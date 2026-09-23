package service

import (
    nethttp "net/http"
    "testing"
    "time"
)

var providerClockSentAt = time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)

func providerClockHeaders(date string, age string) nethttp.Header {
    headers := nethttp.Header{}
    if "" != date {
        headers.Set("Date", date)
    }

    if "" != age {
        headers.Set("Age", age)
    }

    return headers
}

/* a provider on a clock that agrees with this one to within the second its date is read to and the round trip
   has no offset at all, so its stamps land on this clock exactly as they came */
func TestReadProviderClock_AnOffsetInsideTheUncertaintyIsNoOffset(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", ""), providerClockSentAt, providerClockSentAt.Add(200*time.Millisecond))

    if false == reading.Measured || 0 != reading.Offset {
        t.Fatalf("expected a measured clock without an offset, got %+v", reading)
    }

    if 600*time.Millisecond != reading.Uncertainty {
        t.Errorf("expected half a second of date plus half the round trip, got %s", reading.Uncertainty)
    }
}

func TestReadProviderClock_MeasuresAClockAhead(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:04:00 GMT", ""), providerClockSentAt, providerClockSentAt)

    if false == reading.Measured || 4*time.Minute+500*time.Millisecond != reading.Offset {
        t.Fatalf("expected four minutes and the middle of the second, got %+v", reading)
    }

    stamped := time.Date(2026, time.September, 8, 9, 3, 30, 0, time.UTC)
    if onThisClock := reading.onThisClock(stamped); false == onThisClock.Equal(stamped.Add(-reading.Offset)) {
        t.Errorf("expected the stamp moved back by the offset, got %s", onThisClock)
    }

    if false == reading.AnsweredAt.Equal(time.Date(2026, time.September, 8, 9, 4, 1, 0, time.UTC)) {
        t.Errorf("expected the answer bounded by the end of its second, got %s", reading.AnsweredAt)
    }
}

func TestReadProviderClock_MeasuresAClockBehind(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 08:58:00 GMT", ""), providerClockSentAt, providerClockSentAt)

    if -2*time.Minute+500*time.Millisecond != reading.Offset {
        t.Fatalf("expected two minutes behind less the middle of the second, got %+v", reading)
    }
}

/* the Age a cache adds is part of the provider's clock at the moment the answer was handed out */
func TestReadProviderClock_AddsTheAgeOfACachedAnswer(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 08:00:00 GMT", "3600"), providerClockSentAt, providerClockSentAt)

    if 0 != reading.Offset {
        t.Fatalf("expected an hour-old cached answer of an agreeing clock to carry no offset, got %+v", reading)
    }
}

/* an Age that is not a non-negative integer is ignored, as the caching rules say */
func TestReadProviderClock_IgnoresAnAgeThatIsNotANonNegativeInteger(t *testing.T) {
    for _, age := range []string{"-5", "soon", "1.5"} {
        reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", age), providerClockSentAt, providerClockSentAt)

        if false == reading.Measured || 0 != reading.Offset {
            t.Errorf("age %q: expected it ignored, got %+v", age, reading)
        }
    }
}

func TestReadProviderClock_AnAnswerWithoutAReadableDateIsUnmeasured(t *testing.T) {
    for _, date := range []string{"", "yesterday", "2026-09-08T09:00:00Z"} {
        reading := readProviderClock(providerClockHeaders(date, ""), providerClockSentAt, providerClockSentAt)

        if true == reading.Measured || 0 != reading.Offset {
            t.Errorf("date %q: expected an unmeasured clock, got %+v", date, reading)
        }
    }
}

/* a round trip read backwards — a clock stepped between the two reads — is no round trip, not a negative one */
func TestReadProviderClock_ARoundTripReadBackwardsCountsAsNone(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", ""), providerClockSentAt, providerClockSentAt.Add(-time.Hour))

    if 500*time.Millisecond != reading.Uncertainty || 0 != reading.Offset {
        t.Fatalf("expected the date's half second alone, got %+v", reading)
    }
}
