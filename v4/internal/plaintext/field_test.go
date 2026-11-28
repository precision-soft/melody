package plaintext

import (
    "bytes"
    "encoding/json"
    "reflect"
    "testing"
    "time"
)

type fieldTestBase struct {
    Id   int    `json:"id"`
    Name string `json:"name"`
}

type fieldTestOther struct {
    Name  string `json:"name"`
    Score int
}

type fieldTestTagged struct {
    Score int `json:"Score"`
}

type fieldTestHidden struct {
    Visible string `json:"visible"`
}

type fieldTestShadowing struct {
    fieldTestBase
    *fieldTestOther
    fieldTestHidden
    Score   string
    Skipped string    `json:"-"`
    Dash    string    `json:"-,"`
    Empty   string    `json:"empty,omitempty"`
    Zero    time.Time `json:"zero,omitzero"`
    Plain   string
}

type fieldTestAmbiguous struct {
    fieldTestOther
    fieldTestTagged
}

type fieldTestLeft struct {
    Label string
    Left  int
}

type fieldTestRight struct {
    Label string
    Right int
}

type fieldTestUntaggedTie struct {
    fieldTestLeft
    fieldTestRight
}

func jsonKeys(t *testing.T, value any) []string {
    t.Helper()

    encoded, err := json.Marshal(value)
    if nil != err {
        t.Fatalf("unexpected marshal error: %v", err)
    }

    decoder := json.NewDecoder(bytes.NewReader(encoded))
    keys := []string{}
    depth := 0
    expectKey := false
    for {
        token, tokenErr := decoder.Token()
        if nil != tokenErr {
            break
        }

        switch typedToken := token.(type) {
        case json.Delim:
            if '{' == typedToken || '[' == typedToken {
                depth++
                expectKey = '{' == typedToken && 1 == depth
                continue
            }
            depth--
            expectKey = 1 == depth
            continue
        case string:
            if true == expectKey && 1 == depth {
                keys = append(keys, typedToken)
                expectKey = false
                continue
            }
        }

        expectKey = 1 == depth
    }

    return keys
}

func renderedKeys(value any) []string {
    reflected := reflect.ValueOf(value)
    keys := []string{}
    for _, field := range jsonFields(reflected.Type()) {
        fieldValue, reachable := fieldByIndex(reflected, field.index)
        if false == reachable || true == field.omitted(fieldValue) {
            continue
        }

        keys = append(keys, field.name)
    }

    return keys
}

func TestJsonFields_AnswersTheKeysEncodingJsonWrites(t *testing.T) {
    for name, value := range map[string]any{
        "shadowing with a nil embedded pointer": fieldTestShadowing{fieldTestBase: fieldTestBase{Id: 1, Name: "base"}, Score: "outer", Plain: "p"},
        "shadowing with an embedded pointer":    fieldTestShadowing{fieldTestOther: &fieldTestOther{Name: "other", Score: 3}, Empty: "set", Zero: time.Unix(1, 0)},
        "a tagged field wins a tie":             fieldTestAmbiguous{},
        "an untagged tie drops both":            fieldTestUntaggedTie{},
    } {
        expected := jsonKeys(t, value)
        actual := renderedKeys(value)

        if false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("%s: expected the keys %v encoding/json writes, got %v", name, expected, actual)
        }
    }
}

func TestRender_AStructFieldIsReadThroughItsEmbeddedStructs(t *testing.T) {
    body := Render(fieldTestShadowing{fieldTestBase: fieldTestBase{Id: 1, Name: "base"}, fieldTestOther: &fieldTestOther{Score: 3}, fieldTestHidden: fieldTestHidden{Visible: "v"}, Score: "outer", Dash: "d"}, DefaultOptions())

    if "id: 1\nvisible: v\nScore: outer\n-: d\nPlain:\n" != body {
        t.Fatalf("expected the promoted fields, the two names of one depth dropped and the outer field over the deeper one, got %q", body)
    }
}

type fieldTestThroughPointer struct {
    *fieldTestHidden
    Own string `json:"own"`
}

func TestJsonFields_AFieldBehindANilEmbeddedPointerIsLeftOut(t *testing.T) {
    for name, value := range map[string]fieldTestThroughPointer{
        "nil":     {Own: "o"},
        "not nil": {fieldTestHidden: &fieldTestHidden{Visible: "v"}, Own: "o"},
    } {
        expected := jsonKeys(t, value)
        actual := renderedKeys(value)

        if false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("%s: expected the keys %v encoding/json writes, got %v", name, expected, actual)
        }
    }

    if "own: o\n" != Render(fieldTestThroughPointer{Own: "o"}, DefaultOptions()) {
        t.Fatalf("expected the promoted field behind a nil pointer left out, got %q", Render(fieldTestThroughPointer{Own: "o"}, DefaultOptions()))
    }
}

type fieldTestShared struct {
    X int
}

type fieldTestBranchB struct {
    fieldTestShared
}

type fieldTestBranchC struct {
    fieldTestShared
}

type fieldTestDiamond struct {
    fieldTestBranchB
    fieldTestBranchC
}

type fieldTestRecursive struct {
    *fieldTestRecursive
    Value int `json:"value"`
}

type fieldTestUnexportedNumber int

type fieldTestZeroer struct {
    Set bool
}

func (instance fieldTestZeroer) IsZero() bool {
    return false == instance.Set
}

type fieldTestOmissions struct {
    fieldTestUnexportedNumber
    Pointer *int            `json:"pointer,omitempty"`
    Map     map[string]int  `json:"map,omitempty"`
    List    []int           `json:"list,omitempty"`
    Zeroer  fieldTestZeroer `json:"zeroer,omitzero"`
    Kept    int             `json:"kept"`
}

func TestJsonFields_AnswersTheKeysEncodingJsonWritesForEmbeddingAndOmission(t *testing.T) {
    seven := 7

    for name, value := range map[string]any{
        "a struct embedded twice at one depth": fieldTestDiamond{},
        "a struct that embeds itself":          fieldTestRecursive{fieldTestRecursive: &fieldTestRecursive{Value: 1}, Value: 2},
        "omitted when empty or zero":           fieldTestOmissions{fieldTestUnexportedNumber: 3},
        "written when set":                     fieldTestOmissions{Pointer: &seven, Map: map[string]int{"a": 1}, List: []int{1}, Zeroer: fieldTestZeroer{Set: true}},
    } {
        expected := jsonKeys(t, value)
        actual := renderedKeys(value)

        if false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("%s: expected the keys %v encoding/json writes, got %v", name, expected, actual)
        }
    }
}
