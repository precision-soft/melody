package serializer

import (
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/plaintext"
)

func TestPlainTextSerializer_Serialize_String(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    data, err := serializer.Serialize("x")
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(data) {
        t.Fatalf("unexpected payload")
    }
}

func TestPlainTextSerializer_Serialize_Bytes(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    data, err := serializer.Serialize([]byte("x"))
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(data) {
        t.Fatalf("unexpected payload")
    }
}

func TestPlainTextSerializer_Deserialize_StringTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    var target string
    err := serializer.Deserialize([]byte("x"), &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != target {
        t.Fatalf("unexpected target")
    }
}

func TestPlainTextSerializer_Deserialize_BytesTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    var target []byte
    err := serializer.Deserialize([]byte("x"), &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }
    if "x" != string(target) {
        t.Fatalf("unexpected target")
    }
}

func TestPlainTextSerializer_Deserialize_NilTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    err := serializer.Deserialize([]byte("x"), nil)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestPlainTextSerializer_Deserialize_TypedNilTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    var stringTarget *string
    err := serializer.Deserialize([]byte("x"), stringTarget)
    if nil == err {
        t.Fatalf("expected error for a typed-nil string target")
    }

    var bytesTarget *[]byte
    err = serializer.Deserialize([]byte("x"), bytesTarget)
    if nil == err {
        t.Fatalf("expected error for a typed-nil bytes target")
    }
}

func TestPlainTextSerializer_Serialize_CopiesTheBytePayload(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    input := []byte("abc")

    data, err := serializer.Serialize(input)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    input[0] = 'x'

    if "abc" != string(data) {
        t.Fatalf("expected the serialized payload to own its bytes, got %q", string(data))
    }
}

func TestPlainTextSerializer_Deserialize_CopiesThePayloadIntoTheTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    payload := []byte("abc")

    var target []byte
    err := serializer.Deserialize(payload, &target)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    payload[0] = 'x'

    if "abc" != string(target) {
        t.Fatalf("expected the deserialized value to own its bytes, got %q", string(target))
    }
}

