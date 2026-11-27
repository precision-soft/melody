package application

import (
    "context"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    container "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    runtime "github.com/precision-soft/melody/v4/container/runtime"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
)

func sessionRuntime() containercontract.Runtime {
    return sessionRuntimeUnder(context.Background())
}

func sessionRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

const servingTestStaticFile = "app.css"

/* bootServingTestApplication answers an http application booted in the named environment from a directory of its own, a .env and one public file, under every build tag set */
func bootServingTestApplication(t *testing.T, environmentName string, applicationConfig ApplicationConfig, prepare func(applicationInstance *Application)) *Application {
    t.Helper()

    return bootServingTestApplicationWithEnvironment(t, environmentName, "", applicationConfig, prepare)
}

/* bootServingTestApplicationWithEnvironment appends environmentLines, each ending in a newline, to the .env it boots with */
func bootServingTestApplicationWithEnvironment(t *testing.T, environmentName string, environmentLines string, applicationConfig ApplicationConfig, prepare func(applicationInstance *Application)) *Application {
    t.Helper()

    directory := t.TempDir()
    environment := []byte(config.EnvironmentNameKey + "=" + environmentName + "\n" + environmentLines)
    staticFile := []byte("body{}")

    if writeErr := os.WriteFile(filepath.Join(directory, ".env"), environment, 0o600); nil != writeErr {
        t.Fatalf("write .env: %v", writeErr)
    }

    if mkdirErr := os.MkdirAll(filepath.Join(directory, "public"), 0o700); nil != mkdirErr {
        t.Fatalf("mkdir public: %v", mkdirErr)
    }

    if writeErr := os.WriteFile(filepath.Join(directory, "public", servingTestStaticFile), staticFile, 0o600); nil != writeErr {
        t.Fatalf("write the public file: %v", writeErr)
    }

    previousDirectory, getwdErr := os.Getwd()
    if nil != getwdErr {
        t.Fatalf("getwd: %v", getwdErr)
    }

    if chdirErr := os.Chdir(directory); nil != chdirErr {
        t.Fatalf("chdir: %v", chdirErr)
    }
    t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

    applicationConfig.EnvironmentFiles = fstest.MapFS{".env": &fstest.MapFile{Data: environment}}
    applicationConfig.PublicFiles = fstest.MapFS{"public/" + servingTestStaticFile: &fstest.MapFile{Data: staticFile}}

    applicationInstance := NewApplication(context.Background(), applicationConfig)
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})

    if nil != prepare {
        prepare(applicationInstance)
    }

    applicationInstance.Boot()

    return applicationInstance
}

/* serveTestRequest serves the request through the handler the application hands out */
func serveTestRequest(t *testing.T, applicationInstance *Application, request *nethttp.Request) *httptest.ResponseRecorder {
    t.Helper()

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil != handlerErr {
        t.Fatalf("unexpected http handler error: %v", handlerErr)
    }

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    return recorder
}

/* sessionCookieOfAnIdentity saves a session holding an identity and answers its cookie */
func sessionCookieOfAnIdentity(t *testing.T, kernelInstance kernelcontract.Kernel) *nethttp.Cookie {
    t.Helper()

    manager := session.SessionMustFromResolver(t.Context(), kernelInstance.ServiceContainer())
    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("identity", "u1")
    if saveErr := manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    return &nethttp.Cookie{Name: session.SessionCookieName, Value: sessionInstance.Id()}
}

/* registerLevelCapturingLogger replaces the application's logger with one that counts records by level */
func registerLevelCapturingLogger(applicationInstance *Application) *levelCapturingLogger {
    logger := &levelCapturingLogger{}

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        containercontract.RegisterOptions{},
    )

    return logger
}

type levelCapturingLogger struct {
    mutex  sync.Mutex
    levels []loggingcontract.Level
}

func (instance *levelCapturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.levels = append(instance.levels, level)
}

func (instance *levelCapturingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *levelCapturingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *levelCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *levelCapturingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *levelCapturingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *levelCapturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *levelCapturingLogger) CauseChainDepth() int {
    return 8
}

