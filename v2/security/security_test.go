package security

import (
	"net/http/httptest"
	"time"

	"github.com/precision-soft/melody/v2/http"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type securityTestRequestContext struct {
	requestIdValue string
	startedAtValue time.Time
}

func (instance *securityTestRequestContext) RequestId() string {
	return instance.requestIdValue
}

func (instance *securityTestRequestContext) StartedAt() time.Time {
	return instance.startedAtValue
}

func newSecurityTestRequest(method string, path string, headers map[string]string, runtimeInstance runtimecontract.Runtime) httpcontract.Request {
	req := httptest.NewRequest(method, "http://example.com"+path, nil)

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return http.NewRequest(
		req,
		nil,
		runtimeInstance,
		&securityTestRequestContext{
			requestIdValue: "test",
			startedAtValue: time.Now(),
		},
	)
}
