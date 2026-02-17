package bootstrap

import (
	nethttp "net/http"
	"strconv"
	"time"

	melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewTimingMiddleware() melodyhttpcontract.Middleware {
	return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
		return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
			startedAt := time.Now()

			response, err := next(runtimeInstance, writer, request)
			if nil != err {
				return response, err
			}

			duration := time.Since(startedAt).Milliseconds()
			if nil != response {
				response.Headers().Set("X-Example-Duration-Ms", strconv.FormatInt(duration, 10))
			}

			return response, nil
		}
	}
}
