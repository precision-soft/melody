package application

import (
    "context"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "slices"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

var environmentKeyCountRule = bound.Rule[int]{Default: 3, Minimum: 1}

func environmentKeyCount(defaultText string) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_COUNT",
        Parameter: "melody.x.count",
        Field:     "x.ModuleConfig.Count",
        Default:   defaultText,
        Rule:      config.LimitKeyRule(environmentKeyCountRule),
    }
}

func environmentKeyText() configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_TEXT",
        Parameter: "melody.x.text",
        Field:     "x.ModuleConfig.Text",
        Default:   "declared",
    }
}

type environmentKeyTestModule struct {
    name string
    keys []configcontract.EnvironmentKey
    read func(registrar applicationcontract.ServiceRegistrar)
}

func (instance *environmentKeyTestModule) Name() string {
    return instance.name
}

func (instance *environmentKeyTestModule) Description() string {
    return "declares environment keys"
}

func (instance *environmentKeyTestModule) RegisterEnvironmentKeys(registrar applicationcontract.EnvironmentKeyRegistrar) {
    for _, key := range instance.keys {
        registrar.DeclareEnvironmentKey(key)
    }
}

func (instance *environmentKeyTestModule) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil != instance.read {
        instance.read(registrar)
    }
}

var _ applicationcontract.EnvironmentKeyModule = (*environmentKeyTestModule)(nil)
var _ applicationcontract.ServiceModule = (*environmentKeyTestModule)(nil)

type handParameterModule struct {
    fakeModule
    parameterName string
}

func (instance *handParameterModule) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
    registrar.RegisterParameter(instance.parameterName, "hand")
}

func newEnvironmentKeyBootApplication(t *testing.T, values map[string]string) *Application {
    t.Helper()

    return newEnvironmentKeyBootApplicationIn(t, values, t.TempDir())
}

func newEnvironmentKeyBootApplicationIn(t *testing.T, values map[string]string, projectDirectory string) *Application {
    t.Helper()

    bootValues := map[string]string{config.EnvironmentNameKey: config.EnvironmentDevelopment}
    for key, value := range values {
        bootValues[key] = value
    }

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: bootValues})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli}),
        kernel:               newTestKernel(),
        cliCommands:          make([]clicontract.Command, 0),
        moduleConfigurations: make(map[string]any),
    }
}

func errorChainHolds(err error, message string) *exception.Error {
    for current := err; nil != current; current = errors.Unwrap(current) {
        if refusal, isRefusal := current.(*exception.Error); true == isRefusal && message == refusal.Message() {
            return refusal
        }
    }

    return nil
}

const environmentKeyOrderChildVariable = "MELODY_TEST_ENVIRONMENT_KEY_ORDER_CHILD"

/* Boot exits the process at the report, so it runs in a child of the test binary */
func TestBoot_AModuleKeyIsDeclaredBeforeAnyParameterHook(t *testing.T) {
    if "1" == os.Getenv(environmentKeyOrderChildVariable) {
        applicationInstance := newEnvironmentKeyBootApplication(t, nil)
        applicationInstance.modules = []applicationcontract.Module{
            &handParameterModule{fakeModule: fakeModule{name: "hand"}, parameterName: "melody.x.text"},
            &environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText()}},
        }

        applicationInstance.Boot()

        return
    }

    command := exec.Command(os.Args[0], "-test.run=^TestBoot_AModuleKeyIsDeclaredBeforeAnyParameterHook$")
    command.Env = append(os.Environ(), environmentKeyOrderChildVariable+"=1")

    output, runErr := command.CombinedOutput()
    text := string(output)

    if nil == runErr || false == strings.Contains(text, "after unrecovered error: duplicate registrations detected at boot") {
        t.Fatalf("expected the hand registration reported as the duplicate, got %v: %s", runErr, text[max(0, len(text)-1500):])
    }
}

