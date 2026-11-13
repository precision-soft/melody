package cron

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

func newDeclaringConfiguration(t *testing.T, values map[string]string, defaults KeyDefaults) *melodyconfig.Configuration {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    for _, key := range EnvironmentKeys(defaults) {
        if declareErr := configuration.DeclareEnvironmentKey("cron", key); nil != declareErr {
            t.Fatalf("unexpected declaration error for %s: %v", key.Key, declareErr)
        }
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    return configuration
}

func TestEnvironmentKeys_DeclaresTheTenKeysAndTheirParameters(t *testing.T) {
    expected := []struct {
        key       string
        parameter string
        field     string
        value     string
        ruled     bool
    }{
        {KeyUser, ParameterUser, "cron.KeyDefaults.User", "", false},
        {KeyLogsDirectory, ParameterLogsDirectory, "cron.KeyDefaults.LogsDirectory", "%kernel.logs_dir%/cron", false},
        {KeyBinary, ParameterBinary, "cron.KeyDefaults.Binary", "", false},
        {KeyDestinationFile, ParameterDestinationFile, "cron.KeyDefaults.DestinationFile", "%kernel.project_dir%/generated_conf/cron/crontab", false},
        {KeyHeartbeatPath, ParameterHeartbeatPath, "cron.KeyDefaults.HeartbeatPath", "", false},
        {KeyHeartbeatEnabled, ParameterHeartbeatAutoEnabled, "cron.KeyDefaults.HeartbeatEnabled", "", true},
        {KeyTemplate, ParameterTemplate, "cron.KeyDefaults.Template", TemplateNameCrontab, false},
        {KeyImage, ParameterImage, "cron.KeyDefaults.Image", "", false},
        {KeyNamespace, ParameterNamespace, "cron.KeyDefaults.Namespace", "", true},
        {KeyRestartPolicy, ParameterRestartPolicy, "cron.KeyDefaults.RestartPolicy", "", true},
    }

    keys := EnvironmentKeys(KeyDefaults{})
    if len(expected) != len(keys) {
        t.Fatalf("expected %d keys, got %d", len(expected), len(keys))
    }

    for index, want := range expected {
        got := keys[index]
        if want.key != got.Key || want.parameter != got.Parameter || want.field != got.Field || want.value != got.Default || want.ruled != (nil != got.Rule) || true == got.Secret {
            t.Fatalf("key %d: expected %+v, got %+v", index, want, got)
        }
    }

    for index, description := range map[int]string{5: "switch", 8: "rfc 1123 label", 9: "choice OnFailure|Never"} {
        if description != keys[index].Rule.Describe() {
            t.Fatalf("%s: expected the rule %q, got %q", keys[index].Key, description, keys[index].Rule.Describe())
        }
    }
}

func TestEnvironmentKeys_TheLogsDirectoryIsSpelledOut(t *testing.T) {
    if "MELODY_CRON_LOGS_DIRECTORY" != KeyLogsDirectory || "melody.cron.logs_directory" != ParameterLogsDirectory || "logs-directory" != flagNameLogsDirectory {
        t.Fatalf("expected the spelled-out key, parameter and flag, got %q, %q, %q", KeyLogsDirectory, ParameterLogsDirectory, flagNameLogsDirectory)
    }

    for _, key := range EnvironmentKeys(KeyDefaults{}) {
        if "MELODY_CRON_LOGS_DIR" == key.Key || "melody.cron.logs_dir" == key.Parameter {
            t.Fatalf("expected the abbreviated spelling declared by nothing, got %+v", key)
        }
    }

    configuration := newDeclaringConfiguration(t, map[string]string{"MELODY_CRON_LOGS_DIRECTORY": "/var/log/app"}, KeyDefaults{})
    if value := configuration.MustGet(ParameterLogsDirectory).MustString(); "/var/log/app" != value {
        t.Fatalf("expected the key read into the parameter, got %q", value)
    }
}

func TestEnvironmentKeys_AKeyDefaultsFieldReplacesTheModuleDefault(t *testing.T) {
    keys := EnvironmentKeys(KeyDefaults{
        User:             "www-data",
        LogsDirectory:    "/var/log/app",
        Binary:           "/usr/local/bin/app",
        DestinationFile:  "/etc/cron.d/app",
        HeartbeatPath:    "/var/run/heartbeat",
        HeartbeatEnabled: configcontract.Switch{Value: false, IsSet: true},
        Template:         TemplateNameK8s,
        Image:            "registry/app:1",
        Namespace:        "jobs",
        RestartPolicy:    "Never",
    })

    for index, want := range []string{"www-data", "/var/log/app", "/usr/local/bin/app", "/etc/cron.d/app", "/var/run/heartbeat", "false", TemplateNameK8s, "registry/app:1", "jobs", "Never"} {
        if want != keys[index].Default {
            t.Fatalf("key %s: expected default %q, got %q", keys[index].Key, want, keys[index].Default)
        }
    }

    if heartbeat := EnvironmentKeys(KeyDefaults{HeartbeatEnabled: configcontract.Switch{Value: true, IsSet: true}})[5]; "true" != heartbeat.Default {
        t.Fatalf("expected a set heartbeat switch rendered true, got %q", heartbeat.Default)
    }
}

func TestEnvironmentKeys_EveryDefaultPassesItsOwnRule(t *testing.T) {
    for _, defaults := range []KeyDefaults{{}, {HeartbeatEnabled: configcontract.Switch{Value: true, IsSet: true}, Namespace: "jobs", RestartPolicy: "OnFailure"}} {
        for _, key := range EnvironmentKeys(defaults) {
            if nil == key.Rule {
                continue
            }

            if checkErr := key.Rule.Check(key.Default, key.Field); nil != checkErr {
                t.Fatalf("%s: the default %q is refused by its own rule: %v", key.Key, key.Default, checkErr)
            }
        }
    }
}

func TestNamespaceRule_ReadsTheRfc1123Label(t *testing.T) {
    rule := namespaceKeyRule{}

    for _, text := range []string{"", "jobs", "team-a-jobs", "a1"} {
        if checkErr := rule.Check(text, "cron.KeyDefaults.Namespace"); nil != checkErr {
            t.Fatalf("%q: unexpected refusal %v", text, checkErr)
        }
    }

    for _, text := range []string{"Jobs", "-jobs", "jobs-", "team_a", " jobs"} {
        checkErr := rule.Check(text, "cron.KeyDefaults.Namespace")
        refusal, isRefusal := checkErr.(*exception.Error)
        if false == isRefusal || "cron.KeyDefaults.Namespace" != refusal.Context()["field"] || false == errors.Is(checkErr, ErrK8sInvalidNamespace) {
            t.Fatalf("%q: expected a refusal naming the field, got %v", text, checkErr)
        }
    }
}

func TestEnvironmentKeys_TheBootCheckRefusesARestartPolicyNamingTheField(t *testing.T) {
    configuration := newDeclaringConfiguration(t, map[string]string{KeyRestartPolicy: "Always"}, KeyDefaults{})

    checkErr := configuration.CheckEnvironmentKeys()
    refusal, isRefusal := checkErr.(*exception.Error)
    if false == isRefusal || "invalid environment value" != refusal.Message() || KeyRestartPolicy != refusal.Context()["environmentKey"] || "cron.KeyDefaults.RestartPolicy" != refusal.Context()["field"] || "cron" != refusal.Context()["module"] {
        t.Fatalf("expected the restart policy refused naming the key, the field and the module, got %v", checkErr)
    }

    if clean := newDeclaringConfiguration(t, map[string]string{KeyRestartPolicy: "Never", KeyHeartbeatEnabled: "true", KeyNamespace: "jobs"}, KeyDefaults{}); nil != clean.CheckEnvironmentKeys() {
        t.Fatalf("expected admitted values to pass, got %v", clean.CheckEnvironmentKeys())
    }
}

func TestEnvironmentKeys_TheGeneratorReadsTheDeclaredDefaults(t *testing.T) {
    configuration := newDeclaringConfiguration(t, map[string]string{KeyUser: "apache"}, KeyDefaults{})

    if "apache" != parameterValue(configuration, ParameterUser) || TemplateNameCrontab != parameterValue(configuration, ParameterTemplate) {
        t.Fatalf("expected the .env user and the declared template, got %q %q", parameterValue(configuration, ParameterUser), parameterValue(configuration, ParameterTemplate))
    }

    enabled, enabledErr := isHeartbeatAutoEnabled(configuration)
    if nil != enabledErr || true == enabled {
        t.Fatalf("expected the empty declared heartbeat switch read as off, got %v %v", enabled, enabledErr)
    }
}

func TestEnvironmentKeys_TheGeneratorRunsOverTheDeclaredKeys(t *testing.T) {
    configuration := newDeclaringConfiguration(t, map[string]string{KeyUser: "apache", KeyHeartbeatEnabled: "true"}, KeyDefaults{})

    projectDirectory := configuration.Kernel().ProjectDir()
    commands := []clicontract.Command{newFakeCommandWithSchedule("backup:run", &testSchedule{Minute: "0", Hour: "2"})}

    if _, runErr := runGenerateCommandWithConfiguration(t, commands, []string{"--binary", "/usr/local/bin/fakeapp"}, configuration); nil != runErr {
        t.Fatalf("unexpected generation error: %v", runErr)
    }

    content, readErr := os.ReadFile(filepath.Join(projectDirectory, "generated_conf", "cron", "crontab"))
    if nil != readErr {
        t.Fatalf("expected the crontab at the declared destination: %v", readErr)
    }

    logsDirectory := filepath.Join(configuration.Kernel().LogsDir(), "cron")
    for _, expected := range []string{"0 2 * * * apache /usr/local/bin/fakeapp backup:run", filepath.Join(logsDirectory, "backup-run.log"), "apache /bin/touch " + filepath.Join(logsDirectory, "heartbeat.crontab")} {
        if false == strings.Contains(string(content), expected) {
            t.Fatalf("expected %q in the crontab, got:\n%s", expected, content)
        }
    }
}