/* countSince answers how many records at the level were filed after the first `from` records */
func (instance *levelCapturingLogger) countSince(from int, level loggingcontract.Level) int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    count := 0
    for _, recordedLevel := range instance.levels[from:] {
        if level == recordedLevel {
            count = count + 1
        }
    }

    return count
}

func (instance *levelCapturingLogger) recorded() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.levels)
}

const newApplicationRefusalProbeMarker = "MELODY_TEST_NEW_APPLICATION_REFUSAL_PROBE"

/* expectNewApplicationRefused runs the test again in a child, which builds the application over applicationConfig: NewApplication owns the process boundary and exits on a refusal, so the child's exit and output are what is read */
func expectNewApplicationRefused(t *testing.T, testName string, applicationConfig ApplicationConfig, expectedTexts ...string) {
    t.Helper()

    if "" != os.Getenv(newApplicationRefusalProbeMarker) {
        applicationConfig.EnvironmentFiles = testhelper.NewEmbeddedEnvironmentFs()
        applicationConfig.PublicFiles = testhelper.NewEmbeddedStaticFs()
        NewApplication(context.Background(), applicationConfig)

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
    child.Env = append(os.Environ(), newApplicationRefusalProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() {
        t.Fatalf("expected the application refused, got err %v with output: %s", runErr, string(output))
    }

    for _, expectedText := range expectedTexts {
        if false == strings.Contains(string(output), expectedText) {
            t.Fatalf("expected the refusal to carry %q, got: %s", expectedText, string(output))
        }
    }
}

/* freeLoopbackAddress answers a loopback address whose port the system just handed out, for a configuration whose server never serves; a test whose server serves binds through listenLoopback */
func freeLoopbackAddress(t *testing.T) string {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("unexpected listen error: %v", listenErr)
    }

    address := listener.Addr().String()

    if closeErr := listener.Close(); nil != closeErr {
        t.Fatalf("unexpected listener close error: %v", closeErr)
    }

    return address
}

const servingProbePath = "/serving-probe"

/* listenLoopback binds a loopback port the system hands out and keeps it bound for the server the test hands the listener to, so no other process takes the port in between */
func listenLoopback(t *testing.T) (net.Listener, string) {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("unexpected listen error: %v", listenErr)
    }

    t.Cleanup(func() { _ = listener.Close() })

    return listener, listener.Addr().String()
}

/* servingProbeToken is what this test's own server answers on the probe route, unique to the test and the process */
func servingProbeToken(t *testing.T) string {
    return t.Name() + "-" + strconv.Itoa(os.Getpid())
}

/* registerServingProbe answers the probe path with the test's token from an outer decorator, ahead of whatever the kernel under test would answer */
func registerServingProbe(t *testing.T, applicationInstance *Application) {
    t.Helper()

    token := servingProbeToken(t)

    applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            if servingProbePath != request.URL.Path {
                next.ServeHTTP(writer, request)

                return
            }

            _, _ = io.WriteString(writer, token)
        })
    })
}

/* awaitServing asks the probe path until the test's own server answers its token, so a foreign listener on the port never passes for it */
func awaitServing(t *testing.T, address string) {
    t.Helper()

    token := servingProbeToken(t)

    client := &nethttp.Client{
        Timeout:   500 * time.Millisecond,
        Transport: &nethttp.Transport{DisableKeepAlives: true},
    }

    answered := "no answer"
    for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
        response, requestErr := client.Get("http://" + address + servingProbePath)
        if nil == requestErr {
            body, _ := io.ReadAll(response.Body)
            _ = response.Body.Close()

            if nethttp.StatusOK == response.StatusCode && token == string(body) {
                return
            }

            answered = strconv.Itoa(response.StatusCode) + " " + string(body)
        } else {
            answered = requestErr.Error()
        }

        time.Sleep(10 * time.Millisecond)
    }

    t.Fatalf("the test's own server never answered on %s: %s", address, answered)
}

/* teardownWalkHolder is a service value whose teardown walk queues two items per level */
type teardownWalkHolder struct {
    first  *teardownWalkHolder
    second *teardownWalkHolder
}

/* openAccessControl keeps every path open on purpose: the fallback a compiled access control must hold */
func openAccessControl() *security.AccessControl {
    return security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewSegmentPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}})})
}
