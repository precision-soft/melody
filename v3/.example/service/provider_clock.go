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
   this clock as it came, and the judgement of order falls back to comparing the provider's stamps with each
   other, which is exact for every provider whose clock is not set back. */
type providerClockReading struct {
    Measured    bool
    Offset      time.Duration
    Uncertainty time.Duration
    /* AnsweredAt is the provider's clock at the moment it answered, the latest instant a reading in that answer
       can have been taken at, on the provider's own clock. */
    AnsweredAt time.Time
}

/* readProviderClock measures the provider's clock from one answer's headers, the request having left at sentAt
   and the answer arrived at receivedAt, both on this clock. The cache's Age is honoured because a cache keeps
   the origin's Date on the answer it hands out again: without it, a document served from a cache an hour after
   the provider wrote it read as a provider whose clock runs an hour late. An Age that is not a non-negative
   integer is ignored, as the http caching rules say to. */
func readProviderClock(headers nethttp.Header, sentAt time.Time, receivedAt time.Time) providerClockReading {
    dateValue := strings.TrimSpace(headers.Get("Date"))
    if "" == dateValue {
        return providerClockReading{}
    }

    providerDate, parseErr := nethttp.ParseTime(dateValue)
    if nil != parseErr {
        return providerClockReading{}
    }

    if ageValue := strings.TrimSpace(headers.Get("Age")); "" != ageValue {
        if ageSeconds, ageErr := strconv.ParseInt(ageValue, 10, 64); nil == ageErr && 0 <= ageSeconds {
            providerDate = providerDate.Add(time.Duration(ageSeconds) * time.Second)
        }
    }

    roundTrip := receivedAt.Sub(sentAt)
    if 0 > roundTrip {
        roundTrip = 0
    }

    /* each half is read at its middle: the provider's second at half a second past what it wrote, and this
       clock at half the round trip past the request leaving */
    providerAnswer := providerDate.Add(providerDateResolution / 2)
    localAnswer := sentAt.Add(roundTrip / 2)
    uncertainty := providerDateResolution/2 + roundTrip/2

    offset := providerAnswer.Sub(localAnswer)
    if -uncertainty <= offset && offset <= uncertainty {
        offset = 0
    }

    return providerClockReading{
        Measured:    true,
        Offset:      offset,
        Uncertainty: uncertainty,
        AnsweredAt:  providerDate.Add(providerDateResolution),
    }
}

/* onThisClock moves an instant stamped on the provider's clock onto this one. */
func (instance providerClockReading) onThisClock(providerInstant time.Time) time.Time {
    return providerInstant.Add(-instance.Offset)
}
