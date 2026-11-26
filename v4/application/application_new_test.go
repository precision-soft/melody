package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/httpkernelseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestFindProjectRootStartingFrom_FindsGoMod(t *testing.T) {
    projectDirectory := t.TempDir()

    err := os.WriteFile(filepath.Join(projectDirectory, "go.mod"), []byte("module example.com/test\n"), 0o644)
    if nil != err {
        t.Fatalf("failed to create go.mod: %v", err)
    }

    subDirectory := filepath.Join(projectDirectory, "a", "b")
    err = os.MkdirAll(subDirectory, 0o755)
    if nil != err {
        t.Fatalf("failed to create sub directory: %v", err)
    }

    resolvedProjectDirectory, err := findProjectRootStartingFrom(subDirectory)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if projectDirectory != resolvedProjectDirectory {
        t.Fatalf("expected %q, got %q", projectDirectory, resolvedProjectDirectory)
    }
}

func TestFindProjectRootStartingFrom_ReturnsErrorWhenNotFound(t *testing.T) {
    directory := t.TempDir()

    resolvedProjectDirectory, err := findProjectRootStartingFrom(directory)
    if nil == err {
        t.Fatalf("expected error")
    }
    if "" != resolvedProjectDirectory {
        t.Fatalf("expected empty directory, got %q", resolvedProjectDirectory)
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnv(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnvLocal(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env.local"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env.local: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env.local is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_ReturnsFalseWhenAbsent(t *testing.T) {
    directory := t.TempDir()

    if true == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected false when no .env files are present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_IgnoresDirectoryNamedDotEnv(t *testing.T) {
    directory := t.TempDir()

    err := os.Mkdir(filepath.Join(directory, ".env"), 0o755)
    if nil != err {
        t.Fatalf("failed to create .env directory: %v", err)
    }

    if true == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected false when .env is a directory")
    }
}

/* stat-ing through a regular file yields ENOTDIR, which is how the non-not-exist stat error is produced here without permission tricks. */
func TestWorkingDirectoryHasEnvironmentFile_TreatsUnprovableStatErrorAsPresent(t *testing.T) {
    directory := t.TempDir()

    occupiedPath := filepath.Join(directory, "occupied")
    err := os.WriteFile(occupiedPath, []byte("not a directory"), 0o600)
    if nil != err {
        t.Fatalf("failed to create file: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(occupiedPath) {
        t.Fatalf("expected true when the stat error cannot prove absence")
    }
}

func TestIsGoRunExecutableDirectory_MatchesOnlyTheToolsTemporaryDirectories(t *testing.T) {
    matching := []string{
        "/tmp/go-build2932477933/b001/exe",
        "/var/folders/x1/T/go-build1/b001/exe",
        "/tmp/go-build/b001/exe",
        "/root/.cache/go-build/77/77f7a6c161b2fd7fcb2ed10b4c769bbb65331d8d32f44f46148489c0a64cd13c-d",
    }

    for _, directory := range matching {
        if false == isGoRunExecutableDirectory(directory) {
            t.Fatalf("expected %q to be recognized as a go run build directory", directory)
        }
    }

    nonMatching := []string{
        "/opt/go-builder/bin",
        "/srv/go-build-artifacts/app",
        "/data/go-builds/bin",
        "/usr/local/bin",
    }

    for _, directory := range nonMatching {
        if true == isGoRunExecutableDirectory(directory) {
            t.Fatalf("expected %q not to be classified as a go run build directory", directory)
        }
    }
}

/* the refusal of an executable path whose symlinks cannot be followed carries the path handed in, since the resolved one is empty on a failure. */
func TestResolveExecutablePath_RefusalCarriesThePathAsHanded(t *testing.T) {
    missingPath := filepath.Join(t.TempDir(), "missing", "binary")

    _, resolveErr := resolveExecutablePath(missingPath)
    melodyErr, isMelodyErr := resolveErr.(*exception.Error)
    if false == isMelodyErr || "failed to resolve executable path" != melodyErr.Message() {
        t.Fatalf("expected the resolution to be refused, got %v", resolveErr)
    }

    if missingPath != melodyErr.Context()["executablePath"] {
        t.Fatalf("expected the refusal to carry the path as handed, got %v", melodyErr.Context())
    }
}

type creationWaitersProbe struct{}

func TestNewApplication_TheContainerCreationWaitersFieldReachesTheContainer(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), ContainerCreationWaiters: bound.Of(1)})
    serviceContainer := applicationInstance.kernel.ServiceContainer()
    defer func() {
        _ = serviceContainer.CloseWithContext(context.Background())
    }()

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    container.MustRegister[*creationWaitersProbe](serviceContainer, "app.creation.waiters", func(resolver containercontract.Resolver) (*creationWaitersProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &creationWaitersProbe{}, nil
    }, container.RegisterOptions{})

    results := make(chan error, 3)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
        results <- getErr
    }()

    <-providerEntered

    for waiter := 0; waiter < 2; waiter++ {
        go func() {
            _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
            results <- getErr
        }()
    }

    select {
    case getErr := <-results:
        if false == errors.Is(getErr, container.ErrCreationWaitersExceeded) {
            t.Fatalf("expected the waiter past the bound of 1 to be refused, got %v", getErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("expected the bound the application declared to refuse the second waiter")
    }

    close(releaseProvider)

    for answer := 0; answer < 2; answer++ {
        if getErr := <-results; nil != getErr {
            t.Fatalf("unexpected resolution error: %v", getErr)
        }
    }
}

func TestNewApplication_TheContainerTeardownSequentialFieldReachesTheContainer(t *testing.T) {
    for _, sequential := range []bool{false, true} {
        applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), ContainerTeardownSequential: sequential})
        serviceContainer := applicationInstance.kernel.ServiceContainer()

        viewed, carriesView := serviceContainer.(interface{ TeardownRunsInWaves() bool })
        if false == carriesView {
            t.Fatalf("expected the container to carry the waves view")
        }

        if sequential == viewed.TeardownRunsInWaves() {
            t.Fatalf("expected ContainerTeardownSequential %v to reach the container, got waves %v", sequential, viewed.TeardownRunsInWaves())
        }

        _ = serviceContainer.CloseWithContext(context.Background())
    }
}

