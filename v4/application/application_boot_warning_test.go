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
    name string
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
        registrar.DeclareBootWarning(declaration.name, applicationcontract.BootWarningConfig{})
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
        declarations: []bootWarningDeclaration{{name: "test.module.hazard"}},
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

func TestBootModuleBootWarnings_AModuleCodeIsWrittenWithTheBootWarningsAndHonoursTheIgnoreKeyInBothProcesses(t *testing.T) {
    cases := []struct {
        name     string
        values   map[string]string
        expected int
    }{
        {"every code", map[string]string{}, 1},
        {"the module code ignored", map[string]string{config.BootWarningsIgnoredKey: "test.module.hazard"}, 0},
        {"another code ignored", map[string]string{config.BootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown}, 1},
    }

    for _, testCase := range cases {
        httpLogger := &warningRecordingLogger{}
        httpApplication := newCacheWarningTestApplicationOver(t, config.ModeHttp, testCase.values, httpLogger)
        httpApplication.modules = []applicationcontract.Module{newModuleHazardModule("storage")}
        httpApplication.bootModuleBootWarnings()
        httpApplication.refuseUnknownBootWarningCodes()

        cancelledContext, cancel := context.WithCancel(context.Background())
        cancel()

        if runErr := httpApplication.runHttp(cancelledContext); nil != runErr {
            t.Fatalf("%s: unexpected run http error: %v", testCase.name, runErr)
        }

        if warnings := httpLogger.warningsContaining(moduleHazardMessage); testCase.expected != len(warnings) {
            t.Fatalf("%s: expected the module's warning written %d times on the http path, got %v", testCase.name, testCase.expected, warnings)
        }
    }

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range cases {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.modules = []applicationcontract.Module{newModuleHazardModule("storage")}
        cliApplication.bootModuleBootWarnings()
        cliApplication.refuseUnknownBootWarningCodes()

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
        {"the framework", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, "the framework"},
    } {
        applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, &warningRecordingLogger{})
        secondModule := &bootWarningTestModule{name: "mailer", declarations: []bootWarningDeclaration{{name: testCase.declaredName}}}
        expectedModule := "mailer"

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

    if names := applicationInstance.bootWarningRegistryOf().Names(); 8 != len(names) || "test.module.hazard" != names[7] {
        t.Fatalf("expected the code declared once, got %v", names)
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
            []applicationcontract.Module{&bootWarningTestModule{name: "mailer", declarations: []bootWarningDeclaration{{name: ""}}}},
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
                &bootWarningTestModule{name: "storage", declarations: []bootWarningDeclaration{{name: "test.module.hazard"}}},
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
                declarations: []bootWarningDeclaration{{name: "test.module.hazard"}},
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

func TestRefuseUnknownBootWarningCodes_JudgesEachCodeAgainstTheFrameworkAndTheModulesInBothProcesses(t *testing.T) {
    unknownMessage := "boot warning code names no boot warning the framework or a module writes"

    for _, mode := range []string{config.ModeCli, config.ModeHttp} {
        for _, testCase := range []struct {
            name     string
            code     string
            modules  []applicationcontract.Module
            expected string
        }{
            {"an unknown code", "security.firewallShadowed", nil, unknownMessage},
            {"a module code with no module", "test.module.hazard", nil, unknownMessage},
            {"the retired defaulted environment code", "application.environment.defaulted", nil, unknownMessage},
            {"a framework code only an http process writes", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, nil, ""},
            {"a framework code both processes write", internal.BootWarningNameFirewallDoorReachUnknown, nil, ""},
            {"the ignored process variable code", internal.BootWarningNameProcessEnvironmentVariableIgnored, nil, ""},
            {"the unmatched secret mark code, now a refusal", "application.secretMark.unmatched", nil, unknownMessage},
            {"the undeclared environment key code, now a refusal", "application.environment.undeclaredKey", nil, unknownMessage},
            {"a module code", "test.module.hazard", []applicationcontract.Module{newModuleHazardModule("storage")}, ""},
        } {
            applicationInstance := newCacheWarningTestApplicationOver(t, mode, map[string]string{config.BootWarningsIgnoredKey: "  " + testCase.code}, &warningRecordingLogger{})
            applicationInstance.modules = testCase.modules
            applicationInstance.bootModuleBootWarnings()

            refusal := refusalOf(t, applicationInstance.refuseUnknownBootWarningCodes)
            if "" == testCase.expected {
                if nil != refusal {
                    t.Fatalf("%s %s: expected the code admitted, got %v", mode, testCase.name, refusal)
                }

                continue
            }

            if nil == refusal || testCase.expected != refusal.Message() {
                t.Fatalf("%s %s: expected %q, got %v", mode, testCase.name, testCase.expected, refusal)
            }

            codes, isList := refusal.Context()["codes"].([]string)
            if false == isList || false == slices.Equal(internal.NewBootWarningRegistry().Names(), codes) {
                t.Fatalf("%s %s: expected the codes the list may name, got %v", mode, testCase.name, refusal.Context()["codes"])
            }

            if config.BootWarningsIgnoredKey != refusal.Context()["environmentKey"] || 0 != refusal.Context()["index"] || testCase.code != refusal.Context()["code"] {
                t.Fatalf("%s %s: expected the key, the index and the code named, got %v", mode, testCase.name, refusal.Context())
            }
        }
    }
}

const bootCliCodeProbeMarker = "MELODY_TEST_BOOT_CLI_CODE_PROBE"

/* Boot exits the process on a refusal, so each probe boots in a child of the test binary; the configuration is built first, so a code refused there would exit 92 */
func TestBoot_JudgesTheCliCodesAfterTheModulesDeclaredTheirs(t *testing.T) {
    if probe := os.Getenv(bootCliCodeProbeMarker); "" != probe {
        values := map[string]string{
            config.EnvironmentNameKey:     config.EnvironmentDevelopment,
            config.HttpAddressKey:         freeLoopbackAddress(t),
            config.BootWarningsIgnoredKey: map[string]string{"moduleCode": "test.module.hazard", "unknownCode": "test.nobody.writes"}[probe],
        }

        environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: values})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli}),
            kernel:               newTestKernel(),
            httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
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
        "unknownCode": {1, "boot warning code names no boot warning the framework or a module writes"},
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

