package rueidis

import (
    "net"
    "slices"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* PlaintextAddressBootWarningName is the code of the warning a client opened without ClientConfig.TlsConfig over an address off this machine writes, the code a process lists in MELODY_BOOT_WARNINGS_IGNORED to leave it out */
const PlaintextAddressBootWarningName = "rueidis.plaintextAddress"

const bootWarningContextKey = "bootWarning"

const plaintextAddressWarningMessage = "redis address is dialled in clear off this machine: the password and every value cross the network in clear; set ClientConfig.TlsConfig"

/* the hazard is the address, not the client: every client over one address names it once per process */
var judgedPlaintextAddresses sync.Map

/* plaintextAddressesOf answers the addresses off this machine a client without tls would dial in clear */
func plaintextAddressesOf(addresses []string) []string {
    plaintext := []string{}
    for _, address := range addresses {
        hostname := address
        if splitHost, _, splitErr := net.SplitHostPort(address); nil == splitErr {
            hostname = splitHost
        }

        if false == hostIsLocal(strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")) {
            plaintext = append(plaintext, address)
        }
    }

    return plaintext
}

/* hostIsLocal answers whether a host stays on this machine: localhost or a loopback address; a private address and a single-label name cross a network */
func hostIsLocal(hostname string) bool {
    if "localhost" == strings.ToLower(hostname) {
        return true
    }

    address := net.ParseIP(hostname)

    return nil != address && true == address.IsLoopback()
}

/* warnPlaintextAddresses names each address, never a credential, once per address, through the runtime's journal, honouring the boot warning ignore key of the configuration it finds */
func warnPlaintextAddresses(runtimeInstance containercontract.Runtime, addresses []string) {
    for _, address := range plaintextAddressesOf(addresses) {
        if _, alreadyJudged := judgedPlaintextAddresses.Load(address); true == alreadyJudged {
            continue
        }

        /* resolved without the emergency record LoggerFromRuntime writes for a runtime that holds no logger, since an open without one leaves the warning for the next */
        logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
        if nil != loggerErr || nil == logger {
            return
        }

        if _, alreadyJudged := judgedPlaintextAddresses.LoadOrStore(address, struct{}{}); true == alreadyJudged {
            continue
        }

        configuration, configurationErr := config.ConfigFromRuntime(runtimeInstance)
        if nil == configurationErr && nil != configuration && true == slices.Contains(configuration.Kernel().BootWarningsIgnored(), PlaintextAddressBootWarningName) {
            return
        }

        logger.Warning(plaintextAddressWarningMessage, loggingcontract.Context{bootWarningContextKey: PlaintextAddressBootWarningName, "address": address})
    }
}