type scopeTeardownTimeoutProbe struct {
    hasDeadline bool
}

func (instance *scopeTeardownTimeoutProbe) CloseWithContext(closeContext context.Context) error {
    _, instance.hasDeadline = closeContext.Deadline()

    return nil
}

func TestNewApplication_TheScopeTeardownTimeoutFieldReachesTheContainersScopes(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), ScopeTeardownTimeout: bound.UnlimitedDuration()})
    serviceContainer := applicationInstance.kernel.ServiceContainer()
    defer func() {
        _ = serviceContainer.CloseWithContext(context.Background())
    }()

    container.MustRegisterScoped[*scopeTeardownTimeoutProbe](
        serviceContainer,
        "app.scope.teardown.timeout",
        func(resolver containercontract.Resolver) (*scopeTeardownTimeoutProbe, error) {
            return &scopeTeardownTimeoutProbe{}, nil
        },
        containercontract.RegisterOptions{},
    )

    scope := serviceContainer.NewScope()
    probe := container.MustFromResolver[*scopeTeardownTimeoutProbe](context.Background(), scope, "app.scope.teardown.timeout")

    if closeErr := scope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == probe.hasDeadline {
        t.Fatalf("expected the application's unlimited timeout to reach the scope, the close carried a deadline")
    }
}

func TestNewApplication_EnvironmentKeysReachTheConfiguration(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{
        EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(),
        PublicFiles:      testhelper.NewEmbeddedStaticFs(),
        EnvironmentKeys: []configcontract.EnvironmentKey{
            {Key: "APP_GREETING", Parameter: "app.greeting", Field: "ApplicationConfig.EnvironmentKeys", Default: "hello"},
        },
    })
    defer func() {
        _ = applicationInstance.kernel.ServiceContainer().CloseWithContext(context.Background())
    }()

    parameter := applicationInstance.Configuration().Get("app.greeting")
    if nil == parameter || "hello" != parameter.String() {
        t.Fatalf("expected the application key's default through its parameter, got %v", parameter)
    }
}

