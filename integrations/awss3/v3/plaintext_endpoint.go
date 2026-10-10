package awss3

import (
    "net"
    "strings"
    "sync"

    "github.com/minio/minio-go/v7"

    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* plaintextEndpointBootWarningName names the warning in the journal under the bootWarning key, the shape every boot warning of the framework carries */
const plaintextEndpointBootWarningName = "awss3.plaintextEndpoint"

const bootWarningContextKey = "bootWarning"

/* endpointIsLocal answers whether a plaintext endpoint stays on this machine: localhost or a loopback address. Every other host, a private address and a single-label name included, crosses a network, where credentials and objects travel in clear. */
func endpointIsLocal(endpoint string) bool {
    hostname := endpoint
    if splitHost, _, splitErr := net.SplitHostPort(endpoint); nil == splitErr {
        hostname = splitHost
    }

    hostname = strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")

    if "localhost" == strings.ToLower(hostname) {
        return true
    }

    address := net.ParseIP(hostname)

    return nil != address && true == address.IsLoopback()
}

/* plaintextEndpointWarning is the warning a storage over a plaintext endpoint off this machine writes once, at its first use with a journal in reach: the storage is built by the application, outside the framework's boot, so its first use is the first moment a configured journal is in reach of it. */
type plaintextEndpointWarning struct {
    mutex    sync.Mutex
    written  bool
    endpoint string
}

/* plaintextEndpointWarningOf reads the endpoint of the client itself, so a client the application built without NewClient is judged too; nil when there is nothing to warn of */
func plaintextEndpointWarningOf(client *minio.Client) *plaintextEndpointWarning {
    endpointUrl := client.EndpointURL()
    if nil == endpointUrl || "http" != endpointUrl.Scheme || true == endpointIsLocal(endpointUrl.Host) {
        return nil
    }

    return &plaintextEndpointWarning{endpoint: endpointUrl.Host}
}

/* write writes the warning the first time a logger is in reach, naming the endpoint's host and never a credential */
func (instance *plaintextEndpointWarning) write(runtimeInstance runtimecontract.Runtime) {
    if nil == instance {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.written {
        return
    }

    /* resolved without the emergency record LoggerFromRuntime writes for a runtime that holds no logger, since a use without one only leaves the warning for the next */
    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return
    }

    instance.written = true

    logger.Warning(
        "object storage endpoint is plain http off this machine: credentials and objects cross the network in clear and every presigned url is http; set Secure on the configuration",
        loggingcontract.Context{
            "endpoint":            instance.endpoint,
            bootWarningContextKey: plaintextEndpointBootWarningName,
        },
    )
}
