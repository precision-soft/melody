package application

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "slices"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/lock"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type bootWarningDeclaration struct {
    name         string
    writtenInCli bool
}

type bootWarningTestModule struct {
    name         string
    declarations []bootWarningDeclaration
    warnings     []applicationcontract.BootWarning
}

func (instance *bootWarningTestModule) Name() string {
    return instance.name
}

func (instance *bootWarningTestModule) Description() string {
    return "declares boot warnings"
}

func (instance *bootWarningTestModule) RegisterBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    for _, declaration := range instance.declarations {
        registrar.DeclareBootWarning(declaration.name, declaration.writtenInCli)
    }

    for _, warning := range instance.warnings {
        registrar.AddBootWarning(warning)
    }
}

var _ applicationcontract.BootWarningModule = (*bootWarningTestModule)(nil)

const moduleHazardMessage = "the module's configuration admits a hazard"

func newModuleHazardModule(name string) *bootWarningTestModule {
    return &bootWarningTestModule{
        name:         name,
        declarations: []bootWarningDeclaration{{name: "test.module.hazard", writtenInCli: true}},
        warnings: []applicationcontract.BootWarning{{
            Name:    "test.module.hazard",
            Message: moduleHazardMessage,
            Context: loggingcontract.Context{"endpoint": "s3.example.com"},
        }},
    }
}

func refusalOf(t *testing.T, call func()) *exception.Error {
    t.Helper()

    var refusal *exception.Error
    func() {
        defer func() {
            refusal, _ = recover().(*exception.Error)
        }()

        call()
    }()

    return refusal
}

func TestBootModuleBootWarnings_AModuleCodeIsWrittenWithTheBootWarningsAndHonoursTheCliKeys(t *testing.T) {
    httpLogger := &warningRecordingLogger{}
    httpApplication := newCacheWarningTestApplication(t, config.ModeHttp, httpLogger)
    httpApplication.modules = []applicationcontract.Module{newModuleHazardModule("storage")}
    httpApplication.bootModuleBootWarnings()
    httpApplication.refuseUnknownCliBootWarningCodes()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := httpApplication.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    if warnings := httpLogger.warningsContaining(moduleHazardMessage); 1 != len(warnings) {
        t.Fatalf("expected the module's warning written once on the http path, got %v", warnings)
    }

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range []struct {
        name     string
        values   map[string]string
        expected int
    }{
        {"every code", map[string]string{}, 1},
        {"the module code ignored", map[string]string{config.CliBootWarningsIgnoredKey: "test.module.hazard"}, 0},
        {"the journal disabled", map[string]string{config.CliBootWarningsDisabledKey: "true"}, 0},
    } {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.modules = []applicationcontract.Module{newModuleHazardModule("storage")}
        cliApplication.bootModuleBootWarnings()
        cliApplication.refuseUnknownCliBootWarningCodes()

        if runErr := cliApplication.runCli(); nil != runErr {
            t.Fatalf("%s: unexpected run cli error: %v", testCase.name, runErr)
        }

        if warnings := cliLogger.warningsContaining(moduleHazardMessage); testCase.expected != len(warnings) {
            t.Fatalf("%s: expected the module's warning written %d times on a cli run, got %v", testCase.name, testCase.expected, warnings)
        }
    }
}

func TestBootModuleBootWarnings_RefusesANameAnotherOwnerHoldsNamingBoth(t *testing.T) {
    for _, testCase := range []struct {
        name          string
        declaredName  string
        expectedOwner string
    }{
        {"another module", "test.module.hazard", "storage"},
        {"the same module, written in cli no more", "test.module.hazard", "storage"},
        {"the framework", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, "the framework"},
    } {
        applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, &warningRecordingLogger{})
        secondModule := &bootWarningTestModule{name: "mailer", declarations: []bootWarningDeclaration{{name: testCase.declaredName, writtenInCli: true}}}
        expectedModule := "mailer"
        if "the same module, written in cli no more" == testCase.name {
            secondModule = &bootWarningTestModule{name: "storage", declarations: []bootWarningDeclaration{{name: testCase.declaredName, writtenInCli: false}}}
            expectedModule = "storage"
        }

        applicationInstance.modules = []applicationcontract.Module{newModuleHazardModule("storage"), secondModule}

        refusal := refusalOf(t, applicationInstance.bootModuleBootWarnings)
        if nil == refusal || "a module declared a boot warning name another owner already holds" != refusal.Message() {
            t.Fatalf("%s: expected the second declaration refused, got %v", testCase.name, refusal)
        }

        if expectedModule != refusal.Context()["module"] || testCase.expectedOwner != refusal.Context()["owner"] || testCase.declaredName != refusal.Context()["name"] {
            t.Fatalf("%s: expected both owners and the name named, got %v", testCase.name, refusal.Context())
        }
    }
}

