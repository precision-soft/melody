package amqp

import (
    "net"
    "net/url"
    "slices"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* PlaintextDsnBootWarningName is the code of the warning a connection opened over an amqp dsn off this machine writes, the code a process lists in MELODY_BOOT_WARNINGS_IGNORED to leave it out */
const PlaintextDsnBootWarningName = "amqp.plaintextDsn"

const bootWarningContextKey = "bootWarning"

const plaintextDsnWarningMessage = "amqp dsn is plain amqp off this machine: credentials and messages cross the network in clear; use an amqps dsn, with ProviderConfig.TlsConfig for a private authority"

/* the hazard is the broker, not the connection: every connection to one host names it once per process */
var judgedPlaintextHosts sync.Map

/* plaintextHostOf answers the host of an amqp dsn off this machine, empty for an amqps dsn, a loopback host or a dsn it cannot read */
func plaintextHostOf(dsn string) string {
    parsed, parseErr := url.Parse(dsn)
    if nil != parseErr || false == strings.EqualFold("amqp", parsed.Scheme) || "" == parsed.Host {
        return ""
    }

    if true == hostIsLocal(parsed.Hostname()) {
        return ""
    }

    return parsed.Host
}

/* hostIsLocal answers whether a host stays on this machine: localhost or a loopback address; a private address and a single-label name cross a network */
func hostIsLocal(hostname string) bool {
    if "localhost" == strings.ToLower(hostname) {
        return true
    }

    address := net.ParseIP(hostname)

    return nil != address && true == address.IsLoopback()
}

/* warnPlaintextDsn names the host, never the dsn, once per host, through the runtime's journal, honouring the boot warning ignore key of the configuration it finds */
func warnPlaintextDsn(runtimeInstance containercontract.Runtime, dsn string) {
    host := plaintextHostOf(dsn)
    if "" == host {
        return
    }

    if _, alreadyJudged := judgedPlaintextHosts.Load(host); true == alreadyJudged {
        return
    }

    /* resolved without the emergency record LoggerFromRuntime writes for a runtime that holds no logger, since an open without one leaves the warning for the next */
    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return
    }

    if _, alreadyJudged := judgedPlaintextHosts.LoadOrStore(host, struct{}{}); true == alreadyJudged {
        return
    }

    configuration, configurationErr := config.ConfigFromRuntime(runtimeInstance)
    if nil == configurationErr && nil != configuration && true == slices.Contains(configuration.Kernel().BootWarningsIgnored(), PlaintextDsnBootWarningName) {
        return
    }

    logger.Warning(plaintextDsnWarningMessage, loggingcontract.Context{bootWarningContextKey: PlaintextDsnBootWarningName, "host": host})
}
