package amqp

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
)

type plaintextDsnEnvironmentSource struct {
    values map[string]string
}

func (instance *plaintextDsnEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

func newPlaintextDsnRuntime(t *testing.T, ctx context.Context, values map[string]string) (containercontract.Runtime, *recordingLogger) {
    t.Helper()

    logger := &recordingLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})

    if nil != values {
        environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &plaintextDsnEnvironmentSource{values: values})
        if nil != environmentErr {
            t.Fatalf("environment: %v", environmentErr)
        }

        configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir(), melodyconfig.ConfigurationConfig{})
        if nil != configurationErr {
            t.Fatalf("configuration: %v", configurationErr)
        }

        container.MustRegister[configcontract.Configuration](serviceContainer, melodyconfig.ServiceConfig, func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        }, container.RegisterOptions{})
    }

    return runtime.NewRun(ctx, serviceContainer.NewScope()), logger
}

func plaintextDsnWarnings(logger *recordingLogger) []logRecord {
    logger.mutex.Lock()
    defer logger.mutex.Unlock()

    warnings := []logRecord{}
    for _, record := range logger.records {
        if PlaintextDsnBootWarningName == record.context[bootWarningContextKey] {
            warnings = append(warnings, record)
        }
    }

    return warnings
}

func TestPlaintextHostOf_OnlyAnAmqpDsnOffThisMachineIsNamed(t *testing.T) {
    for dsn, expected := range map[string]string{
        "amqp://guest:secret@broker.example.com:5672/": "broker.example.com:5672",
        "amqp://rabbitmq:5672/":                        "rabbitmq:5672",
        "amqp://10.0.0.5/":                             "10.0.0.5",
        "AMQP://broker.example.com/":                   "broker.example.com",
        "amqps://guest:secret@broker.example.com/":     "",
        "amqp://guest:secret@localhost:5672/":          "",
        "amqp://127.0.0.1:5672/":                       "",
        "amqp://[::1]:5672/":                           "",
        "not a dsn":                                    "",
    } {
        if expected != plaintextHostOf(dsn) {
            t.Fatalf("%s: expected %q, got %q", dsn, expected, plaintextHostOf(dsn))
        }
    }
}

func TestPlaintextHostJudge_NamesTheHostOnceAndNeverTheCredential(t *testing.T) {
    runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), nil)
    judge := &plaintextHostJudge{}
    for range 2 {
        judge.judge(plaintextJournalOf(runtimeInstance), "amqp://guest:secret@broker.example.com:5672/")
        judge.judge(plaintextJournalOf(runtimeInstance), "amqps://guest:secret@other.example.com/")
        judge.judge(plaintextJournalOf(runtimeInstance), "amqp://guest:secret@localhost/")
    }

    warnings := plaintextDsnWarnings(logger)
    if 1 != len(warnings) || plaintextDsnWarningMessage != warnings[0].message || "broker.example.com:5672" != warnings[0].context["host"] {
        t.Fatalf("expected one warning naming the host, got %+v", warnings)
    }

    if true == strings.Contains(fmt.Sprint(warnings[0].context), "secret") {
        t.Fatalf("expected the warning to carry no credential, got %v", warnings[0].context)
    }
}

func TestPlaintextHostJudge_HonoursTheBootWarningIgnoreKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        values   map[string]string
        expected int
    }{
        "no key":               {values: map[string]string{}, expected: 1},
        "the code ignored":     {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: PlaintextDsnBootWarningName}, expected: 0},
        "another code ignored": {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: "security.firewallDoorReachUnknown"}, expected: 1},
    } {
        runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), testCase.values)
        (&plaintextHostJudge{}).judge(plaintextJournalOf(runtimeInstance), "amqp://broker.example.com/")

        if testCase.expected != len(plaintextDsnWarnings(logger)) {
            t.Fatalf("%s: expected %d warnings, got %+v", name, testCase.expected, plaintextDsnWarnings(logger))
        }
    }
}

/* the runtime's context is done, so the dial never leaves; the warning is written first all the same */
func TestProviderOpen_WritesThePlaintextDsnWarning(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance, logger := newPlaintextDsnRuntime(t, cancelledContext, nil)

    if _, openErr := NewProvider("melody", ProviderConfig{}).Open(runtimeInstance, "amqp://guest:secret@broker.example.com:5672/"); nil == openErr {
        t.Fatalf("expected the open refused under a done context")
    }

    if 1 != len(plaintextDsnWarnings(logger)) {
        t.Fatalf("expected the open to name the plaintext dsn, got %+v", plaintextDsnWarnings(logger))
    }
}

