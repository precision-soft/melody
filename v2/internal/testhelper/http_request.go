package testhelper

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/precision-soft/melody/v2/bag"
	bagcontract "github.com/precision-soft/melody/v2/bag/contract"
	"github.com/precision-soft/melody/v2/exception"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	"github.com/precision-soft/melody/v2/internal"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewHttpTestRequest(method string, urlString string) httpcontract.Request {
	req := httptest.NewRequest(method, urlString, nil)

	return NewHttpTestRequestFromHttpRequest(req)
}

func NewHttpTestRequestWithAccept(method string, urlString string, accept string) httpcontract.Request {
	req := httptest.NewRequest(method, urlString, nil)
	req.Header.Set("Accept", accept)

	return NewHttpTestRequestFromHttpRequest(req)
}

func NewHttpTestRequestFromHttpRequest(req *http.Request) httpcontract.Request {
	if nil == req {
		exception.Panic(
			exception.NewError("http request may not be nil", nil, nil),
		)
	}

	return &HttpTestRequest{
		httpRequestValue:   req,
		paramsValue:        map[string]string{},
		queryBagValue:      bag.NewParameterBag(),
		postBagValue:       bag.NewParameterBag(),
		attributesBagValue: bag.NewParameterBag(),
		routeNameValue:     "",
		routePatternValue:  "",
		runtimeValue:       nil,
		requestContextValue: &HttpTestRequestContext{
			requestIdValue: "test",
			startedAtValue: time.Now(),
		},
	}
}

type HttpTestRequest struct {
	httpRequestValue    *http.Request
	paramsValue         map[string]string
	queryBagValue       bagcontract.ParameterBag
	postBagValue        bagcontract.ParameterBag
	attributesBagValue  bagcontract.ParameterBag
	routeNameValue      string
	routePatternValue   string
	runtimeValue        runtimecontract.Runtime
	requestContextValue httpcontract.RequestContext
}

func (instance *HttpTestRequest) HttpRequest() *http.Request {
	return instance.httpRequestValue
}

func (instance *HttpTestRequest) Param(name string) (string, bool) {
	value, ok := instance.paramsValue[name]
	return value, ok
}

func (instance *HttpTestRequest) Params() map[string]string {
	return internal.CopyStringMap[string](instance.paramsValue)
}

func (instance *HttpTestRequest) Query() bagcontract.ParameterBag {
	return instance.queryBagValue
}

func (instance *HttpTestRequest) Post() bagcontract.ParameterBag {
	return instance.postBagValue
}

func (instance *HttpTestRequest) Attributes() bagcontract.ParameterBag {
	return instance.attributesBagValue
}

func (instance *HttpTestRequest) Header(name string) string {
	if nil == instance.httpRequestValue {
		return ""
	}

	return instance.httpRequestValue.Header.Get(name)
}

func (instance *HttpTestRequest) RouteName() string { return instance.routeNameValue }

func (instance *HttpTestRequest) RoutePattern() string { return instance.routePatternValue }

func (instance *HttpTestRequest) RuntimeInstance() runtimecontract.Runtime {
	return instance.runtimeValue
}

func (instance *HttpTestRequest) RequestContext() httpcontract.RequestContext {
	return instance.requestContextValue
}

type HttpTestRequestContext struct {
	requestIdValue string
	startedAtValue time.Time
}

func (instance *HttpTestRequestContext) RequestId() string { return instance.requestIdValue }

func (instance *HttpTestRequestContext) StartedAt() time.Time { return instance.startedAtValue }

var _ httpcontract.Request = (*HttpTestRequest)(nil)

var _ httpcontract.RequestContext = (*HttpTestRequestContext)(nil)
