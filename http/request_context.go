package http

import (
    "time"

    httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewRequestContext(requestId string, startedAt time.Time) *RequestContext {
    return &RequestContext{
        requestId: requestId,
        startedAt: startedAt,
    }
}

type RequestContext struct {
    requestId string
    startedAt time.Time
}

func (instance *RequestContext) RequestId() string {
    return instance.requestId
}

func (instance *RequestContext) StartedAt() time.Time {
    return instance.startedAt
}

var _ httpcontract.RequestContext = (*RequestContext)(nil)
