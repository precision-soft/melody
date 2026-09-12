package serializer

import (
    "bytes"
    "strings"
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

func TestJsonSerializer_Deserialize_TypedNilTarget(t *testing.T) {
    serializer := NewJsonSerializer()

    var target *map[string]any

    err := serializer.Deserialize([]byte("{}"), target)
    if nil == err {
        t.Fatalf("expected error")
    }

    if false == strings.Contains(err.Error(), "deserialize target is nil") {
        t.Fatalf("expected the package's own nil-target refusal, got: %v", err)
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

func TestNewPrettyJsonSerializer_IndentsWhereTheCompactOneDoesNot(t *testing.T) {
    value := map[string]any{"x": "y"}

    prettyPayload, prettyErr := NewPrettyJsonSerializer().Serialize(value)
    if nil != prettyErr {
        t.Fatalf("unexpected error: %v", prettyErr)
    }

    compactPayload, compactErr := NewJsonSerializer().Serialize(value)
    if nil != compactErr {
        t.Fatalf("unexpected error: %v", compactErr)
    }

    if false == bytes.Contains(prettyPayload, []byte("\n  ")) {
        t.Fatalf("expected the pretty serializer to indent, got %q", prettyPayload)
    }

    if true == bytes.Contains(compactPayload, []byte("\n")) {
        t.Fatalf("expected the compact serializer to stay on one line, got %q", compactPayload)
    }

    if string(prettyPayload) == string(compactPayload) {
        t.Fatalf("expected the two serializers to render differently")
    }
}

func TestNewPrettyJsonSerializer_RoundTripsThroughTheCompactDeserializer(t *testing.T) {
    payload, serializeErr := NewPrettyJsonSerializer().Serialize(map[string]any{"x": "y"})
    if nil != serializeErr {
        t.Fatalf("unexpected error: %v", serializeErr)
    }

    var result map[string]any
    if err := NewJsonSerializer().Deserialize(payload, &result); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "y" != result["x"] {
        t.Fatalf("unexpected round-trip result: %v", result)
    }
}
