package awss3

import (
    "net"
    "slices"
    "strings"
    "sync"

    "github.com/minio/minio-go/v7"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* PlaintextEndpointBootWarningName is the code of the warning a storage over a plaintext endpoint off this machine writes, the code a cli process lists in MELODY_CLI_BOOT_WARNINGS_IGNORED to leave it out */
const PlaintextEndpointBootWarningName = "aws.s3.plaintextEndpoint"

const bootWarningContextKey = "bootWarning"

const plaintextEndpointWarningMessage = "object storage endpoint is plain http off this machine: credentials and objects cross the network in clear and every presigned url is http; leave Config.Insecure unset for an https endpoint"

/* the hazard is the endpoint, not the storage value: two storages over one host, or the module's boot record and a storage's first use, name it once per process */
var judgedPlaintextEndpoints sync.Map

/* markPlaintextEndpointJudged answers true for the one caller that judges the host first */
func markPlaintextEndpointJudged(endpoint string) bool {
    _, alreadyJudged := judgedPlaintextEndpoints.LoadOrStore(endpoint, struct{}{})

    return false == alreadyJudged
}

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

/* plaintextEndpointWarning names a plaintext endpoint off this machine. The module adds it to the boot warnings; a storage built outside the module writes it at its first use with a journal in reach, honouring the cli boot warning keys of the configuration it finds. */
type plaintextEndpointWarning struct {
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

/* bootWarning names the endpoint's host and never a credential */
func (instance *plaintextEndpointWarning) bootWarning() applicationcontract.BootWarning {
    return applicationcontract.BootWarning{
        Name:    PlaintextEndpointBootWarningName,
        Message: plaintextEndpointWarningMessage,
        Context: loggingcontract.Context{"endpoint": instance.endpoint},
    }
}

func (instance *plaintextEndpointWarning) write(runtimeInstance runtimecontract.Runtime) {
    if nil == instance {
        return
    }

    if _, alreadyJudged := judgedPlaintextEndpoints.Load(instance.endpoint); true == alreadyJudged {
        return
    }

    /* resolved without the emergency record LoggerFromRuntime writes for a runtime that holds no logger, since a use without one only leaves the warning for the next */
    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return
    }

    if false == markPlaintextEndpointJudged(instance.endpoint) {
        return
    }

    if true == silencedByCliBootWarningKeys(runtimeInstance) {
        return
    }

    warning := instance.bootWarning()
    context := loggingcontract.Context{bootWarningContextKey: warning.Name}
    for key, value := range warning.Context {
        context[key] = value
    }

    logger.Warning(warning.Message, context)
}

/* a storage used outside an application finds no configuration and writes; an http process refuses both keys at boot, so they only ever narrow what a command writes */
func silencedByCliBootWarningKeys(runtimeInstance runtimecontract.Runtime) bool {
    configuration, configurationErr := config.ConfigFromContainer(runtimeInstance.Context(), runtimeInstance.Container())
    if nil != configurationErr || nil == configuration {
        return false
    }

    cliConfiguration := configuration.Cli()

    return true == cliConfiguration.BootWarningsDisabled() || true == slices.Contains(cliConfiguration.BootWarningsIgnored(), PlaintextEndpointBootWarningName)
}