func TestNewApplication_AnOverrideReachesTheContainerAndTheRuntimeFlags(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{
        EnvironmentFiles:   testhelper.NewEmbeddedEnvironmentFs(),
        PublicFiles:        testhelper.NewEmbeddedStaticFs(),
        ParameterOverrides: map[string]string{"kernel.container_creation_waiters": "1", "kernel.default_mode": "cli"},
    })
    serviceContainer := applicationInstance.kernel.ServiceContainer()
    defer func() {
        _ = serviceContainer.CloseWithContext(context.Background())
    }()

    if "cli" != applicationInstance.runtimeFlags.Mode() {
        t.Fatalf("expected the overridden default mode, got %q", applicationInstance.runtimeFlags.Mode())
    }

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})
    defer close(releaseProvider)

    container.MustRegister[*creationWaitersProbe](serviceContainer, "app.creation.waiters", func(resolver containercontract.Resolver) (*creationWaitersProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &creationWaitersProbe{}, nil
    }, container.RegisterOptions{})

    results := make(chan error, 3)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
        results <- getErr
    }()

    <-providerEntered

    for waiter := 0; waiter < 2; waiter++ {
        go func() {
            _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
            results <- getErr
        }()
    }

    select {
    case getErr := <-results:
        if false == errors.Is(getErr, container.ErrCreationWaitersExceeded) {
            t.Fatalf("expected the waiter past the overridden bound of 1 to be refused, got %v", getErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("expected the overridden bound to refuse the second waiter")
    }
}

func TestNewApplication_HttpKernelMiddlewares_RefusedNamingTheDoor(t *testing.T) {
    expectNewApplicationRefused(
        t,
        "TestNewApplication_HttpKernelMiddlewares_RefusedNamingTheDoor",
        ApplicationConfig{HttpKernel: http.KernelConfig{Middlewares: []httpcontract.Middleware{func(next httpcontract.Handler) httpcontract.Handler { return next }}}},
        "the application's http middlewares are registered through RegisterHttpMiddlewares",
        "ApplicationConfig.HttpKernel.Middlewares",
    )
}

func TestNewApplication_HttpKernelSessionCookiePolicy_RefusedAtConstruction(t *testing.T) {
    expectNewApplicationRefused(
        t,
        "TestNewApplication_HttpKernelSessionCookiePolicy_RefusedAtConstruction",
        ApplicationConfig{HttpKernel: http.KernelConfig{SessionCookiePolicy: httpcontract.SessionCookiePolicy{SameSite: nethttp.SameSiteNoneMode, Secure: httpcontract.SessionCookieSecureNever}}},
        "the session cookie policy sets SameSite=None with Secure never",
        "SessionCookiePolicy.Secure",
    )
}

func TestNewApplication_HttpKernelUnparsableTrustedProxy_RefusedAtConstruction(t *testing.T) {
    expectNewApplicationRefused(
        t,
        "TestNewApplication_HttpKernelUnparsableTrustedProxy_RefusedAtConstruction",
        ApplicationConfig{HttpKernel: http.KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{TrustForwardedHeaders: true, TrustedProxyList: []string{"10.0.0"}}}},
        "trusted proxy entry is neither a CIDR prefix nor an address",
    )
}

func TestNewApplication_HttpServerLimit_RefusedNamingTheNestedField(t *testing.T) {
    expectNewApplicationRefused(
        t,
        "TestNewApplication_HttpServerLimit_RefusedNamingTheNestedField",
        ApplicationConfig{HttpServer: HttpServerConfig{WriteTimeout: bound.UnlimitedDuration()}},
        "the http server limits are refused",
        "ApplicationConfig.HttpServer.WriteTimeout",
    )
}

func TestNewApplication_HttpKernelNotFoundHandler_ServesAnUnmatchedPath(t *testing.T) {
    applicationInstance := bootServingTestApplication(t, config.EnvironmentProduction, ApplicationConfig{HttpKernel: http.KernelConfig{
        NotFoundHandler: func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusTeapot, "the application's own"), nil
        },
    }}, nil)

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/nowhere", nil))
    if nethttp.StatusTeapot != recorder.Code || "the application's own" != recorder.Body.String() {
        t.Fatalf("expected the field's not found handler to answer, got %d %q", recorder.Code, recorder.Body.String())
    }
}

func TestNewApplication_HttpKernelRetryAfter_KeyWinsOverTheField(t *testing.T) {
    for name, testCase := range map[string]struct {
        environmentLines string
        expected         string
    }{
        "the key over the field":   {environmentLines: config.HttpUnavailableRetryAfterKey + "=7s\n", expected: "7"},
        "the field without the key": {environmentLines: "", expected: "9"},
    } {
        applicationInstance := bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, testCase.environmentLines, ApplicationConfig{HttpKernel: http.KernelConfig{UnavailableRetryAfter: bound.Of(9 * time.Second)}}, nil)

        applicationInstance.kernel.HttpRouter().Handle(
            "/unavailable",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, exception.MarkInfrastructureFailure(errors.New("cache is down"))
            },
            httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/unavailable", nil))
        if nethttp.StatusServiceUnavailable != recorder.Code || testCase.expected != recorder.Header().Get("Retry-After") {
            t.Fatalf("%s: expected 503 with Retry-After %s, got %d %q", name, testCase.expected, recorder.Code, recorder.Header().Get("Retry-After"))
        }
    }
}

