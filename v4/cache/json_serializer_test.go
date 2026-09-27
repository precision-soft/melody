package cache

import (
    "testing"
)

func TestJsonSerializer_RoundTrip(t *testing.T) {
    serializer := NewJsonSerializer()

    payload, serializeErr := serializer.Serialize(
        map[string]any{
            "a": "b",
            "n": float64(1),
        },
    )
    if nil != serializeErr {
        t.Fatalf("serialize error: %v", serializeErr)
    }

    value, deserializeErr := serializer.Deserialize(payload)
    if nil != deserializeErr {
        t.Fatalf("deserialize error: %v", deserializeErr)
    }

    decoded := value.(map[string]any)

    if "b" != decoded["a"].(string) {
        t.Fatalf("unexpected decoded value")
    }
    if float64(1) != decoded["n"].(float64) {
        t.Fatalf("unexpected decoded number")
    }
}