func TestBootModuleEnvironmentKeys_TheDeclarationStandsOverAHandRegistrationInAParameterHook(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.modules = []applicationcontract.Module{
        &handParameterModule{fakeModule: fakeModule{name: "hand"}, parameterName: "melody.x.text"},
        &environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText()}},
    }

    applicationInstance.bootModuleEnvironmentKeys()
    applicationInstance.bootModulesPreConfigurationResolve()

    if 1 != len(applicationInstance.bootCollisions) || bootCollisionKindParameter != applicationInstance.bootCollisions[0].kind || "melody.x.text" != applicationInstance.bootCollisions[0].name {
        t.Fatalf("expected the hand registration recorded as the duplicate, got %+v", applicationInstance.bootCollisions)
    }

    if "declared" != applicationInstance.configuration.Get("melody.x.text").EnvironmentValue() {
        t.Fatalf("expected the declaration to stand")
    }
}

const environmentKeyRefusalChildVariable = "MELODY_TEST_ENVIRONMENT_KEY_REFUSAL_CHILD"

const environmentKeyServicesRanMarker = "the module's services were registered"

/* Boot exits the process at the refusal, so it runs in a child of the test binary and the parent reads the child's journal */
func TestBoot_RefusesAModuleKeyOutsideItsRule(t *testing.T) {
    if projectDirectory := os.Getenv(environmentKeyRefusalChildVariable); "" != projectDirectory {
        applicationInstance := newEnvironmentKeyBootApplicationIn(t, map[string]string{"MELODY_X_COUNT": "%MELODY_X_SOURCE%", "MELODY_X_SOURCE": "-424242"}, projectDirectory)
        applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{
            name: "x",
            keys: []configcontract.EnvironmentKey{environmentKeyCount("")},
            read: func(registrar applicationcontract.ServiceRegistrar) {
                fmt.Println(environmentKeyServicesRanMarker)
            },
        }}

        applicationInstance.Boot()

        return
    }

    childProjectDirectory := t.TempDir()
    command := exec.Command(os.Args[0], "-test.run=^TestBoot_RefusesAModuleKeyOutsideItsRule$")
    command.Env = append(os.Environ(), environmentKeyRefusalChildVariable+"="+childProjectDirectory)

    output, runErr := command.CombinedOutput()
    text := string(output)

    if nil == runErr || false == strings.Contains(text, "after unrecovered error: a module's environment key is refused") {
        t.Fatalf("expected the boot refused over the module's key, got %v: %s", runErr, text[max(0, len(text)-1500):])
    }

    if true == strings.Contains(text, environmentKeyServicesRanMarker) {
        t.Fatalf("expected the refusal before any module registered its services: %s", text)
    }

    journalBytes, readErr := os.ReadFile(filepath.Join(childProjectDirectory, "var", "log", "dev.log"))
    if nil != readErr {
        t.Fatalf("expected the child's journal: %v", readErr)
    }
    journal := string(journalBytes)

    for _, expected := range []string{"invalid environment value", `"environmentKey":"MELODY_X_COUNT"`, `"module":"x"`, `"field":"x.ModuleConfig.Count"`, `"parameterName":"melody.x.count"`, `"bootWarning":"` + internal.BootWarningNameEnvironmentUndeclaredKey + `"`} {
        if false == strings.Contains(journal, expected) {
            t.Fatalf("expected the journal to carry %s, got: %s", expected, journal)
        }
    }

    if true == strings.Contains(journal+text, "424242") {
        t.Fatalf("expected the value of the undeclared source nowhere, got: %s", journal)
    }
}

