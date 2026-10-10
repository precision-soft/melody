package cache

import (
    "encoding/json"

    cachecontract "github.com/precision-soft/melody/cache/contract"
)

/* NewJsonSerializer stores the value as a bare json document, with no envelope and no schema discriminant, so a document written by one version of the application decodes cleanly in the next, a field added since reading as its zero value. Where a cached value's shape can change between releases, carry a version inside the value or move the key with the shape. */
func NewJsonSerializer() cachecontract.Serializer {
    return &JsonSerializer{}
}

type JsonSerializer struct{}

func (instance *JsonSerializer) Serialize(value any) ([]byte, error) {
    return json.Marshal(value)
}

func (instance *JsonSerializer) Deserialize(payload []byte) (any, error) {
    var value any
    unmarshalErr := json.Unmarshal(payload, &value)
    if nil != unmarshalErr {
        return nil, unmarshalErr
    }

    return value, nil
}

var _ cachecontract.Serializer = (*JsonSerializer)(nil)
