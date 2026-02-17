package http

import "time"

func NewHttpRequestProfile(
	requestId string,
	method string,
	path string,
	routeName string,
	routePattern string,
	statusCode int,
	startedAt time.Time,
	finishedAt time.Time,
	duration time.Duration,
) *HttpRequestProfile {
	return &HttpRequestProfile{
		requestId:    requestId,
		method:       method,
		path:         path,
		routeName:    routeName,
		routePattern: routePattern,
		statusCode:   statusCode,
		startedAt:    startedAt,
		finishedAt:   finishedAt,
		duration:     duration,
	}
}

type HttpRequestProfile struct {
	requestId    string
	method       string
	path         string
	routeName    string
	routePattern string
	statusCode   int
	startedAt    time.Time
	finishedAt   time.Time
	duration     time.Duration
}

func (instance *HttpRequestProfile) RequestId() string {
	return instance.requestId
}

func (instance *HttpRequestProfile) Method() string {
	return instance.method
}

func (instance *HttpRequestProfile) Path() string {
	return instance.path
}

func (instance *HttpRequestProfile) RouteName() string {
	return instance.routeName
}

func (instance *HttpRequestProfile) RoutePattern() string {
	return instance.routePattern
}

func (instance *HttpRequestProfile) StatusCode() int {
	return instance.statusCode
}

func (instance *HttpRequestProfile) StartedAt() time.Time {
	return instance.startedAt
}

func (instance *HttpRequestProfile) FinishedAt() time.Time {
	return instance.finishedAt
}

func (instance *HttpRequestProfile) Duration() time.Duration {
	return instance.duration
}
