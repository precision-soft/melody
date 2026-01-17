package cache

import (
	"encoding/json"

	cachecontract "github.com/precision-soft/melody/cache/contract"
)

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
