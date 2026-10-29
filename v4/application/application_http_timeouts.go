package application

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type httpServerLimits struct {
    readTimeout       time.Duration
    readHeaderTimeout time.Duration
    writeTimeout      time.Duration
    idleTimeout       time.Duration
    maxHeaderBytes    int
}

/* the key a deployment set wins, then the application's field, then the rule's default */
func resolveHttpServerLimits(configuration configcontract.Configuration, applicationConfig ApplicationConfig) (httpServerLimits, error) {
    httpConfiguration := configuration.Http()

    readTimeout, readTimeoutErr := resolveApplicationLimit(httpConfiguration.ReadTimeout(), applicationConfig.ReadTimeout, config.HttpReadTimeoutRule)
    if nil != readTimeoutErr {
        return httpServerLimits{}, readTimeoutErr
    }

    readHeaderTimeout, readHeaderTimeoutErr := resolveApplicationLimit(httpConfiguration.ReadHeaderTimeout(), applicationConfig.ReadHeaderTimeout, config.HttpReadHeaderTimeoutRule)
    if nil != readHeaderTimeoutErr {
        return httpServerLimits{}, readHeaderTimeoutErr
    }

    writeTimeout, writeTimeoutErr := resolveApplicationLimit(httpConfiguration.WriteTimeout(), applicationConfig.WriteTimeout, config.HttpWriteTimeoutRule)
    if nil != writeTimeoutErr {
        return httpServerLimits{}, writeTimeoutErr
    }

    idleTimeout, idleTimeoutErr := resolveApplicationLimit(httpConfiguration.IdleTimeout(), applicationConfig.IdleTimeout, config.HttpIdleTimeoutRule)
    if nil != idleTimeoutErr {
        return httpServerLimits{}, idleTimeoutErr
    }

    maxHeaderBytes, maxHeaderBytesErr := resolveApplicationLimit(httpConfiguration.MaxHeaderBytes(), applicationConfig.MaxHeaderBytes, config.HttpMaxHeaderBytesRule)
    if nil != maxHeaderBytesErr {
        return httpServerLimits{}, maxHeaderBytesErr
    }

    return httpServerLimits{
        readTimeout:       readTimeout.Value,
        readHeaderTimeout: readHeaderTimeout.Value,
        writeTimeout:      writeTimeout.Value,
        idleTimeout:       idleTimeout.Value,
        maxHeaderBytes:    maxHeaderBytes.Value,
    }, nil
}

func resolveTeardownTimeout(configuration configcontract.Configuration, applicationConfig ApplicationConfig) (bound.Resolved[time.Duration], error) {
    return resolveApplicationLimit(configuration.Kernel().TeardownTimeout(), applicationConfig.TeardownTimeout, config.TeardownTimeoutRule)
}

func resolveApplicationLimit[T bound.Unit](keyLimit bound.Limit[T], fieldLimit bound.Limit[T], rule bound.Rule[T]) (bound.Resolved[T], error) {
    if false == keyLimit.IsAbsent() {
        return bound.Resolve(keyLimit, rule)
    }

    return bound.Resolve(fieldLimit, rule)
}

/* net/http arms the write timeout once, from the request line, so a streaming handler re-arms it per frame through the event-stream writer's write budget, and a client that stops reading then holds its connection until the socket buffers fill unless the handler bounds the stream */
func applyHttpServerTimeouts(httpServer *nethttp.Server, limits httpServerLimits) {
    httpServer.ReadTimeout = limits.readTimeout
    httpServer.ReadHeaderTimeout = limits.readHeaderTimeout
    httpServer.WriteTimeout = limits.writeTimeout
    httpServer.IdleTimeout = limits.idleTimeout
    httpServer.MaxHeaderBytes = limits.maxHeaderBytes
}

/* applyHttpServerErrorLog routes what net/http reports on its own, a connection failing before a request exists or a request rejected before any handler, into the application's journal instead of unstructured stderr. */
func applyHttpServerErrorLog(httpServer *nethttp.Server, logger loggingcontract.Logger) {
    httpServer.ErrorLog = logging.NewStandardErrorLogger(logger, "http server error")
}
