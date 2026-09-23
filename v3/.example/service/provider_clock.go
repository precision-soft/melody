package service

import (
    nethttp "net/http"
    "strconv"
    "strings"
    "time"
)

/* providerDateResolution is how finely an HTTP date reads the clock that wrote it: the format carries whole
   seconds, and the clock that wrote 09:00:00 read anything from 09:00:00 to just before 09:00:01. */
const providerDateResolution = time.Second

/* providerAgeCeilingSeconds is the largest Age the http caching rules let a cache send, 2^31 seconds: a cache
   whose count grew past it sends this value instead (RFC 9111, section 1.2.2). */
const providerAgeCeilingSeconds = 2147483648

/* providerClockReading is the provider's clock read against this one, from one answer. A rate document is
   stamped on the PROVIDER's clock, and the catalogue judges readings against each other on THIS one: a stamp
   compared with a stamp is exact only while the provider's clock is never set back, and a provider whose clock
   ran ahead and was then corrected had every honest reading after the correction judged older than the one
   stamped before it. The answer says what the provider's clock read when it answered — the Date header, plus
   the Age a cache in front of it adds to an answer it kept — and this clock read the moment the answer was
   in flight, so the difference between the two is the offset a stamp is moved by onto this clock.

   The offset is measured, never exact: the provider answered somewhere between the request leaving and the
   answer arriving, and its date is read to the second. Both are carried as the uncertainty, and an offset
   inside it is no offset at all — two clocks that agree to within what one answer can tell are taken to
   agree, so the reading of a provider on a synchronised clock lands on this clock at its own stamp, exactly.
   Measured is false when the answer carried no readable date; the offset is then zero, the stamp is taken on
   this clock as it came — or at the moment the answer arrived, when it is stamped after that — and the
   judgement of order falls back to comparing the provider's stamps with each other, which is exact for every
   provider whose clock is not set back.

   The offset is trusted only within a bound (see exceeds): from one answer, a provider whose clock runs late is
   indistinguishable from a verbatim replay of an old answer that kept its Date and dropped its Age, and moved
   by an offset of any size the replay's old stamp would land at the moment it was replayed, over the newer
   reading it replays. */
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

/* readProviderClock measures the provider's clock from one answer's headers, the request having left at sentAt
   and the answer arrived at receivedAt, both on this clock. The cache's Age is honoured because a cache keeps
   the origin's Date on the answer it hands out again: without it, a document served from a cache an hour after
   the provider wrote it read as a provider whose clock runs an hour late. An Age that is not a non-negative
   integer is ignored, as the http caching rules say to. An Age is read to the second like the Date it is added
   to, so the two span two seconds rather than one, and the provider's instant is read at the middle of both.
   An Age above the largest the caching rules let a cache send, 2^31 seconds, is not an age any cache kept an
   answer for: the answer's clock is then unreadable and the answer is judged as one that carried no date,
   rather than moving its stamps by decades — or by an overflow — onto this clock. */
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
    if "" != ageValue {
        if ageSeconds, ageErr := strconv.ParseInt(ageValue, 10, 64); nil == ageErr && 0 <= ageSeconds {
            if providerAgeCeilingSeconds < ageSeconds {
                return unmeasured
            }

            providerDate = providerDate.Add(time.Duration(ageSeconds) * time.Second)
            resolution = resolution + providerDateResolution
        }
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

/* exceeds answers whether the measured offset lies beyond the bound by more than the measurement can tell: an
   offset inside the bound, or past it by less than its own uncertainty, is not known to be beyond it. */
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

/* onThisClock moves an instant stamped on the provider's clock onto this one. An answer whose clock went
   unmeasured has no offset to move the stamp by, and a stamp ahead of the moment the answer arrived is then
   taken at that moment: a reading cannot have been taken after it reached this application, and stored ahead of
   it — admitted under the skew — every honest reading of the minutes after was judged older and kept out. The
   stamp itself still names the reading, so the same document read again is the reading already held. */
func (instance providerClockReading) onThisClock(providerInstant time.Time) time.Time {
    if false == instance.Measured {
        if false == instance.ReceivedAt.IsZero() && true == providerInstant.After(instance.ReceivedAt) {
            return instance.ReceivedAt
        }

        return providerInstant
    }

    return providerInstant.Add(-instance.Offset)
}