func TestBoot_AModuleReadsItsKeyInRegisterServices(t *testing.T) {
    for _, testCase := range []struct {
        name         string
        values       map[string]string
        fieldDefault string
        expected     int
    }{
        {"the rule's default", nil, "", 3},
        {"the module's config field", nil, "7", 7},
        {"the .env line over the field", map[string]string{"MELODY_X_COUNT": "9"}, "7", 9},
        {"an empty .env line takes the field", map[string]string{"MELODY_X_COUNT": ""}, "7", 7},
        {"a templated .env line", map[string]string{"MELODY_X_COUNT": "%APP_COUNT%", "APP_COUNT": "9"}, "7", 9},
    } {
        read := 0
        applicationInstance := newEnvironmentKeyBootApplication(t, testCase.values)
        applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{
            name: "x",
            keys: []configcontract.EnvironmentKey{environmentKeyCount(testCase.fieldDefault)},
            read: func(registrar applicationcontract.ServiceRegistrar) {
                resolved, resolveErr := config.ResolveLimitParameter(applicationInstance.configuration.MustGet("melody.x.count"), "MELODY_X_COUNT", environmentKeyCountRule)
                if nil != resolveErr {
                    t.Fatalf("%s: unexpected read error %v", testCase.name, resolveErr)
                }

                read = resolved.Value
            },
        }}

        applicationInstance.Boot()

        if testCase.expected != read {
            t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.expected, read)
        }
    }
}

func TestBoot_AnInactiveModuleDeclaresNothing(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, map[string]string{"MELODY_X_COUNT": "0"})

    applicationInstance.Boot()

    if nil != applicationInstance.configuration.Get("melody.x.count") {
        t.Fatalf("expected no parameter under the name of an inactive module's declaration")
    }

    if parameter := applicationInstance.configuration.Get("MELODY_X_COUNT"); nil == parameter || "0" != parameter.String() || true == parameter.IsDefault() {
        t.Fatalf("expected the key a plain parameter of its own name, got %v", parameter)
    }
}

func TestBoot_NamesTheUndeclaredMelodyKeysInOneBootWarning(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, map[string]string{"MELODY_TYPO": "x", "MELODY_INACTIVE_DSN": "amqp://user:secret@host", "MELODY_X_TEXT": "v", "APP_X": "y"})
    applicationInstance.configuration.RegisterRuntime("app.dsn_reader", "%MELODY_INACTIVE_DSN%")
    applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText()}}}

    applicationInstance.Boot()

    warnings := make([]internal.BootWarning, 0)
    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameEnvironmentUndeclaredKey == bootWarning.Name {
            warnings = append(warnings, bootWarning)
        }
    }

    if 1 != len(warnings) {
        t.Fatalf("expected one warning, got %+v", warnings)
    }

    keys, _ := warnings[0].Context["environmentKeys"].([]string)
    if false == slices.Equal([]string{"MELODY_INACTIVE_DSN", "MELODY_TYPO"}, keys) || 1 != len(warnings[0].Context) {
        t.Fatalf("expected the keys sorted and nothing else in the context, got %v", warnings[0].Context)
    }

    for _, name := range []string{"MELODY_TYPO", "MELODY_INACTIVE_DSN", "app.dsn_reader"} {
        if false == applicationInstance.configuration.Get(name).IsSecret() {
            t.Fatalf("expected %s marked secret", name)
        }
    }

    if true == applicationInstance.configuration.Get("APP_X").IsSecret() || true == applicationInstance.configuration.Get("MELODY_X_TEXT").IsSecret() {
        t.Fatalf("expected an application key and a declared key left unmarked")
    }

    clean := newEnvironmentKeyBootApplication(t, nil)
    clean.Boot()

    for _, bootWarning := range clean.bootWarnings {
        if internal.BootWarningNameEnvironmentUndeclaredKey == bootWarning.Name {
            t.Fatalf("expected no warning without an undeclared key, got %+v", bootWarning)
        }
    }
}