const ignoredProcessVariableFragment = "process environment variable is ignored"

/* collected rather than written at boot, so a command filters it like every other boot warning */
func TestRunCli_WritesTheIgnoredProcessVariableUnderTheIgnoreKey(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range []struct {
        name             string
        values           map[string]string
        expectedVariable int
    }{
        {"every code", map[string]string{}, 1},
        {"the variable code ignored", map[string]string{config.BootWarningsIgnoredKey: internal.BootWarningNameProcessEnvironmentVariableIgnored}, 0},
    } {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.refuseUnknownBootWarningCodes()

        cliApplication.bootWarnings = append(cliApplication.bootWarnings, ignoredProcessEnvironmentBootWarnings(cliApplication.configuration, []string{"MELODY_PROCESS_ROLE=worker"})...)

        if runErr := cliApplication.runCli(); nil != runErr {
            t.Fatalf("%s: unexpected run cli error: %v", testCase.name, runErr)
        }

        if warnings := cliLogger.warningsContaining(ignoredProcessVariableFragment); testCase.expectedVariable != len(warnings) {
            t.Fatalf("%s: expected the ignored variable written %d times, got %v", testCase.name, testCase.expectedVariable, warnings)
        }
    }
}

func TestBoot_CollectsTheIgnoredProcessVariable(t *testing.T) {
    t.Setenv(config.HttpAddressKey, "127.0.0.1:1")

    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    applicationInstance.MarkParameterSecretOptional("app.misspelled")

    applicationInstance.Boot()

    collected := make(map[string]loggingcontract.Context)
    for _, bootWarning := range applicationInstance.bootWarnings {
        collected[bootWarning.Name] = bootWarning.Context
    }

    if config.HttpAddressKey != collected[internal.BootWarningNameProcessEnvironmentVariableIgnored]["environmentVariable"] {
        t.Fatalf("expected the boot to collect the ignored process variable, got %+v", applicationInstance.bootWarnings)
    }

    if 1 != len(collected) {
        t.Fatalf("expected an optional mark that matched nothing to write no warning, got %+v", applicationInstance.bootWarnings)
    }
}

const bootWarningsBeforeExitProbeMarker = "MELODY_TEST_BOOT_WARNINGS_BEFORE_EXIT_PROBE"

