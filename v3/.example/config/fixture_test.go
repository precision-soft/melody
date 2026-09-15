package config

import (
    bun "github.com/uptrace/bun"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "context"
    "database/sql/driver"
    "errors"
    "net/http/httptest"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodywiring "github.com/precision-soft/melody/v3/wiring"
    "github.com/uptrace/bun/dialect/mysqldialect"
    nethttp "net/http"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    "database/sql"
    "strings"
    "testing"
    "time"
)

const balancerAddress = "172.18.0.9"

func requestForwardedBy(t *testing.T, peer string, forwardedFor string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = peer + ":41234"
    if "" != forwardedFor {
        httpRequest.Header.Set("X-Forwarded-For", forwardedFor)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("budget-test", time.Now()))
}

func resolvedBudgetKey(t *testing.T, trustedProxyList []string, peer string, forwardedFor string) string {
    t.Helper()

    resolver := requestBudgetConfig(100, newTrustedProxyResolver(strings.Join(trustedProxyList, ","), time.Now)).ClientIpResolver()
    if nil == resolver {
        t.Fatal("expected the request budget to resolve the client address rather than fall back to the peer")
    }

    return resolver(requestForwardedBy(t, peer, forwardedFor))
}

var errUndialedDatabase = errors.New("this handle is never dialed")

type deletionFailureCache struct {
    cachecontract.Cache
    failure error
}

func (instance *deletionFailureCache) Delete(key string) error {
    return instance.failure
}

func moduleWithRegisteredParameters(t *testing.T, values map[string]string) *Module {
    t.Helper()
    moduleInstance := moduleWithEnvironment(t, values)
    registrar := newRecordingParameterRegistrar()
    moduleInstance.RegisterParameters(registrar)
    for _, name := range []string{parameterRatesBaseUrl, parameterReportExportEndpoint} {
        value, exists := registrar.registered[name]
        if false == exists {
            t.Fatalf("required parameter %s missing", name)
        }
        moduleInstance.configuration.RegisterRuntime(name, value)
    }
    for _, name := range registrar.marked {
        moduleInstance.configuration.MarkSecret(name)
    }
    if err := moduleInstance.configuration.Resolve(); nil != err {
        t.Fatal(err)
    }
    return moduleInstance
}

func moduleWithEnvironment(t *testing.T, values map[string]string) *Module {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    return &Module{configuration: configuration}
}

type recordingJournalRepository struct {
    batchCount int
    entryCount int
    appendErr  error
}

func (instance *recordingJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    return entry, instance.appendErr
}

func (instance *recordingJournalRepository) AppendBatch(ctx context.Context, entryList []*repository.CatalogJournalEntry) error {
    if nil != instance.appendErr {
        return instance.appendErr
    }

    instance.batchCount = instance.batchCount + 1
    instance.entryCount = instance.entryCount + len(entryList)

    return nil
}

func (instance *recordingJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    return nil, nil
}

func (instance *recordingJournalRepository) Count(ctx context.Context) (int, error) {
    return 0, nil
}

var _ repository.CatalogJournalRepository = (*recordingJournalRepository)(nil)

func newTrailRuntime(t *testing.T, journalRepository repository.CatalogJournalRepository, requestId string) melodyruntimecontract.Runtime {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegisterScoped(
        serviceContainer,
        reporting.ServiceRequestReportTrail,
        func(resolver containercontract.Resolver) (*reporting.RequestReportTrail, error) {
            requestContext, requestContextErr := melodycontainer.FromResolverByType[*melodyhttp.RequestContext](resolver)
            if nil != requestContextErr {
                return nil, requestContextErr
            }

            return reporting.NewRequestReportTrail(
                requestContext,
                reporting.NewReportFormatter(),
                journalRepository,
                melodyclock.NewFrozenClock(time.Unix(1700000000, 0).UTC()),
            )
        },
    )

    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(
        melodyhttp.ServiceRequestContext,
        melodyhttp.NewRequestContext(requestId, time.Unix(0, 0)),
    )

    return melodyruntime.New(context.Background(), scope, serviceContainer)
}