func TestNewApplication_HttpKernelSessionEmptyOnOutage_KeyWinsOverTheField(t *testing.T) {
    for name, testCase := range map[string]struct {
        environmentLines string
        field            bool
        expected         int
    }{
        "the key on over the field off":  {environmentLines: config.HttpSessionEmptyOnOutageKey + "=true\n", field: false, expected: nethttp.StatusOK},
        "the key off over the field on":  {environmentLines: config.HttpSessionEmptyOnOutageKey + "=false\n", field: true, expected: nethttp.StatusServiceUnavailable},
        "the field on without the key":   {environmentLines: "", field: true, expected: nethttp.StatusOK},
    } {
        applicationInstance := bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, testCase.environmentLines, ApplicationConfig{HttpKernel: http.KernelConfig{SessionPolicy: httpcontract.SessionPolicy{EmptyOnOutage: testCase.field}}}, nil)

        prepareSessionReaderOverAnOutage(applicationInstance.kernel.ServiceContainer(), applicationInstance.kernel.HttpRouter())

        if status := serveSessionReader(t, applicationInstance); testCase.expected != status {
            t.Fatalf("%s: expected %d, got %d", name, testCase.expected, status)
        }
    }
}

func TestNewApplication_HttpKernelSessionCookieSecure_KeyWinsOverTheField(t *testing.T) {
    for name, testCase := range map[string]struct {
        environmentLines string
        field            httpcontract.SessionCookieSecurePolicy
        expected         bool
    }{
        "the key always over the field from scheme": {environmentLines: config.HttpSessionCookieSecureKey + "=always\n", field: httpcontract.SessionCookieSecureFromScheme, expected: true},
        "the key always over the field never":       {environmentLines: config.HttpSessionCookieSecureKey + "=always\n", field: httpcontract.SessionCookieSecureNever, expected: true},
        "the field always without the key":          {environmentLines: "", field: httpcontract.SessionCookieSecureAlways, expected: true},
        "neither":                                   {environmentLines: "", field: httpcontract.SessionCookieSecureFromScheme, expected: false},
    } {
        applicationInstance := bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, testCase.environmentLines, ApplicationConfig{HttpKernel: http.KernelConfig{SessionCookiePolicy: httpcontract.SessionCookiePolicy{Secure: testCase.field}}}, nil)

        applicationInstance.kernel.HttpRouter().Handle(
            "/writes-the-session",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                sessionInstance, sessionErr := http.RequestSession(request)
                if nil != sessionErr {
                    return nil, sessionErr
                }

                sessionInstance.Set("identity", "u1")

                return http.TextResponse(nethttp.StatusOK, "ok"), nil
            },
            httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        cookies := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "http://app.example/writes-the-session", nil)).Result().Cookies()
        if 1 != len(cookies) || testCase.expected != cookies[0].Secure {
            t.Fatalf("%s: expected one session cookie Secure %v over plain http, got %+v", name, testCase.expected, cookies)
        }
    }
}

/* the key only hardens: from-scheme would lower a Go always, so only the Go field lowers the policy */
func TestNewApplication_HttpSessionCookieSecureKey_RefusedNamingTheKey(t *testing.T) {
    if value := os.Getenv(newApplicationRefusalProbeMarker); "" != value {
        bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, config.HttpSessionCookieSecureKey+"="+value+"\n", ApplicationConfig{HttpKernel: http.KernelConfig{SessionCookiePolicy: httpcontract.SessionCookiePolicy{Secure: httpcontract.SessionCookieSecureAlways}}}, nil)

        os.Exit(93)
    }

    for _, value := range []string{"never", "from-scheme"} {
        child := exec.Command(os.Args[0], "-test.run=^TestNewApplication_HttpSessionCookieSecureKey_RefusedNamingTheKey$")
        child.Env = append(os.Environ(), newApplicationRefusalProbeMarker+"="+value)

        output, runErr := child.CombinedOutput()

        exitError, isExitError := runErr.(*exec.ExitError)
        if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), config.HttpSessionCookieSecureKey) {
            t.Fatalf("%s: expected the application refused naming the key, got err %v with output: %s", value, runErr, string(output))
        }
    }
}

func TestApplicationBoot_InstallsThePipelineThroughTheSeam(t *testing.T) {
    applicationInstance := bootServingTestApplication(t, config.EnvironmentProduction, ApplicationConfig{}, nil)

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.httpKernel.InstallMiddlewares(httpkernelseam.NewToken(), nil)
    }, "the http kernel's middlewares are already installed")
}

func TestNewApplication_TheNamespaceFieldReachesTheKernelConfiguration(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), Namespace: "shop"})
    defer func() {
        _ = applicationInstance.kernel.ServiceContainer().CloseWithContext(context.Background())
    }()

    if namespace := applicationInstance.configuration.Kernel().Namespace(); "shop" != namespace {
        t.Fatalf("expected the field's namespace, got %q", namespace)
    }
}