func TestBootModuleBootWarnings_AdmitsTheSameModuleRegisteredTwice(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, &warningRecordingLogger{})
    applicationInstance.modules = []applicationcontract.Module{newModuleHazardModule("storage"), newModuleHazardModule("storage")}

    if refusal := refusalOf(t, applicationInstance.bootModuleBootWarnings); nil != refusal {
        t.Fatalf("expected a second instance of one module admitted, got %v", refusal)
    }

    if cliNames := applicationInstance.bootWarningRegistryOf().CliNames(); 5 != len(cliNames) || "test.module.hazard" != cliNames[4] {
        t.Fatalf("expected the code declared once, got %v", cliNames)
    }
}

func TestBootModuleBootWarnings_RefusesAnEmptyNameAWarningUnderANameTheModuleDidNotDeclareAndAnEmptyMessage(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        modules  []applicationcontract.Module
        expected string
    }{
        {
            "an empty name",
            []applicationcontract.Module{&bootWarningTestModule{name: "mailer", declarations: []bootWarningDeclaration{{name: "", writtenInCli: true}}}},
            "a module declared a boot warning with an empty name",
        },
        {
            "an undeclared name",
            []applicationcontract.Module{&bootWarningTestModule{name: "mailer", warnings: []applicationcontract.BootWarning{{Name: "test.undeclared", Message: "x"}}}},
            "a module added a boot warning under a name it did not declare",
        },
        {
            "a framework name",
            []applicationcontract.Module{&bootWarningTestModule{name: "mailer", warnings: []applicationcontract.BootWarning{{Name: internal.BootWarningNameFirewallDoorReachUnknown, Message: "x"}}}},
            "a module added a boot warning under a name it did not declare",
        },
        {
            "another module's name",
            []applicationcontract.Module{
                &bootWarningTestModule{name: "storage", declarations: []bootWarningDeclaration{{name: "test.module.hazard", writtenInCli: true}}},
                &bootWarningTestModule{name: "mailer", warnings: []applicationcontract.BootWarning{{Name: "test.module.hazard", Message: "x"}}},
            },
            "a module added a boot warning under a name it did not declare",
        },
        {
            "an undeclared name from a module with no name",
            []applicationcontract.Module{&bootWarningTestModule{name: "", warnings: []applicationcontract.BootWarning{{Name: internal.BootWarningNameFirewallDoorReachUnknown, Message: "x"}}}},
            "a module added a boot warning under a name it did not declare",
        },
        {
            "an empty message",
            []applicationcontract.Module{&bootWarningTestModule{
                name:         "mailer",
                declarations: []bootWarningDeclaration{{name: "test.module.hazard", writtenInCli: true}},
                warnings:     []applicationcontract.BootWarning{{Name: "test.module.hazard"}},
            }},
            "a module added a boot warning with an empty message",
        },
    } {
        applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, &warningRecordingLogger{})
        applicationInstance.modules = testCase.modules

        refusal := refusalOf(t, applicationInstance.bootModuleBootWarnings)
        expectedModule := "mailer"
        if "an undeclared name from a module with no name" == testCase.name {
            expectedModule = ""
        }

        if nil == refusal || testCase.expected != refusal.Message() || expectedModule != refusal.Context()["module"] {
            t.Fatalf("%s: expected %q naming the module, got %v", testCase.name, testCase.expected, refusal)
        }

        if 0 != len(applicationInstance.bootWarnings) {
            t.Fatalf("%s: expected nothing collected, got %v", testCase.name, applicationInstance.bootWarnings)
        }
    }
}