func TestBoot_ACommandLeavesTheUndeclaredKeyWarningOutWhenItIsIgnored(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range []struct {
        name     string
        values   map[string]string
        expected int
    }{
        {"every code", map[string]string{"MELODY_TYPO": "x"}, 1},
        {"the code ignored", map[string]string{"MELODY_TYPO": "x", config.CliBootWarningsIgnoredKey: internal.BootWarningNameEnvironmentUndeclaredKey}, 0},
    } {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.refuseUnknownCliBootWarningCodes()
        cliApplication.collectUndeclaredEnvironmentKeys()

        if runErr := cliApplication.runCli(); nil != runErr {
            t.Fatalf("%s: unexpected run cli error: %v", testCase.name, runErr)
        }

        if warnings := cliLogger.warningsContaining("an environment key in the MELODY_ namespace"); testCase.expected != len(warnings) {
            t.Fatalf("%s: expected the warning written %d times, got %v", testCase.name, testCase.expected, warnings)
        }

        if false == cliApplication.configuration.Get("MELODY_TYPO").IsSecret() {
            t.Fatalf("%s: expected the key marked secret whether or not the warning is written", testCase.name)
        }
    }
}

func TestBoot_AModuleDeclaringOneKeyTwiceIsRefused(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText(), environmentKeyText()}}}

    refusal := refusalOf(t, applicationInstance.bootModuleEnvironmentKeys)
    if nil == refusal || "a module's environment key declaration is refused" != refusal.Message() || "x" != refusal.Context()["module"] || "MELODY_X_TEXT" != refusal.Context()["environmentKey"] {
        t.Fatalf("expected the second declaration refused naming the module and the key, got %v", refusal)
    }

    if cause := errorChainHolds(refusal, "a module declared an environment key another owner already holds"); nil == cause || "x" != cause.Context()["owner"] {
        t.Fatalf("expected the configuration's refusal naming the owner in the chain, got %v", refusal)
    }
}

type configurationWithoutDeclarer struct {
    configcontract.Configuration
}

func TestBoot_RefusesAnEnvironmentKeyModuleOverAConfigurationThatCannotDeclare(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.configuration = configurationWithoutDeclarer{Configuration: applicationInstance.configuration}

    applicationInstance.modules = []applicationcontract.Module{fakeModule{name: "plain"}}
    if refusal := refusalOf(t, func() {
        applicationInstance.bootModuleEnvironmentKeys()
        applicationInstance.refuseModuleEnvironmentKeys()
        applicationInstance.collectUndeclaredEnvironmentKeys()
    }); nil != refusal {
        t.Fatalf("expected a configuration without the capability admitted when no module declares, got %v", refusal)
    }

    applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText()}}}
    refusal := refusalOf(t, applicationInstance.bootModuleEnvironmentKeys)
    if nil == refusal || "the configuration cannot hold the environment keys a module declares" != refusal.Message() {
        t.Fatalf("expected the module refused, got %v", refusal)
    }

    if "x" != refusal.Context()["module"] || "application.configurationWithoutDeclarer" != refusal.Context()["configurationType"] {
        t.Fatalf("expected the module and the configuration's type named, got %v", refusal.Context())
    }
}

func TestBoot_AHandMappingRegisteredBeforeBootIsRefusedNamingTheKey(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, map[string]string{"APP_X_TEXT": "v"})
    applicationInstance.RegisterParameter("melody.x.text", "%APP_X_TEXT%")
    applicationInstance.modules = []applicationcontract.Module{&environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{environmentKeyText()}}}

    refusal := refusalOf(t, applicationInstance.bootModuleEnvironmentKeys)
    cause := errorChainHolds(refusal, "the parameter a module's environment key feeds is already registered")
    if nil == cause || "MELODY_X_TEXT" != cause.Context()["environmentKey"] || "melody.x.text" != cause.Context()["parameterName"] || "x" != cause.Context()["module"] {
        t.Fatalf("expected the hand mapping refused naming the key, the parameter and the module, got %v", refusal)
    }
}