func TestPlainTextSerializer_Deserialize_UnsupportedTarget(t *testing.T) {
    serializer := NewPlainTextSerializer(PlainTextSerializerConfig{})

    var target int
    err := serializer.Deserialize([]byte("x"), &target)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func serializeAsPlainText(t *testing.T, config PlainTextSerializerConfig, value any) string {
    t.Helper()

    payload, err := NewPlainTextSerializer(config).Serialize(value)
    if nil != err {
        t.Fatalf("unexpected error for %#v: %v", value, err)
    }

    return string(payload)
}

func TestPlainTextSerializer_Serialize_WritesAMapAsSortedLines(t *testing.T) {
    for attempt := 0; attempt < 20; attempt++ {
        body := serializeAsPlainText(t, PlainTextSerializerConfig{}, map[string]any{"userId": 7, "name": "melody", "active": true, "zone": ""})

        if "active: true\nname: melody\nuserId: 7\nzone:\n" != body {
            t.Fatalf("expected one line per entry in key order under its own key, got %q", body)
        }
    }
}

func TestPlainTextSerializer_Serialize_WritesANestedMapIndented(t *testing.T) {
    body := serializeAsPlainText(t, PlainTextSerializerConfig{}, map[string]any{
        "order": map[string]any{"id": 7, "customer": map[string]string{"name": "ana"}},
        "total": 1.5,
    })

    if "order:\n  customer:\n    name: ana\n  id: 7\ntotal: 1.5\n" != body {
        t.Fatalf("expected a nested map indented beneath its key, got %q", body)
    }
}

func TestPlainTextSerializer_Serialize_WritesASliceAsItems(t *testing.T) {
    body := serializeAsPlainText(t, PlainTextSerializerConfig{}, []any{"one", 2, nil, map[string]any{"field": "email"}, []int{3, 4}})

    if "- one\n- 2\n-\n-\n  field: email\n-\n  - 3\n  - 4\n" != body {
        t.Fatalf("expected one - line per item, a map or a list beneath its -, got %q", body)
    }
}

type plainTextSerializerAddress struct {
    City string `json:"city"`
}

type plainTextSerializerCustomer struct {
    plainTextSerializerAddress
    Id       int                         `json:"id"`
    Name     string                      `json:"customerName"`
    Email    string                      `json:"email,omitempty"`
    Password string                      `json:"-"`
    Note     string                      `json:"note,omitempty"`
    Shipping *plainTextSerializerAddress `json:"shipping"`
    Untagged string
    internal string
}

func TestPlainTextSerializer_Serialize_WritesAStructByItsJsonNames(t *testing.T) {
    body := serializeAsPlainText(t, PlainTextSerializerConfig{}, plainTextSerializerCustomer{
        plainTextSerializerAddress: plainTextSerializerAddress{City: "cluj"},
        Id:                         7,
        Name:                       "ana",
        Password:                   "secret",
        Note:                       "fragile",
        Shipping:                   &plainTextSerializerAddress{City: "iasi"},
        Untagged:                   "kept",
        internal:                   "hidden",
    })

    if "city: cluj\nid: 7\ncustomerName: ana\nnote: fragile\nshipping:\n  city: iasi\nUntagged: kept\n" != body {
        t.Fatalf("expected the fields a json client reads, in their order, got %q", body)
    }
}

type plainTextSerializerStringer struct{}

func (instance plainTextSerializerStringer) String() string {
    return "stringer text"
}

func TestPlainTextSerializer_Serialize_KeepsAScalarOnOneLineWithoutANewline(t *testing.T) {
    for _, testCase := range []struct {
        value    any
        expected string
    }{
        {value: 42, expected: "42"},
        {value: 1.5, expected: "1.5"},
        {value: true, expected: "true"},
        {value: nil, expected: ""},
        {value: errors.New("refused"), expected: "refused"},
        {value: plainTextSerializerStringer{}, expected: "stringer text"},
        {value: time.Duration(3 * time.Second), expected: "3s"},
        {value: map[string]any{}, expected: ""},
        {value: []int{}, expected: ""},
    } {
        body := serializeAsPlainText(t, PlainTextSerializerConfig{}, testCase.value)
        if testCase.expected != body {
            t.Fatalf("expected %#v to render as %q, got %q", testCase.value, testCase.expected, body)
        }
    }
}

func TestPlainTextSerializer_Serialize_EndsAValueThatHoldsItself(t *testing.T) {
    selfReferencing := map[string]any{"name": "loop"}
    selfReferencing["self"] = selfReferencing

    rendered := make(chan string, 1)
    go func() {
        rendered <- serializeAsPlainText(t, PlainTextSerializerConfig{MaxDepth: bound.Of(3)}, selfReferencing)
    }()

    select {
    case body := <-rendered:
        if "name: loop\nself:\n  name: loop\n  self:\n    name: loop\n    self:\n      name: ...\n      self: ...\n" != body {
            t.Fatalf("expected the walk cut at the depth bound, got %q", body)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the self-referencing map rendered within the depth bound")
    }
}

func TestPlainTextSerializer_Serialize_TruncatesAtEachBound(t *testing.T) {
    items := make([]string, 10)
    for index := range items {
        items[index] = "item"
    }

    for name, testCase := range map[string]struct {
        config   PlainTextSerializerConfig
        expected string
    }{
        "lines":   {config: PlainTextSerializerConfig{MaxLines: bound.Of(3)}, expected: "- item\n- item\n- item\n...(truncated)\n"},
        "bytes":   {config: PlainTextSerializerConfig{MaxBytes: bound.Of(10)}, expected: "- item\n- item\n- it...\n...(truncated)\n"},
        "members": {config: PlainTextSerializerConfig{MaxMembers: bound.Of(2)}, expected: "- item\n- ...\n...(truncated)\n"},
        "default": {config: PlainTextSerializerConfig{}, expected: strings.Repeat("- item\n", 10)},
        "unlimited": {
            config:   PlainTextSerializerConfig{MaxLines: bound.UnlimitedCount(), MaxBytes: bound.UnlimitedCount(), MaxMembers: bound.UnlimitedCount()},
            expected: strings.Repeat("- item\n", 10),
        },
    } {
        body := serializeAsPlainText(t, testCase.config, items)
        if testCase.expected != body {
            t.Fatalf("%s: expected %q, got %q", name, testCase.expected, body)
        }
    }
}

func TestPlainTextSerializer_Serialize_ALongListStopsAtTheDefaultLineBound(t *testing.T) {
    items := make([]int, 5000)

    body := serializeAsPlainText(t, PlainTextSerializerConfig{}, items)

    if plaintext.DefaultMaxLines+1 != strings.Count(body, "\n") || false == strings.HasSuffix(body, "...(truncated)\n") {
        t.Fatalf("expected %d lines and the truncation line, got %d", plaintext.DefaultMaxLines, strings.Count(body, "\n"))
    }
}

type plainTextSerializerPanickingStringer struct{}

func (instance plainTextSerializerPanickingStringer) String() string {
    panic("stringer exploded")
}

func TestPlainTextSerializer_Serialize_APanickingStringerIsAnError(t *testing.T) {
    payload, err := NewPlainTextSerializer(PlainTextSerializerConfig{}).Serialize(map[string]any{"cause": plainTextSerializerPanickingStringer{}})

    if nil == err || nil != payload {
        t.Fatalf("expected an error and no payload, got %q and %v", payload, err)
    }

    if false == strings.Contains(err.Error(), "could not write the value") {
        t.Fatalf("expected the error to say the value could not be written, got %q", err.Error())
    }
}

func TestNewPlainTextSerializer_RefusesABoundOutsideItsRule(t *testing.T) {
    for name, testCase := range map[string]struct {
        config  PlainTextSerializerConfig
        message string
        field   string
    }{
        "depth below its minimum":   {config: PlainTextSerializerConfig{MaxDepth: bound.Of(0)}, message: "the limit is below its minimum", field: "PlainTextSerializerConfig.MaxDepth"},
        "depth unlimited":           {config: PlainTextSerializerConfig{MaxDepth: bound.UnlimitedCount()}, message: "the limit does not admit unlimited", field: "PlainTextSerializerConfig.MaxDepth"},
        "lines below its minimum":   {config: PlainTextSerializerConfig{MaxLines: bound.Of(0)}, message: "the limit is below its minimum", field: "PlainTextSerializerConfig.MaxLines"},
        "bytes below its minimum":   {config: PlainTextSerializerConfig{MaxBytes: bound.Of(0)}, message: "the limit is below its minimum", field: "PlainTextSerializerConfig.MaxBytes"},
        "members below its minimum": {config: PlainTextSerializerConfig{MaxMembers: bound.Of(0)}, message: "the limit is below its minimum", field: "PlainTextSerializerConfig.MaxMembers"},
        "depth above its maximum":   {config: PlainTextSerializerConfig{MaxDepth: bound.Of(1025)}, message: "the limit is above its maximum", field: "PlainTextSerializerConfig.MaxDepth"},
    } {
        refusal := capturePlainTextSerializerRefusal(t, func() {
            NewPlainTextSerializer(testCase.config)
        })

        if testCase.message != refusal.Message() || testCase.field != refusal.Context()["field"] {
            t.Fatalf("%s: expected %q naming %s, got %q naming %v", name, testCase.message, testCase.field, refusal.Message(), refusal.Context()["field"])
        }
    }
}

func capturePlainTextSerializerRefusal(t *testing.T, call func()) (refusal *exception.Error) {
    t.Helper()

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected a panic with a refusal, got %v", recoveredErr)
        }
    }()

    call()

    return nil
}