/* the once is the provider's: a second application's provider over the same broker writes its own line */
func TestProvider_JudgesEachHostOncePerProvider(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance, logger := newPlaintextDsnRuntime(t, cancelledContext, nil)

    for _, provider := range []*Provider{NewProvider("melody", ProviderConfig{}), NewProvider("melody", ProviderConfig{})} {
        for range 2 {
            _, _ = provider.Open(runtimeInstance, "amqp://guest:secret@broker.example.com:5672/")
        }
    }

    if 2 != len(plaintextDsnWarnings(logger)) {
        t.Fatalf("expected one warning per provider, got %+v", plaintextDsnWarnings(logger))
    }
}

/* the dial context is done, so the dial never leaves; the dialer judges its dsn first through the journal the dial carries, once per host, and a dial with no journal leaves the host for the next */
func TestProviderDialer_JudgesTheDsnAtTheDial(t *testing.T) {
    runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), nil)
    dialer := NewProvider("melody", ProviderConfig{}).Dialer("amqp://guest:secret@broker.example.com:5672/")

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, _ = dialer(cancelledContext)
    if 0 != len(plaintextDsnWarnings(logger)) {
        t.Fatalf("expected no warning from a dial with no journal, got %+v", plaintextDsnWarnings(logger))
    }

    for range 2 {
        if _, dialErr := dialer(withPlaintextJournal(cancelledContext, plaintextJournalOf(runtimeInstance))); nil == dialErr {
            t.Fatalf("expected the dial refused under a done context")
        }
    }

    warnings := plaintextDsnWarnings(logger)
    if 1 != len(warnings) || "broker.example.com:5672" != warnings[0].context["host"] {
        t.Fatalf("expected the dial to name the host once, got %+v", warnings)
    }
}

/* the transport hands its dial the journal of the runtime it served, so a dialer built by Provider.Dialer writes the warning at the transport's first dial */
func TestTransport_HandsItsDialTheJournalOfTheRuntimeItServed(t *testing.T) {
    runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), nil)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.plaintext-journal")

    var dialJournal *plaintextJournal
    transport := NewTransport("melody.amqp.v4.plaintext-journal", registry, FromDialer(func(dialContext context.Context) (*Connection, error) {
        dialJournal = plaintextJournalFrom(dialContext)

        return nil, errors.New("broker down")
    }), TransportConfig{})
    defer transport.CloseWithContext(t.Context())

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "plaintext"}, nil)); nil == sendErr {
        t.Fatalf("expected the send refused over a broker that is down")
    }

    if nil == dialJournal || logger != dialJournal.logger {
        t.Fatalf("expected the dial to carry the journal of the runtime the send served, got %+v", dialJournal)
    }
}

/* the backplane's listener dials before any runtime reaches it, so its dials carry the backplane's own logger */
func TestServerSentEventBackplane_HandsItsDialItsLogger(t *testing.T) {
    logger := &recordingLogger{}
    journals := make(chan *plaintextJournal, 1)
    backplane := NewServerSentEventBackplane(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), FromDialer(func(dialContext context.Context) (*Connection, error) {
        select {
        case journals <- plaintextJournalFrom(dialContext):
        default:
        }

        return nil, errors.New("broker down")
    }), ServerSentEventBackplaneConfig{Logger: logger})
    defer backplane.CloseWithContext(t.Context())

    select {
    case journal := <-journals:
        if nil == journal || logger != journal.logger || true == journal.ignored {
            t.Fatalf("expected the dial to carry the backplane's logger, got %+v", journal)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the listener to dial")
    }
}

type plaintextDsnBootWarningRegistrar struct {
    declared []string
}

func (instance *plaintextDsnBootWarningRegistrar) DeclareBootWarning(name string, config applicationcontract.BootWarningConfig) {
    instance.declared = append(instance.declared, name)
}

func (instance *plaintextDsnBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
}

func TestModule_RegisterBootWarningsDeclaresThePlaintextDsnCode(t *testing.T) {
    registrar := &plaintextDsnBootWarningRegistrar{}
    NewModule(ModuleConfig{}).RegisterBootWarnings(registrar)

    if 1 != len(registrar.declared) || PlaintextDsnBootWarningName != registrar.declared[0] {
        t.Fatalf("expected the module to declare %s, got %v", PlaintextDsnBootWarningName, registrar.declared)
    }
}
