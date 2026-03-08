package serializer

import (
    "bytes"
    "testing"
)

func TestJsonSerializer_RoundTrip_Map(t *testing.T) {
    serializer := NewJsonSerializer()

    input := map[string]any{
        "a": "b",
        "n": 1,
        "f": 1.5,
        "b": true,
    }

    data, err := serializer.Serialize(input)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    var result map[string]any
    err = serializer.Deserialize(data, &result)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "b" != result["a"].(string) {
        t.Fatalf("unexpected value")
    }
    if float64(1) != result["n"].(float64) {
        t.Fatalf("unexpected number")
    }
    if float64(1.5) != result["f"].(float64) {
        t.Fatalf("unexpected float")
    }
    if true != result["b"].(bool) {
        t.Fatalf("unexpected bool")
    }
}

func TestJsonSerializer_RoundTrip_Struct(t *testing.T) {
    type testStruct struct {
        Name string `json:"name"`
        Age  int    `json:"age"`
    }

    serializer := NewJsonSerializer()

    input := testStruct{
        Name: "a",
        Age:  10,
    }

    data, err := serializer.Serialize(input)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    var result map[string]any
    err = serializer.Deserialize(data, &result)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "a" != result["name"].(string) {
        t.Fatalf("unexpected name")
    }
    if float64(10) != result["age"].(float64) {
        t.Fatalf("unexpected age")
    }
}

func TestJsonSerializer_Deserialize_InvalidJson(t *testing.T) {
    serializer := NewJsonSerializer()

    var result map[string]any
    err := serializer.Deserialize([]byte("{invalid"), &result)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestJsonSerializer_Deserialize_NilTarget(t *testing.T) {
    serializer := NewJsonSerializer()

    err := serializer.Deserialize([]byte("{}"), nil)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestJsonSerializer_Serialize_UnsupportedType(t *testing.T) {
    serializer := NewJsonSerializer()

    ch := make(chan int)

    _, err := serializer.Serialize(ch)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestJsonSerializer_Serialize_DoesNotMutateInput(t *testing.T) {
    serializer := NewJsonSerializer()

    input := map[string]any{
        "a": "b",
    }

    _, err := serializer.Serialize(input)
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "b" != input["a"].(string) {
        t.Fatalf("input mutated")
    }
}

func TestJsonSerializer_Deserialize_EmptyPayload(t *testing.T) {
    serializer := NewJsonSerializer()

    var result map[string]any
    err := serializer.Deserialize([]byte{}, &result)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestJsonSerializer_Serialize_ProducesValidJson(t *testing.T) {
    serializer := NewJsonSerializer()

    data, err := serializer.Serialize(
        map[string]any{
            "x": "y",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if false == bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
        t.Fatalf("expected json object")
    }
}