/* a boot refused after the collection exits before any run, so the exit handler writes the boot warnings beside the exit record; the hazard they name may be what killed it */
func TestBoot_ARefusedBootWritesTheCollectedBootWarningsBeforeItsExitRecord(t *testing.T) {
    if probe := os.Getenv(bootWarningsBeforeExitProbeMarker); "" != probe {
        projectDirectory, ignored, _ := strings.Cut(probe, "|")
        values := map[string]string{config.EnvironmentNameKey: config.EnvironmentDevelopment, config.HttpAddressKey: freeLoopbackAddress(t)}
        if "ignored" == ignored {
            values[config.BootWarningsIgnoredKey] = internal.BootWarningNameProcessEnvironmentVariableIgnored
        }

        environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: values})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{})
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

        lock.NewLazyLocker(applicationInstance.kernel.ServiceContainer())

        applicationInstance.Boot()

        os.Exit(93)
    }

    for _, testCase := range []struct {
        name     string
        ignored  string
        expected bool
    }{
        {"every code", "", true},
        {"the code ignored", "ignored", false},
    } {
        childProjectDirectory := t.TempDir()
        child := exec.Command(os.Args[0], "-test.run=^TestBoot_ARefusedBootWritesTheCollectedBootWarningsBeforeItsExitRecord$")
        child.Env = append(os.Environ(), bootWarningsBeforeExitProbeMarker+"="+childProjectDirectory+"|"+testCase.ignored, config.HttpAddressKey+"=127.0.0.1:1")

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

        for _, code := range []string{internal.BootWarningNameProcessEnvironmentVariableIgnored} {
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

func TestWriteBootWarnings_WritesOnceAndAnHttpProcessHonoursTheIgnoreKey(t *testing.T) {
    bootWarning := internal.BootWarning{Name: internal.BootWarningNameFirewallDoorReachUnknown, Message: "a firewall door's reach is not known at boot"}

    for values, expected := range map[string]int{"": 1, internal.BootWarningNameFirewallDoorReachUnknown: 0} {
        httpLogger := &warningRecordingLogger{}
        httpApplication := newCacheWarningTestApplicationOver(t, config.ModeHttp, map[string]string{config.BootWarningsIgnoredKey: values}, httpLogger)
        httpApplication.bootWarnings = []internal.BootWarning{bootWarning}

        httpApplication.writeBootWarnings(httpLogger)
        httpApplication.writeBootWarningsBeforeExit(httpLogger)

        if warnings := httpLogger.warningsContaining(bootWarning.Message); expected != len(warnings) {
            t.Fatalf("ignored %q: expected the boot warning written %d times, got %v", values, expected, warnings)
        }
    }
}

const runRefusalBootWarningsProbeMarker = "MELODY_TEST_RUN_REFUSAL_BOOT_WARNINGS_PROBE"

/* runHttp refuses a scope teardown outlasting the http shutdown before it writes the boot warnings, so Run's exit handler writes them */
func TestRun_AnHttpRunRefusedBeforeItsWriterWritesTheBootWarningsBeforeItsExitRecord(t *testing.T) {
    if projectDirectory := os.Getenv(runRefusalBootWarningsProbeMarker); "" != projectDirectory {
        environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: map[string]string{
            config.EnvironmentNameKey:      config.EnvironmentDevelopment,
            config.HttpAddressKey:          freeLoopbackAddress(t),
            config.ScopeTeardownTimeoutKey: "1h",
        }})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{})
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
            kernel:               newTestKernel(),
            httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.Run()

        os.Exit(93)
    }

    childProjectDirectory := t.TempDir()
    child := exec.Command(os.Args[0], "-test.run=^TestRun_AnHttpRunRefusedBeforeItsWriterWritesTheBootWarningsBeforeItsExitRecord$")
    child.Env = append(os.Environ(), runRefusalBootWarningsProbeMarker+"="+childProjectDirectory, config.HttpAddressKey+"=127.0.0.1:1")

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
    writtenAt := strings.Index(string(journal), `"bootWarning":"`+internal.BootWarningNameProcessEnvironmentVariableIgnored+`"`)
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
    applicationInstance.bootWarnings = []internal.BootWarning{{Name: internal.BootWarningNameProcessEnvironmentVariableIgnored, Message: "a process environment variable is ignored"}}

    applicationInstance.writeBootWarningsBeforeExit(&panickingWarningLogger{})
}