const retiredEnvironmentKeyChildVariable = "MELODY_TEST_RETIRED_ENVIRONMENT_KEY_CHILD"

const retiredEnvironmentKeyHookRanMarker = "a module's parameter hook ran"

type printingParameterModule struct {
    fakeModule
}

func (instance *printingParameterModule) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
    fmt.Println(retiredEnvironmentKeyHookRanMarker)
}

func retiringEnvironmentKeyText() configcontract.EnvironmentKey {
    key := environmentKeyText()
    key.Retired = []string{"MELODY_X_TXT"}

    return key
}

/* Boot exits the process at the refusal, so the boot runs in a child of the test binary and the parent reads its output and its journal */
func bootRetiredEnvironmentKeyChild(t *testing.T, values map[string]string, modules ...applicationcontract.Module) (string, string, bool) {
    t.Helper()

    return bootRetiredEnvironmentKeyChildInMode(t, config.ModeCli, values, modules...)
}

func bootRetiredEnvironmentKeyChildInMode(t *testing.T, mode string, values map[string]string, modules ...applicationcontract.Module) (string, string, bool) {
    t.Helper()

    if projectDirectory := os.Getenv(retiredEnvironmentKeyChildVariable); "" != projectDirectory {
        applicationInstance := newEnvironmentKeyBootApplicationIn(t, values, projectDirectory)
        applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: mode})
        applicationInstance.modules = modules

        applicationInstance.Boot()

        return "", "", true
    }

    childProjectDirectory := t.TempDir()
    command := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
    command.Env = append(os.Environ(), retiredEnvironmentKeyChildVariable+"="+childProjectDirectory)

    output, runErr := command.CombinedOutput()
    if nil == runErr {
        t.Fatalf("expected the child's boot refused, got: %s", output)
    }

    journal, _ := os.ReadFile(filepath.Join(childProjectDirectory, "var", "log", "dev.log"))

    return string(output), string(journal), false
}

func TestBoot_RetiredPublicDirectoryKey_RefusedNamingBothSpellings(t *testing.T) {
    output, _, isChild := bootRetiredEnvironmentKeyChild(t, map[string]string{"MELODY_PUBLIC_DIR": "web"})
    if true == isChild {
        return
    }

    if false == strings.Contains(output, "after unrecovered error: the environment key MELODY_PUBLIC_DIR is retired: rename it to MELODY_PUBLIC_DIRECTORY") {
        t.Fatalf("expected the refusal naming both spellings, got: %s", output[max(0, len(output)-1500):])
    }
}

func TestBoot_RetiredKeyBesideItsSuccessor_Refused(t *testing.T) {
    output, _, isChild := bootRetiredEnvironmentKeyChild(t, map[string]string{"MELODY_PUBLIC_DIR": "web", config.PublicDirectoryKey: "web"})
    if true == isChild {
        return
    }

    if false == strings.Contains(output, "after unrecovered error: the environment key MELODY_PUBLIC_DIR is retired: rename it to MELODY_PUBLIC_DIRECTORY") {
        t.Fatalf("expected the refusal beside the successor, got: %s", output[max(0, len(output)-1500):])
    }
}

func TestBoot_TwoRetiredKeys_OneRefusalNamingBoth(t *testing.T) {
    output, journal, isChild := bootRetiredEnvironmentKeyChild(
        t,
        map[string]string{"MELODY_PUBLIC_DIR": "web", "MELODY_X_TXT": "text"},
        &environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{retiringEnvironmentKeyText()}},
    )
    if true == isChild {
        return
    }

    expected := "after unrecovered error: the environment key MELODY_PUBLIC_DIR is retired: rename it to MELODY_PUBLIC_DIRECTORY; the environment key MELODY_X_TXT is retired: rename it to MELODY_X_TEXT"
    if false == strings.Contains(output, expected) {
        t.Fatalf("expected one refusal naming both keys, got: %s", output[max(0, len(output)-1500):])
    }

    expectedContext := `"context":{"retiredEnvironmentKeys":[{"key":"MELODY_PUBLIC_DIR","owner":"the framework","successor":"MELODY_PUBLIC_DIRECTORY"},{"key":"MELODY_X_TXT","owner":"x","successor":"MELODY_X_TEXT"}]}`
    if false == strings.Contains(journal, expectedContext) {
        t.Fatalf("expected the journal context %s, got: %s", expectedContext, journal)
    }
}