func TestRefuseUnknownCliBootWarningCodes_JudgesEachCodeAgainstTheFrameworkAndTheModules(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        code     string
        modules  []applicationcontract.Module
        expected string
    }{
        {"an unknown code", "security.firewallShadowed", nil, "cli boot warning code names no boot warning the framework or a module writes"},
        {"a module code with no module", "test.module.hazard", nil, "cli boot warning code names no boot warning the framework or a module writes"},
        {"an http-only framework code", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, nil, "cli boot warning code names a boot warning only an http process writes, so a cli process never reads it"},
        {"the retired defaulted environment code", "application.environment.defaulted", nil, "cli boot warning code names no boot warning the framework or a module writes"},
        {
            "an http-only module code",
            "test.module.httpHazard",
            []applicationcontract.Module{&bootWarningTestModule{name: "storage", declarations: []bootWarningDeclaration{{name: "test.module.httpHazard", writtenInCli: false}}}},
            "cli boot warning code names a boot warning only an http process writes, so a cli process never reads it",
        },
        {"a cli framework code", internal.BootWarningNameFirewallDoorReachUnknown, nil, ""},
        {"the ignored process variable code", internal.BootWarningNameProcessEnvironmentVariableIgnored, nil, ""},
        {"the unmatched secret mark code", internal.BootWarningNameSecretMarkUnmatched, nil, ""},
        {"the undeclared environment key code", internal.BootWarningNameEnvironmentUndeclaredKey, nil, ""},
        {"a cli module code", "test.module.hazard", []applicationcontract.Module{newModuleHazardModule("storage")}, ""},
    } {
        applicationInstance := newCacheWarningTestApplicationOver(t, config.ModeCli, map[string]string{config.CliBootWarningsIgnoredKey: "  " + testCase.code}, &warningRecordingLogger{})
        applicationInstance.modules = testCase.modules
        applicationInstance.bootModuleBootWarnings()

        refusal := refusalOf(t, applicationInstance.refuseUnknownCliBootWarningCodes)
        if "" == testCase.expected {
            if nil != refusal {
                t.Fatalf("%s: expected the code admitted, got %v", testCase.name, refusal)
            }

            continue
        }

        if nil == refusal || testCase.expected != refusal.Message() {
            t.Fatalf("%s: expected %q, got %v", testCase.name, testCase.expected, refusal)
        }

        cliCodes, isList := refusal.Context()["cliCodes"].([]string)
        if false == isList || false == slices.Equal(internal.NewBootWarningRegistry().CliNames(), cliCodes) {
            t.Fatalf("%s: expected the codes a command can list named, got %v", testCase.name, refusal.Context()["cliCodes"])
        }

        if config.CliBootWarningsIgnoredKey != refusal.Context()["environmentKey"] || 0 != refusal.Context()["index"] || testCase.code != refusal.Context()["code"] {
            t.Fatalf("%s: expected the key, the index and the code named, got %v", testCase.name, refusal.Context())
        }
    }
}

const bootCliCodeProbeMarker = "MELODY_TEST_BOOT_CLI_CODE_PROBE"

/* Boot exits the process on a refusal, so each probe boots in a child of the test binary; the configuration is built first, so a code refused there would exit 92 */
func TestBoot_JudgesTheCliCodesAfterTheModulesDeclaredTheirs(t *testing.T) {
    if probe := os.Getenv(bootCliCodeProbeMarker); "" != probe {
        values := map[string]string{
            config.EnvKey:                    config.EnvDevelopment,
            config.HttpAddressKey:            "127.0.0.1:34524",
            config.CliBootWarningsIgnoredKey: map[string]string{"moduleCode": "test.module.hazard", "unknownCode": "test.nobody.writes"}[probe],
        }

        environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: values})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli}),
            kernel:               newTestKernel(),
            httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
            modules:              []applicationcontract.Module{newModuleHazardModule("storage")},
        }

        applicationInstance.Boot()

        os.Exit(93)
    }

    for probe, expected := range map[string]struct {
        exitCode int
        output   string
    }{
        "moduleCode":  {93, ""},
        "unknownCode": {1, "cli boot warning code names no boot warning the framework or a module writes"},
    } {
        child := exec.Command(os.Args[0], "-test.run=^TestBoot_JudgesTheCliCodesAfterTheModulesDeclaredTheirs$")
        child.Env = append(os.Environ(), bootCliCodeProbeMarker+"="+probe)

        output, runErr := child.CombinedOutput()

        exitError, isExitError := runErr.(*exec.ExitError)
        if false == isExitError || expected.exitCode != exitError.ExitCode() {
            t.Fatalf("%s: expected the boot to exit %d, got err %v with output: %s", probe, expected.exitCode, runErr, string(output))
        }

        if false == strings.Contains(string(output), expected.output) {
            t.Fatalf("%s: expected the refusal %q, got: %s", probe, expected.output, string(output))
        }
    }
}

const (
    ignoredProcessVariableFragment = "process environment variable is ignored"
    unmatchedSecretMarkFragment    = "a secret marking matched no parameter"
)

