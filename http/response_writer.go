package http

import (
	"io"
	nethttp "net/http"

	"github.com/precision-soft/melody/exception"
	httpcontract "github.com/precision-soft/melody/http/contract"
	"github.com/precision-soft/melody/logging"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func WriteToHttpResponseWriter(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	responseWriter nethttp.ResponseWriter,
	response httpcontract.Response,
) error {
	if nil == response {
		return nil
	}

	headers := response.Headers()
	if nil != headers {
		for key, values := range headers {
			for _, value := range values {
				responseWriter.Header().Add(key, value)
			}
		}
	}

	statusCode := response.StatusCode()
	if 0 == statusCode {
		statusCode = nethttp.StatusOK
	}

	responseWriter.WriteHeader(statusCode)

	bodyReader := response.BodyReader()
	if nil == bodyReader {
		return nil
	}

	if closer, ok := bodyReader.(io.Closer); true == ok {
		defer func(closer io.Closer) {
			err := closer.Close()
			if nil != err {
				logger := logging.LoggerFromRuntime(runtimeInstance)
				if nil != logger {
					logger.Error(
						"failed to close response body reader",
						exception.LogContext(err),
					)
				}
			}
		}(closer)
	}

	if nil != request && nil != request.HttpRequest() && nethttp.MethodHead == request.HttpRequest().Method {
		return nil
	}

	_, err := io.Copy(responseWriter, bodyReader)
	if nil != err {
		return err
	}

	return nil
}
