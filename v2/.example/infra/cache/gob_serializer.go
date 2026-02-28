package cache

import (
    "bytes"
    "encoding/gob"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
)

func NewGobSerializer() melodycachecontract.Serializer {
    gob.Register(&entity.Product{})
    gob.Register([]*entity.Product{})

    gob.Register(&entity.Category{})
    gob.Register([]*entity.Category{})

    gob.Register(&entity.Currency{})
    gob.Register([]*entity.Currency{})

    gob.Register(&entity.User{})
    gob.Register([]*entity.User{})

    return &gobSerializer{}
}

type gobSerializer struct{}

func (instance *gobSerializer) Serialize(value any) ([]byte, error) {
    buffer := &bytes.Buffer{}
    encoder := gob.NewEncoder(buffer)

    var interfaceValue = value

    encodeErr := encoder.Encode(&interfaceValue)
    if nil != encodeErr {
        return nil, encodeErr
    }

    return buffer.Bytes(), nil
}

func (instance *gobSerializer) Deserialize(payload []byte) (any, error) {
    buffer := bytes.NewBuffer(payload)
    decoder := gob.NewDecoder(buffer)

    var value any
    decodeErr := decoder.Decode(&value)
    if nil != decodeErr {
        return nil, decodeErr
    }

    return value, nil
}

var _ melodycachecontract.Serializer = (*gobSerializer)(nil)