/* both are collected rather than written at boot, so a command filters them like every other boot warning */
func TestRunCli_WritesTheIgnoredProcessVariableAndTheUnmatchedSecretMarkUnderTheCliKeys(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range []struct {
        name               string
        values             map[string]string
        expectedVariable   int
        expectedSecretMark int
    }{
        {"every code", map[string]string{}, 1, 1},
        {"the variable code ignored", map[string]string{config.CliBootWarningsIgnoredKey: internal.BootWarningNameProcessEnvironmentVariableIgnored}, 0, 1},
        {"the secret mark code ignored", map[string]string{config.CliBootWarningsIgnoredKey: internal.BootWarningNameSecretMarkUnmatched}, 1, 0},
        {"the journal disabled", map[string]string{config.CliBootWarningsDisabledKey: "true"}, 0, 0},
    } {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.refuseUnknownCliBootWarningCodes()

        cliApplication.bootWarnings = append(cliApplication.bootWarnings, ignoredProcessEnvironmentBootWarnings(cliApplication.configuration, []string{"MELODY_PROCESS_ROLE=worker"})...)
        cliApplication.MarkParameterSecret("app.misspelled")
        cliApplication.collectUnappliedSecretMarks()

        if runErr := cliApplication.runCli(); nil != runErr {
            t.Fatalf("%s: unexpected run cli error: %v", testCase.name, runErr)
        }

        if warnings := cliLogger.warningsContaining(ignoredProcessVariableFragment); testCase.expectedVariable != len(warnings) {
            t.Fatalf("%s: expected the ignored variable written %d times, got %v", testCase.name, testCase.expectedVariable, warnings)
        }

        if warnings := cliLogger.warningsContaining(unmatchedSecretMarkFragment); testCase.expectedSecretMark != len(warnings) {
            t.Fatalf("%s: expected the unmatched secret mark written %d times, got %v", testCase.name, testCase.expectedSecretMark, warnings)
        }
    }
}

func TestBoot_CollectsTheIgnoredProcessVariableAndTheUnmatchedSecretMark(t *testing.T) {
    t.Setenv(config.HttpAddressKey, "127.0.0.1:1")

    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    applicationInstance.MarkParameterSecret("app.misspelled")

    applicationInstance.Boot()

    collected := make(map[string]loggingcontract.Context)
    for _, bootWarning := range applicationInstance.bootWarnings {
        collected[bootWarning.Name] = bootWarning.Context
    }

    if config.HttpAddressKey != collected[internal.BootWarningNameProcessEnvironmentVariableIgnored]["environmentVariable"] {
        t.Fatalf("expected the boot to collect the ignored process variable, got %+v", applicationInstance.bootWarnings)
    }

    if "app.misspelled" != collected[internal.BootWarningNameSecretMarkUnmatched]["parameterName"] {
        t.Fatalf("expected the boot to collect the unmatched secret mark, got %+v", applicationInstance.bootWarnings)
    }
}

const bootWarningsBeforeExitProbeMarker = "MELODY_TEST_BOOT_WARNINGS_BEFORE_EXIT_PROBE"

/* a boot refused after the collection exits before any run, so the exit handler writes the boot warnings beside the exit record; the hazard they name may be what killed it */
func TestBoot_ARefusedBootWritesTheCollectedBootWarningsBeforeItsExitRecord(t *testing.T) {
    if probe := os.Getenv(bootWarningsBeforeExitProbeMarker); "" != probe {
        projectDirectory, disabled, _ := strings.Cut(probe, "|")
        values := map[string]string{config.EnvKey: config.EnvDevelopment, config.HttpAddressKey: "127.0.0.1:34525"}
        if "disabled" == disabled {
            values[config.CliBootWarningsDisabledKey] = "true"
        }

        environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: values})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli}),
            kernel:               newTestKernel(),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.MarkParameterSecret("app.misspelled")
        lock.NewLazyLocker(applicationInstance.kernel.ServiceContainer())

        applicationInstance.Boot()

        os.Exit(93)
    }

    for _, testCase := range []struct {
        name     string
        disabled string
        expected bool
    }{
        {"every code", "", true},
        {"the journal disabled", "disabled", false},
    } {
        childProjectDirectory := t.TempDir()
        child := exec.Command(os.Args[0], "-test.run=^TestBoot_ARefusedBootWritesTheCollectedBootWarningsBeforeItsExitRecord$")
        child.Env = append(os.Environ(), bootWarningsBeforeExitProbeMarker+"="+childProjectDirectory+"|"+testCase.disabled, config.HttpAddressKey+"=127.0.0.1:1")

        output, runErr := child.CombinedOutput()

        exitError, isExitError := runErr.(*exec.ExitError)
        if false == isExitError || 1 != exitError.ExitCode() {
            t.Fatalf("%s: expected the boot to exit 1, got err %v with output: %s", testCase.name, runErr, string(output))
        }

        journal, readErr := os.ReadFile(filepath.Join(childProjectDirectory, "var", "log", "dev.log"))
        if nil != readErr {
            t.Fatalf("%s: expected the child's journal: %v", testCase.name, readErr)
        }

        refusalAt := strings.Index(string(journal), "a container.Lazy handle names no container service")
        if -1 == refusalAt {
            t.Fatalf("%s: expected the journal to carry the refusal, got: %s", testCase.name, string(journal))
        }

        for _, code := range []string{internal.BootWarningNameProcessEnvironmentVariableIgnored, internal.BootWarningNameSecretMarkUnmatched} {
            writtenAt := strings.Index(string(journal), `"bootWarning":"`+code+`"`)
            if testCase.expected != (-1 != writtenAt) {
                t.Fatalf("%s: expected the boot warning %q written %v, got: %s", testCase.name, code, testCase.expected, string(journal))
            }

            if true == testCase.expected && writtenAt > refusalAt {
                t.Fatalf("%s: expected the boot warning %q before the exit record, got: %s", testCase.name, code, string(journal))
            }
        }
    }
}

