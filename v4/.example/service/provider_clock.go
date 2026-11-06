package service

import (
    nethttp "net/http"
    "strconv"
    "strings"
    "time"
)

/* providerDateResolution is how finely an HTTP date reads the clock that wrote it: whole seconds. */
const providerDateResolution = time.Second

/* providerAgeCeilingSeconds is the largest Age a cache may send, 2^31 seconds (RFC 9111, section 1.2.2). */
const providerAgeCeilingSeconds = 2147483648

/* providerClockReading is the provider's clock read against this one, from one answer. A rate document is stamped on the provider's clock and the catalogue orders readings on this one, so a provider whose clock is set back does not have its honest readings judged older. The offset is the answer's Date plus Age against the moment the answer was in flight, carried with its uncertainty, and an offset inside the uncertainty is zero. The flag is false when the answer carried no readable date: the stamp is then taken as it came, or at the arrival when it lies after it. The offset is trusted only within a bound (see exceeds), because from one answer a late clock cannot be told from a replay that kept its Date; inside the bound a replay is at most five minutes old and the next honest reading replaces it. */
type providerClockReading struct {
    Measured    bool
    Offset      time.Duration
    Uncertainty time.Duration
    /* AnsweredAt is the provider's clock at the moment it answered, the latest instant a reading in that answer
       can have been taken at, on the provider's own clock. */
    AnsweredAt time.Time
    /* ReceivedAt is this clock when the answer arrived, the latest instant on THIS clock a reading in the answer
       can have been taken at. */
    ReceivedAt time.Time
    /* Date and Age are the two headers as the answer carried them, kept for the refusal that names them. */
    Date string
    Age  string
}

/* readProviderClock measures the provider's clock from one answer's headers, the request having left at sentAt and the answer arrived at receivedAt, both on this clock. The Age is honoured because a cache keeps the origin's Date on an answer it hands out again; an Age that is not a non-negative integer is ignored, and one at or past the ceiling makes the answer's clock unreadable, judged as an answer that carried no date. The provider's instant is read at the middle of the span Date and Age leave open: one second for a Date alone, two with an Age. */
func readProviderClock(headers nethttp.Header, sentAt time.Time, receivedAt time.Time) providerClockReading {
    dateValue := strings.TrimSpace(headers.Get("Date"))
    ageValue := strings.TrimSpace(headers.Get("Age"))
    unmeasured := providerClockReading{ReceivedAt: receivedAt, Date: dateValue, Age: ageValue}

    if "" == dateValue {
        return unmeasured
    }

    providerDate, parseErr := nethttp.ParseTime(dateValue)
    if nil != parseErr {
        return unmeasured
    }

    resolution := providerDateResolution
    switch ageSeconds, ageReading := providerAgeSeconds(ageValue); ageReading {
    case providerAgeOverflowed:
        return unmeasured
    case providerAgeRead:
        providerDate = providerDate.Add(time.Duration(ageSeconds) * time.Second)
        resolution = resolution + providerDateResolution
    }

    roundTrip := receivedAt.Sub(sentAt)
    if 0 > roundTrip {
        roundTrip = 0
    }

    /* each half is read at its middle: the provider's instant at half its resolution past what it wrote, and
       this clock at half the round trip past the request leaving */
    providerAnswer := providerDate.Add(resolution / 2)
    localAnswer := sentAt.Add(roundTrip / 2)
    uncertainty := resolution/2 + roundTrip/2

    offset := providerAnswer.Sub(localAnswer)
    if -uncertainty <= offset && offset <= uncertainty {
        offset = 0
    }

    return providerClockReading{
        Measured:    true,
        Offset:      offset,
        Uncertainty: uncertainty,
        AnsweredAt:  providerDate.Add(resolution),
        ReceivedAt:  receivedAt,
        Date:        dateValue,
        Age:         ageValue,
    }
}

/* exceeds answers whether the offset lies beyond the bound by more than its own uncertainty: an offset inside the bound, or past it by less than that, is not known to be beyond it. */
func (instance providerClockReading) exceeds(bound time.Duration) bool {
    if false == instance.Measured {
        return false
    }

    magnitude := instance.Offset
    if 0 > magnitude {
        magnitude = -magnitude
    }

    return magnitude-instance.Uncertainty > bound
}

/* onThisClock moves an instant stamped on the provider's clock onto this one. When the answer carried no readable date, a stamp ahead of the answer's arrival is taken at the arrival, since a reading cannot be taken after it reached this application; the stamp still names the reading. */
func (instance providerClockReading) onThisClock(providerInstant time.Time) time.Time {
    if false == instance.Measured {
        if false == instance.ReceivedAt.IsZero() && true == providerInstant.After(instance.ReceivedAt) {
            return instance.ReceivedAt
        }

        return providerInstant
    }

    return providerInstant.Add(-instance.Offset)
}

/* providerAgeReading is what an Age header says about the answer's age */
type providerAgeReading int

const (
    /* providerAgeAbsent: no Age, or one that is not delta-seconds; the Date is read alone */
    providerAgeAbsent providerAgeReading = iota
    /* providerAgeRead: an age this clock adds to the Date */
    providerAgeRead
    /* providerAgeOverflowed: a count at the ceiling or past it — older than a cache can say */
    providerAgeOverflowed
)

/* providerAgeSeconds reads an Age as delta-seconds, digits and nothing else; a sign or any other character is ignored like a missing Age. A count at the ceiling or past it, whatever its length, is one overflow verdict. */
func providerAgeSeconds(ageValue string) (int64, providerAgeReading) {
    if "" == ageValue {
        return 0, providerAgeAbsent
    }

    for _, character := range ageValue {
        if '0' > character || '9' < character {
            return 0, providerAgeAbsent
        }
    }

    ageSeconds, parseErr := strconv.ParseInt(ageValue, 10, 64)
    if nil != parseErr || providerAgeCeilingSeconds <= ageSeconds {
        return 0, providerAgeOverflowed
    }

    return ageSeconds, providerAgeRead
}
