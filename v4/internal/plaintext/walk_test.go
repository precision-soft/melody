package plaintext

import (
    "encoding/json"
    "errors"
    "math"
    "net"
    "strings"
    "testing"
    "time"
)

func TestLabel_SpellsACamelCasedKeyAsWords(t *testing.T) {
    cases := map[string]string{
        "requestId":        "request id",
        "validationErrors": "validation errors",
        "cause":            "cause",
    }

    for key, expected := range cases {
        if expected != Label(key) {
            t.Fatalf("expected %q for %q, got %q", expected, key, Label(key))
        }
    }
}

func TestWalk_LabelKeysSpellsEveryKeyAsWordsAndOtherwiseKeepsTheKey(t *testing.T) {
    value := map[string]any{"orderId": map[string]any{"lineCount": 2}}

    labelled := DefaultOptions()
    labelled.LabelKeys = true

    if "order id:\n  line count: 2\n" != Render(value, labelled) {
        t.Fatalf("expected every key spelled as words, got %q", Render(value, labelled))
    }

    if "orderId:\n  lineCount: 2\n" != Render(value, DefaultOptions()) {
        t.Fatalf("expected every key kept, got %q", Render(value, DefaultOptions()))
    }
}

func TestWalk_LinesCountTheLinesTheWalkStartedAfter(t *testing.T) {
    options := DefaultOptions()
    options.MaxLines = 2

    walk := NewWalk(options, []string{"400 bad request"})
    walk.Entry("first", 1)
    walk.Entry("second", 2)
    walk.Entry("third", 3)

    if "400 bad request\nfirst: 1\n...(truncated)" != strings.Join(walk.Lines(), "\n") {
        t.Fatalf("expected the starting line counted toward the bound, got %q", walk.Lines())
    }
}

func TestRender_APointerThatReachesItselfEndsAtTheDepthBound(t *testing.T) {
    var selfReferencing any
    selfReferencing = &selfReferencing

    type node struct {
        Name string `json:"name"`
        Next *node  `json:"next"`
    }
    ring := &node{Name: "ring"}
    ring.Next = ring

    for name, value := range map[string]any{"pointer": selfReferencing, "struct": ring} {
        rendered := make(chan string, 1)
        go func() {
            rendered <- Render(value, DefaultOptions())
        }()

        select {
        case body := <-rendered:
            if false == strings.Contains(body, "...") || DefaultMaxDepth*2+2 < strings.Count(body, "\n") {
                t.Fatalf("%s: expected the walk cut at the depth bound, got %q", name, body)
            }
        case <-time.After(5 * time.Second):
            t.Fatalf("%s: expected the value rendered within the depth bound", name)
        }
    }
}

func TestRender_ANilPointerAndANilInterfaceAreWrittenAsNothing(t *testing.T) {
    var typedNil *int
    var nilError error

    if "" != Render(typedNil, DefaultOptions()) {
        t.Fatalf("expected a nil pointer written as nothing, got %q", Render(typedNil, DefaultOptions()))
    }

    body := Render(map[string]any{"pointer": typedNil, "error": nilError, "list": []any{nil}}, DefaultOptions())
    if "error:\nlist:\n  -\npointer:\n" != body {
        t.Fatalf("expected a nil member written as its bare label or its bare -, got %q", body)
    }
}

func TestRender_APointerIsWrittenAsTheValueItHolds(t *testing.T) {
    count := 7

    if "count: 7\n" != Render(map[string]any{"count": &count}, DefaultOptions()) {
        t.Fatalf("expected the pointer's value, got %q", Render(map[string]any{"count": &count}, DefaultOptions()))
    }
}

