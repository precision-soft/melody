package otlp

import (
    "net"
    "slices"
    "strings"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

const bootWarningContextKey = "bootWarning"

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

/* journalInsecureCollector writes the warning in the runtime's journal for a provider built outside the module, honouring the boot warning ignore key of the configuration it finds; a runtime that holds no logger writes nothing */
func journalInsecureCollector(runtimeInstance containercontract.Runtime, endpoint string, config Config) {
    warning, warns := insecureCollectorWarningOf(endpoint, config)
    if false == warns {
        return
    }

    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return
    }

    configuration, configurationErr := melodyconfig.ConfigFromRuntime(runtimeInstance)
    if nil == configurationErr && nil != configuration && true == slices.Contains(configuration.Kernel().BootWarningsIgnored(), InsecureCollectorBootWarningName) {
        return
    }

    journalContext := loggingcontract.Context{bootWarningContextKey: InsecureCollectorBootWarningName}
    for key, value := range warning.Context {
        journalContext[key] = value
    }

    logger.Warning(warning.Message, journalContext)
}
