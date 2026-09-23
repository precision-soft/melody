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

/* an Age is read to the second like the Date it is added to, so the two span two seconds: a provider that agrees
   with this clock, answering from a cache, read as an offset of a whole second when the pair was read as one */
func TestReadProviderClock_AnAgeWidensTheResolutionToTwoSeconds(t *testing.T) {
    arrived := providerClockSentAt.Add(2*time.Second + 500*time.Millisecond)
    reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", "1"), arrived, arrived)

    if false == reading.Measured || 0 != reading.Offset || time.Second != reading.Uncertainty {
        t.Fatalf("expected an agreeing clock measured to within a second, got %+v", reading)
    }

    if false == reading.AnsweredAt.Equal(time.Date(2026, time.September, 8, 9, 0, 3, 0, time.UTC)) {
        t.Errorf("expected the answer bounded by the end of both seconds, got %s", reading.AnsweredAt)
    }
}

/* an Age above the largest a cache may send is no age any cache kept an answer for: the answer's clock is
   unreadable, rather than moved by decades — or wrapped by an overflow — onto this one */
func TestReadProviderClock_AnAgeAboveTheCeilingLeavesTheClockUnmeasured(t *testing.T) {
    for _, age := range []string{"2147483649", "9999999999", "9223372036854775807"} {
        reading := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", age), providerClockSentAt, providerClockSentAt)

        if true == reading.Measured || 0 != reading.Offset || age != reading.Age {
            t.Errorf("age %q: expected an unmeasured clock naming the header, got %+v", age, reading)
        }
    }

    ceiling := readProviderClock(providerClockHeaders("Tue, 08 Sep 2026 09:00:00 GMT", "2147483648"), providerClockSentAt, providerClockSentAt)
    if false == ceiling.Measured {
        t.Errorf("expected the ceiling itself to be read as an age, got %+v", ceiling)
    }
}

/* the bound is judged against the measurement: an offset past it by less than its own uncertainty is not known to
   be past it */
func TestProviderClockReading_ExceedsOnlyBeyondItsUncertainty(t *testing.T) {
    cases := []struct {
        reading providerClockReading
        wanted  bool
    }{
        {providerClockReading{Measured: true, Offset: 5*time.Minute + 500*time.Millisecond, Uncertainty: 600 * time.Millisecond}, false},
        {providerClockReading{Measured: true, Offset: 5*time.Minute + 700*time.Millisecond, Uncertainty: 600 * time.Millisecond}, true},
        {providerClockReading{Measured: true, Offset: -5*time.Minute - 700*time.Millisecond, Uncertainty: 600 * time.Millisecond}, true},
        {providerClockReading{Measured: false, Offset: time.Hour}, false},
    }

    for _, testCase := range cases {
        if got := testCase.reading.exceeds(5 * time.Minute); testCase.wanted != got {
            t.Errorf("%+v: expected %v, got %v", testCase.reading, testCase.wanted, got)
        }
    }
}

/* an unmeasured answer's stamp is taken on this clock as it came, and no later than the moment it arrived */
func TestProviderClockReading_AnUnmeasuredStampIsTakenNoLaterThanItsArrival(t *testing.T) {
    reading := readProviderClock(providerClockHeaders("", ""), providerClockSentAt, providerClockSentAt)

    ahead := providerClockSentAt.Add(4 * time.Minute)
    if onThisClock := reading.onThisClock(ahead); false == onThisClock.Equal(providerClockSentAt) {
        t.Errorf("expected a stamp ahead of the arrival taken at the arrival, got %s", onThisClock)
    }

    behind := providerClockSentAt.Add(-4 * time.Minute)
    if onThisClock := reading.onThisClock(behind); false == onThisClock.Equal(behind) {
        t.Errorf("expected a stamp before the arrival taken as it came, got %s", onThisClock)
    }
}