func TestRender_AMapKeyIsSpelledByItsTextAndAPointerKeyByItsAddress(t *testing.T) {
    body := Render(map[int]string{10: "ten", 2: "two"}, DefaultOptions())
    if "10: ten\n2: two\n" != body {
        t.Fatalf("expected the keys in the order of their text, got %q", body)
    }

    holder := map[any]any{}
    key := &holder
    holder[key] = holder

    rendered := make(chan string, 1)
    go func() {
        rendered <- Render(holder, DefaultOptions())
    }()

    select {
    case body := <-rendered:
        if false == strings.HasPrefix(body, "0x") {
            t.Fatalf("expected a pointer key spelled by its address, got %q", body)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("expected a pointer key that reaches its own map rendered within the bound")
    }
}

func TestRender_AByteSequenceInsideAValueIsWrittenAsItsNumbers(t *testing.T) {
    if "raw: [114 97 119]\n" != Render(map[string]any{"raw": []byte("raw")}, DefaultOptions()) {
        t.Fatalf("expected the byte sequence as one value, got %q", Render(map[string]any{"raw": []byte("raw")}, DefaultOptions()))
    }
}

func TestRender_AControlCharacterInAKeyOrAValueCannotWriteALineOfItsOwn(t *testing.T) {
    body := Render(map[string]any{"note": "first\nrequest id: forged", "key\nforged": []string{"item\r\nforged: yes"}}, DefaultOptions())

    if "key\\nforged:\n  - item\\r\\nforged: yes\nnote: first\\nrequest id: forged\n" != body {
        t.Fatalf("expected every control character spelled as its escape, got %q", body)
    }
}

func TestRender_AScalarOnItsOwnIsWrittenAsItsText(t *testing.T) {
    if "line one\nline two" != Render(errors.New("line one\nline two"), DefaultOptions()) {
        t.Fatalf("expected the text of a value written on its own kept, got %q", Render(errors.New("line one\nline two"), DefaultOptions()))
    }
}

type walkTestCountingStringer struct {
    calls *int
}

func (instance walkTestCountingStringer) String() string {
    *instance.calls++

    return "item"
}

func TestWalk_StopsWalkingAtTheLineBound(t *testing.T) {
    calls := 0
    items := make([]walkTestCountingStringer, 100000)
    for index := range items {
        items[index] = walkTestCountingStringer{calls: &calls}
    }

    options := Options{MaxDepth: DefaultMaxDepth, MaxLines: 3, MaxBytes: math.MaxInt, MaxMembers: math.MaxInt}

    if "- item\n- item\n- item\n...(truncated)\n" != Render(items, options) || 4 < calls {
        t.Fatalf("expected the walk to stop after the line bound, got %d String calls", calls)
    }
}

func TestWalk_EveryEntryCountsAsAMember(t *testing.T) {
    options := Options{MaxDepth: DefaultMaxDepth, MaxLines: math.MaxInt, MaxBytes: math.MaxInt, MaxMembers: 2}

    body := Render(map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"}, options)
    if "a: 1\n...: ...\n...(truncated)\n" != body {
        t.Fatalf("expected the walk ended once two entries were spent, got %q", body)
    }
}

func TestWalk_CutsATextOnARuneBoundary(t *testing.T) {
    options := Options{MaxDepth: DefaultMaxDepth, MaxLines: math.MaxInt, MaxBytes: 3, MaxMembers: math.MaxInt}

    body := Render([]string{"ăîșț"}, options)
    if "- ă...\n...(truncated)\n" != body {
        t.Fatalf("expected the text cut before the rune the bound falls in, got %q", body)
    }
}

type walkTestErrorCode string

func (instance walkTestErrorCode) Error() string {
    return "code " + string(instance)
}

func TestRender_AStringTypeWithAMethodIsWrittenByTheMethod(t *testing.T) {
    if "code: code E1\n" != Render(map[string]any{"code": walkTestErrorCode("E1")}, DefaultOptions()) {
        t.Fatalf("expected the Error method read before the string, got %q", Render(map[string]any{"code": walkTestErrorCode("E1")}, DefaultOptions()))
    }
}

type walkTestRedacted struct {
    Name   string
    Secret string
}

func (instance walkTestRedacted) MarshalJSON() ([]byte, error) {
    return json.Marshal(map[string]any{"name": instance.Name, "count": 7})
}

type walkTestFailingMarshaler struct{}

func (instance walkTestFailingMarshaler) MarshalJSON() ([]byte, error) {
    return nil, errors.New("cannot marshal")
}

func TestRender_AValueThatWritesItsOwnJsonIsWrittenByThatJson(t *testing.T) {
    moment := time.Date(2026, 11, 11, 10, 30, 0, 0, time.UTC)

    body := Render(map[string]any{"user": walkTestRedacted{Name: "ana", Secret: "hunter2"}, "at": moment, "ip": net.ParseIP("10.0.0.1")}, DefaultOptions())

    if "at: 2026-11-11T10:30:00Z\nip: 10.0.0.1\nuser:\n  count: 7\n  name: ana\n" != body {
        t.Fatalf("expected the json a MarshalJSON writes, the text a MarshalText writes, got %q", body)
    }
}

func TestRender_AMarshalerThatFailsPanicsWithItsError(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "cannot marshal") {
            t.Fatalf("expected a panic with the marshaler's error, got %v", recoveredErr)
        }
    }()

    Render(map[string]any{"value": walkTestFailingMarshaler{}}, DefaultOptions())
}

