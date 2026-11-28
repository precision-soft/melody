package rueidis

import (
    "net"
    "slices"
    "strings"
    "sync"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* PlaintextAddressBootWarningName is the code of the warning a client opened without ClientConfig.TlsConfig over an address off this machine writes, the code a process lists in MELODY_BOOT_WARNINGS_IGNORED to leave it out */
const PlaintextAddressBootWarningName = "rueidis.plaintextAddress"

const plaintextAddressWarningMessage = "redis address is dialled in clear off this machine: the password and every value cross the network in clear; set ClientConfig.TlsConfig"

/* plaintextAddressJudge holds the addresses a provider has named: the hazard is the address, not the client, so every client of one provider over an address names it once; it lives on the provider, so two applications in one process judge apart */
type plaintextAddressJudge struct {
    judged sync.Map
}

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

/* judge names each address, never a credential, once per address, through the runtime's journal, honouring the boot warning ignore key of the configuration it finds; a runtime that holds no logger leaves every address for the next open */
func (instance *plaintextAddressJudge) judge(runtimeInstance containercontract.Runtime, addresses []string) {
    plaintext := plaintextAddressesOf(addresses)
    if nil == instance || 0 == len(plaintext) {
        return
    }

    /* resolved without the emergency record LoggerFromRuntime writes for a runtime that holds no logger */
    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return
    }

    configuration, configurationErr := config.ConfigFromRuntime(runtimeInstance)
    ignored := nil == configurationErr && nil != configuration && true == slices.Contains(configuration.Kernel().BootWarningsIgnored(), PlaintextAddressBootWarningName)

    for _, address := range plaintext {
        if _, alreadyJudged := instance.judged.LoadOrStore(address, struct{}{}); true == alreadyJudged {
            continue
        }

        if true == ignored {
            continue
        }

        logger.Warning(plaintextAddressWarningMessage, loggingcontract.Context{applicationcontract.BootWarningContextKey: PlaintextAddressBootWarningName, "address": address})
    }
}
