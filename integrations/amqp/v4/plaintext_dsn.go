package amqp

import (
    "context"
    "net"
    "net/url"
    "slices"
    "strings"
    "sync"
    "sync/atomic"

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

/* plaintextHostJudge holds the hosts a provider has named: the hazard is the broker, not the connection, so every connection of one provider to a host names it once; it lives on the provider, so two applications in one process judge apart */
type plaintextHostJudge struct {
    judged sync.Map
}

/* plaintextJournal is where the warning is written: a runtime's logger, and whether that runtime's configuration lists the code under the ignore key */
type plaintextJournal struct {
    logger  loggingcontract.Logger
    ignored bool
}

type plaintextJournalContextKey struct{}

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

/* plaintextJournalOf answers the journal of a runtime, nil for one that holds no logger: resolved without the emergency record LoggerFromRuntime writes, since a dial without one leaves the warning for the next */
func plaintextJournalOf(runtimeInstance containercontract.Runtime) *plaintextJournal {
    if nil == runtimeInstance {
        return nil
    }

    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr || nil == logger {
        return nil
    }

    configuration, configurationErr := config.ConfigFromRuntime(runtimeInstance)
    ignored := nil == configurationErr && nil != configuration && true == slices.Contains(configuration.Kernel().BootWarningsIgnored(), PlaintextDsnBootWarningName)

    return &plaintextJournal{logger: logger, ignored: ignored}
}

/* withPlaintextJournal hands a dial the journal of the runtime its component last served, so a dialer built by Provider.Dialer judges its dsn at the dial */
func withPlaintextJournal(ctx context.Context, journal *plaintextJournal) context.Context {
    if nil == journal {
        return ctx
    }

    return context.WithValue(ctx, plaintextJournalContextKey{}, journal)
}

func plaintextJournalFrom(ctx context.Context) *plaintextJournal {
    journal, _ := ctx.Value(plaintextJournalContextKey{}).(*plaintextJournal)

    return journal
}

/* judge names the host, never the dsn, once per host, through the journal handed; with no journal the host stays unjudged for the next dial */
func (instance *plaintextHostJudge) judge(journal *plaintextJournal, dsn string) {
    host := plaintextHostOf(dsn)
    if nil == instance || "" == host || nil == journal {
        return
    }

    if _, alreadyJudged := instance.judged.LoadOrStore(host, struct{}{}); true == alreadyJudged {
        return
    }

    if true == journal.ignored {
        return
    }

    journal.logger.Warning(plaintextDsnWarningMessage, loggingcontract.Context{bootWarningContextKey: PlaintextDsnBootWarningName, "host": host})
}

/* dialJournalHolder keeps the journal a component hands its dials: the first runtime it served that holds a logger */
type dialJournalHolder struct {
    journal atomic.Pointer[plaintextJournal]
}

func (instance *dialJournalHolder) remember(runtimeInstance containercontract.Runtime) {
    if nil != instance.journal.Load() {
        return
    }

    if journal := plaintextJournalOf(runtimeInstance); nil != journal {
        instance.journal.CompareAndSwap(nil, journal)
    }
}

func (instance *dialJournalHolder) dialContext(ctx context.Context) context.Context {
    return withPlaintextJournal(ctx, instance.journal.Load())
}