func trailFromRuntime(t *testing.T, runtimeInstance melodyruntimecontract.Runtime) *reporting.RequestReportTrail {
    t.Helper()

    trail, trailErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
        runtimeInstance.Scope(),
        reporting.ServiceRequestReportTrail,
    )
    if nil != trailErr {
        t.Fatalf("resolve the trail: %v", trailErr)
    }

    return trail
}

func runFlushMiddleware(
    t *testing.T,
    runtimeInstance melodyruntimecontract.Runtime,
    next melodyhttpcontract.Handler,
) (melodyhttpcontract.Response, error) {
    t.Helper()

    httpRequest := httptest.NewRequest("POST", "/products/api/create/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("request", time.Unix(0, 0)))

    return NewCatalogJournalFlushMiddleware()(next)(runtimeInstance, httptest.NewRecorder(), request)
}

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

type recordingParameterRegistrar struct {
    registered map[string]any
    marked     []string
}

func newRecordingParameterRegistrar() *recordingParameterRegistrar {
    return &recordingParameterRegistrar{registered: map[string]any{}}
}

func (instance *recordingParameterRegistrar) RegisterParameter(name string, value any) {
    instance.registered[name] = value
}

func (instance *recordingParameterRegistrar) RegisterSecretParameter(name string, value any) {
    instance.registered[name] = value
    instance.marked = append(instance.marked, name)
}

func (instance *recordingParameterRegistrar) MarkParameterSecret(name string) {
    instance.marked = append(instance.marked, name)
}

func (instance *recordingParameterRegistrar) isMarked(name string) bool {
    for _, marked := range instance.marked {
        if name == marked {
            return true
        }
    }

    return false
}

func compiledSecurityModule(t *testing.T) *melodysecurityconfig.Builder {
    t.Helper()

    moduleInstance := &Module{}
    moduleInstance.buildInternalAuth()
    moduleInstance.buildTokenAuth()
    moduleInstance.buildImpersonation()
    moduleInstance.buildTwoFactor()

    builder := melodysecurityconfig.NewBuilder()
    moduleInstance.RegisterSecurity(builder)

    return builder
}

type refusingConnector struct{}

func (instance *refusingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errUndialedDatabase
}

func (instance *refusingConnector) Driver() driver.Driver {
    return nil
}

func newUndialedDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(&refusingConnector{}), mysqldialect.New())
}

type containerRegistrar struct {
    melodycontainercontract.Container
}

func (instance containerRegistrar) RegisterService(
    serviceName string,
    provider any,
    options ...melodycontainercontract.RegisterOption,
) {
    instance.MustRegister(serviceName, provider, options...)
}

type countingBackplane struct {
    hub    *melodyhttp.ServerSentEventHub
    closes int
}

func (instance *countingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return nil
}

func (instance *countingBackplane) Close() error {
    instance.closes++
    instance.hub.SetBackplane(nil)

    return nil
}

func moduleWithTrustedProxyList(t *testing.T, value string) *Module {
    t.Helper()

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyTrustedProxyList: value})
    moduleInstance.buildTrustedProxyResolver()

    return moduleInstance
}

func resolverOver(t *testing.T, value string, now func() time.Time) *trustedProxyResolver {
    t.Helper()

    return newTrustedProxyResolver(value, now)
}

func lookupTable(t *testing.T, table map[string][]string) *int {
    t.Helper()

    lookups := 0
    previous := trustedProxyLookup
    trustedProxyLookup = func(host string) ([]string, error) {
        lookups++

        addressList, known := table[host]
        if false == known {
            return nil, errors.New("lookup " + host + ": no such host")
        }

        return addressList, nil
    }
    t.Cleanup(func() {
        trustedProxyLookup = previous
    })

    return &lookups
}

const (
    generatedWiringFile = "../generated/wiring_gen.go"
    generatedPackage    = "generated"
    generatedFunction   = "RegisterGeneratedServices"
    wiringProjectDir = ".."
)

func generateWiring(t *testing.T) (string, *melodywiring.GenerateReport) {
    t.Helper()

    source, report, generateErr := melodywiring.Generate(&melodywiring.GenerateRequest{
        ProjectDirectory: wiringProjectDir,
        PackageName:      generatedPackage,
        FunctionName:     generatedFunction,
        BindSet:          NewWiringBindSet(),
    })
    if nil != generateErr {
        t.Fatalf("expected the wiring to generate, got %v", generateErr)
    }

    return source, report
}
