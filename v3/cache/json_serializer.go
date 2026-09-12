package cache

import (
    "encoding/json"

    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
)

/* NewJsonSerializer stores the value as a bare json document, with no envelope and no schema discriminant. The consequence belongs to the deployment, not to a call: a document written by one version of the application decodes cleanly in the next one, so a field added since is simply absent and the caller reads the zero value — an empty role list, a false flag — with no decoding error to notice and no version to compare. Nothing heals it either, since an entry cached with a ttl of zero never lapses. Where the shape of a cached value can change between releases, either carry a version inside the value or move the cache key with the shape; the session package writes the same hazard down on its own file storage. */
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