func TestWriteBootWarnings_WritesOnceAndAnHttpProcessReadsNoCliKey(t *testing.T) {
    bootWarning := internal.BootWarning{Name: internal.BootWarningNameFirewallDoorReachUnknown, Message: "a firewall door's reach is not known at boot"}

    httpLogger := &warningRecordingLogger{}
    httpApplication := newCacheWarningTestApplicationOver(t, config.ModeHttp, map[string]string{config.CliBootWarningsDisabledKey: "true"}, httpLogger)
    httpApplication.bootWarnings = []internal.BootWarning{bootWarning}

    httpApplication.writeBootWarnings(httpLogger)
    httpApplication.writeBootWarningsBeforeExit(httpLogger)

    if warnings := httpLogger.warningsContaining(bootWarning.Message); 1 != len(warnings) {
        t.Fatalf("expected an http process dying on the cli keys to write the boot warning once, got %v", warnings)
    }
}

const runRefusalBootWarningsProbeMarker = "MELODY_TEST_RUN_REFUSAL_BOOT_WARNINGS_PROBE"

/* runHttp refuses a scope teardown outlasting the http shutdown before it writes the boot warnings, so Run's exit handler writes them */
func TestRun_AnHttpRunRefusedBeforeItsWriterWritesTheBootWarningsBeforeItsExitRecord(t *testing.T) {
    if projectDirectory := os.Getenv(runRefusalBootWarningsProbeMarker); "" != projectDirectory {
        environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: map[string]string{
            config.EnvKey:                  config.EnvDevelopment,
            config.HttpAddressKey:          "127.0.0.1:34526",
            config.ScopeTeardownTimeoutKey: "1h",
        }})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
            kernel:               newTestKernel(),
            httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.MarkParameterSecret("app.misspelled")

        applicationInstance.Run()

        os.Exit(93)
    }

    childProjectDirectory := t.TempDir()
    child := exec.Command(os.Args[0], "-test.run=^TestRun_AnHttpRunRefusedBeforeItsWriterWritesTheBootWarningsBeforeItsExitRecord$")
    child.Env = append(os.Environ(), runRefusalBootWarningsProbeMarker+"="+childProjectDirectory)

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() {
        t.Fatalf("expected the run to exit 1, got err %v with output: %s", runErr, string(output))
    }

    journal, readErr := os.ReadFile(filepath.Join(childProjectDirectory, "var", "log", "dev.log"))
    if nil != readErr {
        t.Fatalf("expected the child's journal: %v; output: %s", readErr, string(output))
    }

    refusalAt := strings.Index(string(journal), "the scope teardown timeout must stay under the http shutdown timeout")
    writtenAt := strings.Index(string(journal), `"bootWarning":"`+internal.BootWarningNameSecretMarkUnmatched+`"`)
    if -1 == refusalAt || -1 == writtenAt || writtenAt > refusalAt {
        t.Fatalf("expected the boot warning before the exit record, got: %s", string(journal))
    }
}

type panickingWarningLogger struct {
    warningRecordingLogger
}

func (instance *panickingWarningLogger) Warning(message string, context loggingcontract.Context) {
    panic("the journal is gone")
}

func TestWriteBootWarningsBeforeExit_AFailingWriteGivesWayToTheExitRecord(t *testing.T) {
    applicationInstance := newCacheWarningTestApplicationOver(t, config.ModeCli, map[string]string{}, &warningRecordingLogger{})
    applicationInstance.bootWarnings = []internal.BootWarning{{Name: internal.BootWarningNameSecretMarkUnmatched, Message: "a secret marking matched no parameter"}}

    applicationInstance.writeBootWarningsBeforeExit(&panickingWarningLogger{})
}
