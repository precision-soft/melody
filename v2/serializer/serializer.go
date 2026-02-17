package serializer

import (
	"encoding/json"

	"github.com/precision-soft/melody/v2/exception"
	serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

func NewJsonSerializer() *JsonSerializer {
	return &JsonSerializer{
		indent: "",
	}
}

func NewPrettyJsonSerializer() *JsonSerializer {
	return &JsonSerializer{
		indent: "  ",
	}
}

type JsonSerializer struct {
	indent string
}

func (instance *JsonSerializer) Serialize(value any) ([]byte, error) {
	if "" == instance.indent {
		return json.Marshal(value)
	}

	return json.MarshalIndent(value, "", instance.indent)
}

func (instance *JsonSerializer) Deserialize(payload []byte, target any) error {
	if nil == target {
		return exception.NewError("deserialize target is nil", nil, nil)
	}

	return json.Unmarshal(payload, target)
}

func (instance *JsonSerializer) ContentType() string {
	return MimeApplicationJson + "; charset=utf-8"
}

var _ serializercontract.Serializer = (*JsonSerializer)(nil)
