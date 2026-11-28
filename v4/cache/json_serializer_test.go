package cache

import (
    "reflect"
    "testing"
    "time"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/serializer"
)

var _ cachecontract.Serializer = serializer.NewJsonSerializer()

type jsonSerializerTestRecord struct {
    Name      string
    Tags      []string
    Big       int64
    CreatedAt time.Time
}

func TestJsonSerializer_DecodesIntoTheTypeAsked(t *testing.T) {
    jsonSerializer := NewJsonSerializer()

    stored := jsonSerializerTestRecord{
        Name:      "a",
        Tags:      []string{"x", "y"},
        Big:       1<<53 + 1,
        CreatedAt: time.Date(2026, 11, 18, 22, 30, 0, 123, time.UTC),
    }

    payload, serializeErr := jsonSerializer.Serialize(stored)
    if nil != serializeErr {
        t.Fatalf("serialize error: %v", serializeErr)
    }

    var typed jsonSerializerTestRecord
    if deserializeErr := jsonSerializer.Deserialize(payload, &typed); nil != deserializeErr {
        t.Fatalf("deserialize error: %v", deserializeErr)
    }

    if false == reflect.DeepEqual(stored, typed) {
        t.Fatalf("expected the record read back equal, got %#v", typed)
    }

    var generic any
    if deserializeErr := jsonSerializer.Deserialize(payload, &generic); nil != deserializeErr {
        t.Fatalf("deserialize into any error: %v", deserializeErr)
    }

    if _, isMap := generic.(map[string]any); false == isMap {
        t.Fatalf("expected the generic read to answer a map, got %T", generic)
    }
}

func TestJsonSerializer_APayloadOfAnotherShapeIsAnError(t *testing.T) {
    jsonSerializer := NewJsonSerializer()

    var record jsonSerializerTestRecord
    if deserializeErr := jsonSerializer.Deserialize([]byte(`"text"`), &record); nil == deserializeErr {
        t.Fatalf("expected a string payload refused by a struct target")
    }

    if false == reflect.DeepEqual(jsonSerializerTestRecord{}, record) {
        t.Fatalf("expected the target to keep its zero value, got %#v", record)
    }

    var number int
    if deserializeErr := jsonSerializer.Deserialize([]byte(`{}`), &number); nil == deserializeErr {
        t.Fatalf("expected an object payload refused by an int target")
    }

    if 0 != number {
        t.Fatalf("expected the target to keep its zero value, got %d", number)
    }
}
