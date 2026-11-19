package cache

import (
    "encoding/json"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
)

/* NewJsonSerializer stores the value as a bare json document, with no envelope and no schema discriminant, and decodes it into the type the reader names. A field the payload lacks reads as its zero value and a key the type lacks is ignored, so a document written by one version of the application decodes in silence in the next; where a cached value's shape can change between releases, carry a version inside the value or move the key with the shape. */
func NewJsonSerializer() cachecontract.Serializer {
    return &JsonSerializer{}
}

type JsonSerializer struct{}

func (instance *JsonSerializer) Serialize(value any) ([]byte, error) {
    return json.Marshal(value)
}

func (instance *JsonSerializer) Deserialize(payload []byte, target any) error {
    return json.Unmarshal(payload, target)
}

var _ cachecontract.Serializer = (*JsonSerializer)(nil)