func TestRender_APointerIsNoLevelOfItsOwn(t *testing.T) {
    options := DefaultOptions()
    options.MaxDepth = 1

    type pair struct {
        Left  int `json:"left"`
        Right int `json:"right"`
    }

    if "left: 1\nright: 2\n" != Render(&pair{Left: 1, Right: 2}, options) {
        t.Fatalf("expected the struct a pointer holds written at the depth of the pointer, got %q", Render(&pair{Left: 1, Right: 2}, options))
    }
}

func TestRender_TwoKeysOfOneTextKeepTheOrderOfTheirTypes(t *testing.T) {
    for attempt := 0; attempt < 50; attempt++ {
        body := Render(map[any]any{1: "a", "1": "b"}, DefaultOptions())
        if "1: a\n1: b\n" != body {
            t.Fatalf("expected the int key before the string key, got %q", body)
        }
    }
}

type walkTestValueStringer struct {
    name string
}

func (instance walkTestValueStringer) String() string {
    return instance.name
}

func TestRender_APointerKeyIsSpelledByItsAddressWithoutItsMethods(t *testing.T) {
    var nilKey *walkTestValueStringer

    if "0x0: v\n" != Render(map[*walkTestValueStringer]string{nilKey: "v"}, DefaultOptions()) {
        t.Fatalf("expected the nil pointer key spelled by its address, got %q", Render(map[*walkTestValueStringer]string{nilKey: "v"}, DefaultOptions()))
    }

    if "stringer key: v\n" != Render(map[any]string{walkTestValueStringer{name: "stringer key"}: "v"}, DefaultOptions()) {
        t.Fatalf("expected a key held in an interface spelled by its String, got %q", Render(map[any]string{walkTestValueStringer{name: "stringer key"}: "v"}, DefaultOptions()))
    }
}

func TestWalk_AScalarItemCountsAsOneMemberAsAStringItemDoes(t *testing.T) {
    options := Options{MaxDepth: DefaultMaxDepth, MaxLines: math.MaxInt, MaxBytes: math.MaxInt, MaxMembers: 3}

    numbers := Render([]int{1, 2, 3, 4}, options)
    texts := Render([]string{"1", "2", "3", "4"}, options)

    if "- 1\n- 2\n- ...\n...(truncated)\n" != numbers || numbers != texts {
        t.Fatalf("expected numbers and texts spent alike, got %q and %q", numbers, texts)
    }
}

func TestRender_AScalarOnItsOwnIsWrittenWholeWhateverTheBytes(t *testing.T) {
    options := DefaultOptions()
    options.MaxBytes = 4

    if "a long message" != Render(errors.New("a long message"), options) {
        t.Fatalf("expected the whole text, got %q", Render(errors.New("a long message"), options))
    }
}

func TestRender_AListItemAtTheDepthBoundIsAnEllipsisWhenItHoldsValues(t *testing.T) {
    options := DefaultOptions()
    options.MaxDepth = 1

    if "-\n  - ...\n  - 2\n" != Render([]any{[]any{[]int{1}, 2}}, options) {
        t.Fatalf("expected the list at the bound cut and the scalar written, got %q", Render([]any{[]any{[]int{1}, 2}}, options))
    }
}

type walkTestTextOnly struct{}

func (instance walkTestTextOnly) MarshalText() ([]byte, error) {
    return []byte("text form"), nil
}

type walkTestJsonError struct{}

func (instance walkTestJsonError) Error() string {
    return "the message"
}

func (instance walkTestJsonError) MarshalJSON() ([]byte, error) {
    return []byte(`{"hidden":true}`), nil
}

func TestRender_AnErrorIsWrittenByItsMessageAndATextMarshalerByItsText(t *testing.T) {
    body := Render(map[string]any{"cause": walkTestJsonError{}, "value": walkTestTextOnly{}}, DefaultOptions())

    if "cause: the message\nvalue: text form\n" != body {
        t.Fatalf("expected the error's message before its json and the text a MarshalText writes, got %q", body)
    }
}