func TestBoot_RetiredKeyRefusal_CarriesNoValue(t *testing.T) {
    output, journal, isChild := bootRetiredEnvironmentKeyChild(
        t,
        map[string]string{"MELODY_PUBLIC_DIR": "s3cr3t-web", "MELODY_X_TXT": "s3cr3t-text"},
        &environmentKeyTestModule{name: "x", keys: []configcontract.EnvironmentKey{retiringEnvironmentKeyText()}},
    )
    if true == isChild {
        return
    }

    if false == strings.Contains(journal, "MELODY_PUBLIC_DIR is retired") {
        t.Fatalf("expected the refusal journaled, got: %s", journal)
    }

    if true == strings.Contains(output+journal, "s3cr3t") {
        t.Fatalf("expected no value in the refusal, got: %s %s", output, journal)
    }
}

func TestBoot_RetiredKey_RefusedBeforeAnyModuleHook(t *testing.T) {
    output, _, isChild := bootRetiredEnvironmentKeyChild(t, map[string]string{"MELODY_PUBLIC_DIR": "web"}, &printingParameterModule{fakeModule: fakeModule{name: "printing"}})
    if true == isChild {
        return
    }

    if false == strings.Contains(output, "MELODY_PUBLIC_DIR is retired") || true == strings.Contains(output, retiredEnvironmentKeyHookRanMarker) {
        t.Fatalf("expected the refusal before the parameter hook, got: %s", output[max(0, len(output)-1500):])
    }
}

func TestBoot_RetiredKey_RefusedInHttpModeBeforeTheCliKeyRefusal(t *testing.T) {
    output, _, isChild := bootRetiredEnvironmentKeyChildInMode(t, config.ModeHttp, map[string]string{"MELODY_PUBLIC_DIR": "web", config.CliBootWarningsDisabledKey: "true"})
    if true == isChild {
        return
    }

    if false == strings.Contains(output, "after unrecovered error: the environment key MELODY_PUBLIC_DIR is retired") || true == strings.Contains(output, "a cli boot warning key is set in an http process") {
        t.Fatalf("expected the retired refusal in http mode, ahead of the cli key refusal, got: %s", output[max(0, len(output)-1500):])
    }
}

func TestBoot_RetiredKeyOfAnUnregisteredModule_StaysTheUndeclaredWarning(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, map[string]string{"MELODY_X_TXT": "text"})

    applicationInstance.Boot()

    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameEnvironmentUndeclaredKey == bootWarning.Name {
            if keys, _ := bootWarning.Context["environmentKeys"].([]string); true == slices.Equal([]string{"MELODY_X_TXT"}, keys) {
                return
            }
        }
    }

    t.Fatalf("expected the undeclared-key warning naming the key, got %+v", applicationInstance.bootWarnings)
}

func TestRetiredEnvironmentKeysMessage_JoinsInTheGivenOrder(t *testing.T) {
    message := retiredEnvironmentKeysMessage([]configcontract.RetiredEnvironmentKey{
        {Key: "MELODY_A", Successor: "MELODY_A_NEW", Owner: "a"},
        {Key: "MELODY_B", Successor: "MELODY_B_NEW", Owner: "b"},
    })

    if "the environment key MELODY_A is retired: rename it to MELODY_A_NEW; the environment key MELODY_B is retired: rename it to MELODY_B_NEW" != message {
        t.Fatalf("unexpected message %q", message)
    }
}
