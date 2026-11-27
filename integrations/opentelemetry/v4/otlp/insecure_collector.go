package otlp

import (
    "net"
    "strings"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* InsecureCollectorBootWarningName is the code of the warning a module exporting without transport security to a collector off this machine writes, the code a process lists in MELODY_BOOT_WARNINGS_IGNORED to leave it out */
const InsecureCollectorBootWarningName = "opentelemetry.otlp.insecureCollector"

const insecureCollectorWarningMessage = "otlp collector is reached without transport security off this machine: spans, their attributes and Config.Headers, an authentication token among them, cross the network in clear; leave Config.Insecure unset for a collector behind tls"

/* collectorIsLocal answers whether an endpoint stays on this machine: localhost or a loopback address. Every other host, a private address and a single-label name included, crosses a network. */
func collectorIsLocal(endpoint string) bool {
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

/* insecureCollectorWarningOf names an insecure export off this machine; false when there is nothing to warn of */
func insecureCollectorWarningOf(endpoint string, config Config) (applicationcontract.BootWarning, bool) {
    if false == config.Insecure || "" == endpoint || true == collectorIsLocal(endpoint) {
        return applicationcontract.BootWarning{}, false
    }

    return applicationcontract.BootWarning{
        Name:    InsecureCollectorBootWarningName,
        Message: insecureCollectorWarningMessage,
        Context: loggingcontract.Context{"endpoint": endpoint},
    }, true
}