func TestNewPlainTextSerializer_TakesTheDefaultsAndAdmitsTheBoundsAtTheirLimits(t *testing.T) {
    if plaintext.DefaultOptions() != NewPlainTextSerializer(PlainTextSerializerConfig{}).options {
        t.Fatalf("expected the four defaults, got %+v", NewPlainTextSerializer(PlainTextSerializerConfig{}).options)
    }

    admitted := NewPlainTextSerializer(PlainTextSerializerConfig{MaxDepth: bound.Of(1024), MaxLines: bound.Of(1), MaxBytes: bound.Of(1), MaxMembers: bound.Of(1)}).options
    if 1024 != admitted.MaxDepth || 1 != admitted.MaxLines || 1 != admitted.MaxBytes || 1 != admitted.MaxMembers {
        t.Fatalf("expected the bounds at their limits admitted, got %+v", admitted)
    }
}

type plainTextSerializerFailingMarshaler struct{}

func (instance plainTextSerializerFailingMarshaler) MarshalJSON() ([]byte, error) {
    return nil, errors.New("cannot marshal")
}

func TestPlainTextSerializer_Serialize_AFailingMarshalerIsAnError(t *testing.T) {
    payload, err := NewPlainTextSerializer(PlainTextSerializerConfig{}).Serialize([]any{plainTextSerializerFailingMarshaler{}})

    if nil == err || nil != payload || false == strings.Contains(fmt.Sprint(err), "could not write the value") {
        t.Fatalf("expected an error and no payload, got %q and %v", payload, err)
    }
}
