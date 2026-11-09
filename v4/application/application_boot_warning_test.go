package application

import (
    "context"
    "os"
    "os/exec"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
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
        {"the framework", internal.BootWarningNameEnvironmentDefaulted, "the framework"},
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

    if cliNames := applicationInstance.bootWarningRegistryOf().CliNames(); 2 != len(cliNames) || "test.module.hazard" != cliNames[1] {
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
        {"an http-only framework code", internal.BootWarningNameEnvironmentDefaulted, nil, "cli boot warning code names a boot warning only an http process writes, so a cli process never reads it"},
        {
            "an http-only module code",
            "test.module.httpHazard",
            []applicationcontract.Module{&bootWarningTestModule{name: "storage", declarations: []bootWarningDeclaration{{name: "test.module.httpHazard", writtenInCli: false}}}},
            "cli boot warning code names a boot warning only an http process writes, so a cli process never reads it",
        },
        {"a cli framework code", internal.BootWarningNameFirewallDoorReachUnknown, nil, ""},
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
        if false == isList || 1 != len(cliCodes) || internal.BootWarningNameFirewallDoorReachUnknown != cliCodes[0] {
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
