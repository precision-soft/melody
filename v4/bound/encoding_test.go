package bound

import (
    "encoding/json"
    "testing"
    "time"
)

func TestUnmarshalText_ReadsTheThreeStates(t *testing.T) {
    var duration Duration

    if err := duration.UnmarshalText([]byte("unlimited")); nil != err || false == duration.IsUnlimited() {
        t.Fatalf("expected unlimited, got %s (%v)", duration, err)
    }

    if err := duration.UnmarshalText([]byte(" 250ms ")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if value, ok := duration.Value(); false == ok || 250*time.Millisecond != value {
        t.Fatalf("expected 250ms, got %s", duration)
    }

    if err := duration.UnmarshalText([]byte("")); nil != err || false == duration.IsAbsent() {
        t.Fatalf("expected the empty text to read as absent, got %s (%v)", duration, err)
    }

    var count Count

    if err := count.UnmarshalText([]byte("0")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if value, ok := count.Value(); false == ok || 0 != value {
        t.Fatalf("expected the value 0, got %s", count)
    }
}

func TestUnmarshalText_RefusesWhatDoesNotParse(t *testing.T) {
    var duration Duration
    if err := duration.UnmarshalText([]byte("30")); nil == err {
        t.Fatalf("expected a duration without a unit to be refused")
    }

    var count Count
    if err := count.UnmarshalText([]byte("Unlimited")); nil == err {
        t.Fatalf("expected a spelling other than \"unlimited\" to be refused")
    }

    if err := count.UnmarshalText([]byte("1.5")); nil == err {
        t.Fatalf("expected a fractional count to be refused")
    }
}

func TestMarshalText_RoundTrips(t *testing.T) {
    for _, limit := range []Duration{{}, UnlimitedDuration(), Of(2 * time.Minute)} {
        text, err := limit.MarshalText()
        if nil != err {
            t.Fatalf("unexpected error: %v", err)
        }

        var read Duration
        if err := read.UnmarshalText(text); nil != err || limit != read {
            t.Fatalf("expected %s to round trip through %q, got %s (%v)", limit, text, read, err)
        }
    }
}

type limitDocument struct {
    Timeout Duration `json:"timeout"`
    Size    Count    `json:"size"`
}

func TestJson_ReadsNullStringAndCountNumber(t *testing.T) {
    var document limitDocument

    if err := json.Unmarshal([]byte(`{"timeout":"unlimited","size":64}`), &document); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == document.Timeout.IsUnlimited() {
        t.Fatalf("expected an unlimited timeout, got %s", document.Timeout)
    }

    if value, ok := document.Size.Value(); false == ok || 64 != value {
        t.Fatalf("expected the size 64, got %s", document.Size)
    }

    document = limitDocument{}
    if err := json.Unmarshal([]byte(`{"timeout":null,"size":"unlimited"}`), &document); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == document.Timeout.IsAbsent() || false == document.Size.IsUnlimited() {
        t.Fatalf("expected an absent timeout and an unlimited size, got %+v", document)
    }

    document = limitDocument{}
    if err := json.Unmarshal([]byte(`{}`), &document); nil != err || false == document.Timeout.IsAbsent() || false == document.Size.IsAbsent() {
        t.Fatalf("expected a missing key to leave the limit absent, got %+v (%v)", document, err)
    }
}

func TestJson_RefusesADurationNumber(t *testing.T) {
    var document limitDocument

    if err := json.Unmarshal([]byte(`{"timeout":30}`), &document); nil == err {
        t.Fatalf("expected a bare number to be refused for a duration")
    }
}

func TestJson_WritesEachState(t *testing.T) {
    written, err := json.Marshal(limitDocument{Timeout: Of(5 * time.Second), Size: Of(8)})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"timeout":"5s","size":8}` != string(written) {
        t.Fatalf("unexpected json: %s", written)
    }

    written, err = json.Marshal(limitDocument{Size: UnlimitedCount()})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"timeout":null,"size":"unlimited"}` != string(written) {
        t.Fatalf("unexpected json: %s", written)
    }

    var read limitDocument
    if err := json.Unmarshal(written, &read); nil != err || false == read.Timeout.IsAbsent() || false == read.Size.IsUnlimited() {
        t.Fatalf("expected the written document to read back, got %+v (%v)", read, err)
    }
}

func TestJson_OmitzeroLeavesAnAbsentLimitOut(t *testing.T) {
    type omittingDocument struct {
        Timeout Duration `json:"timeout,omitzero"`
        Size    Count    `json:"size,omitzero"`
    }

    written, err := json.Marshal(omittingDocument{Size: Of(0)})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"size":0}` != string(written) {
        t.Fatalf("expected the absent timeout left out and the set zero kept, got %s", written)
    }
}
