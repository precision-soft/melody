package config

import (
    "context"
    "errors"
    "testing"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func newOverriddenConfiguration(values map[string]string, keys []configcontract.EnvironmentKey, overrides map[string]string) (*Configuration, error) {
    environment, environmentErr := NewEnvironment(context.Background(), &testEnvironmentSource{values: values})
    if nil != environmentErr {
        return nil, environmentErr
    }

    return NewConfiguration(environment, "/tmp/melody", ConfigurationConfig{EnvironmentKeys: keys, ParameterOverrides: overrides})
}

func mustOverriddenConfiguration(t *testing.T, values map[string]string, keys []configcontract.EnvironmentKey, overrides map[string]string) *Configuration {
    t.Helper()

    configuration, configurationErr := newOverriddenConfiguration(values, keys, overrides)
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return configuration
}

func refusalInChain(t *testing.T, err error, message string) *exception.Error {
    t.Helper()

    for current := err; nil != current; current = errors.Unwrap(current) {
        refusal, isRefusal := current.(*exception.Error)
        if true == isRefusal && message == refusal.Message() {
            return refusal
        }
    }

    t.Fatalf("expected %q in the chain, got %v", message, err)

    return nil
}

func httpAddressOverride() map[string]string {
    return map[string]string{KernelHttpAddress: "%env(APP_HTTP_ADDRESS)%"}
}

func TestParameterOverride_KernelParameterReadsTheApplicationKey(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{"APP_HTTP_ADDRESS": ":9090"},
        []configcontract.EnvironmentKey{applicationKey("APP_HTTP_ADDRESS", "app.http_address", ":7070")},
        httpAddressOverride(),
    )

    if ":9090" != configuration.Http().Address() {
        t.Fatalf("expected the application key's line, got %q", configuration.Http().Address())
    }
}

func TestParameterOverride_KernelParameterReadsTheApplicationKeysDefault(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{},
        []configcontract.EnvironmentKey{applicationKey("APP_HTTP_ADDRESS", "app.http_address", ":7070")},
        httpAddressOverride(),
    )

    if ":7070" != configuration.Http().Address() {
        t.Fatalf("expected the application key's default, got %q", configuration.Http().Address())
    }
}

func TestParameterOverride_Literal(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{KernelLogsDirectory: "/var/log/app"})

    if "/var/log/app" != configuration.Kernel().LogsDirectory() {
        t.Fatalf("expected the literal, got %q", configuration.Kernel().LogsDirectory())
    }
}

func TestParameterOverride_ReadsAnotherKernelParameter(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{},
        nil,
        map[string]string{KernelLogsDirectory: "/var/log/app", KernelCacheDirectory: "%" + KernelLogsDirectory + "%/c"},
    )

    if "/var/log/app/c" != configuration.Kernel().CacheDirectory() {
        t.Fatalf("expected the kernel parameter read, got %q", configuration.Kernel().CacheDirectory())
    }
}

func TestParameterOverride_RuleJudgesTheResolvedValue(t *testing.T) {
    _, limitErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{KernelHttpReadTimeout: "0s"})
    limitRefusal := refusalInChain(t, limitErr, "invalid environment value")
    if KernelHttpReadTimeout != limitRefusal.Context()["parameterName"] || true != limitRefusal.Context()["overridden"] {
        t.Fatalf("expected the overridden parameter named, got %v", limitRefusal.Context())
    }

    _, switchErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{KernelHttpSessionEmptyOnOutage: "maybe"})
    switchRefusal := refusalInChain(t, switchErr, "invalid environment value")
    if KernelHttpSessionEmptyOnOutage != switchRefusal.Context()["parameterName"] || true != switchRefusal.Context()["overridden"] {
        t.Fatalf("expected the overridden switch named, got %v", switchRefusal.Context())
    }

    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.policy": "Always"})
    mustDeclare(t, configuration, "x", configcontract.EnvironmentKey{Key: "MELODY_X_POLICY", Parameter: "melody.x.policy", Field: "x.F", Default: "Never", Rule: ChoiceKeyRule([]string{"OnFailure", "Never"})})
    mustResolve(t, configuration)

    choiceRefusal := refusalInChain(t, configuration.CheckEnvironmentKeys(), "invalid environment value")
    if "melody.x.policy" != choiceRefusal.Context()["parameterName"] || true != choiceRefusal.Context()["overridden"] {
        t.Fatalf("expected the overridden module parameter named, got %v", choiceRefusal.Context())
    }
}

func TestParameterOverride_EmptyResolutionTakesTheRuleDefault(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{"APP_EMPTY": ""},
        []configcontract.EnvironmentKey{applicationKey("APP_EMPTY", "app.empty", "")},
        map[string]string{KernelHttpReadTimeout: "%env(APP_EMPTY)%"},
    )

    if false == configuration.Http().ReadTimeout().IsAbsent() {
        t.Fatalf("expected the limit absent, got %v", configuration.Http().ReadTimeout())
    }
}

func TestParameterOverride_RefusesEnvironmentAndProjectDirectory(t *testing.T) {
    for name, message := range map[string]string{
        KernelEnvironment:      "kernel.environment cannot be overridden: it selected the .env.<name> files before the configuration existed; set MELODY_ENV in .env or .env.local",
        KernelProjectDirectory: "kernel.project_directory cannot be overridden: it is the path anchor the .env artifacts were read from",
    } {
        _, overrideErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{name: "x"})
        declarationRefusal(t, overrideErr, message)
    }
}

func TestParameterOverride_RefusesAnUnknownKernelName(t *testing.T) {
    _, overrideErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{"kernel.no_such": "x"})
    refusal := declarationRefusal(t, overrideErr, "a parameter override names a kernel parameter that does not exist")
    if "kernel.no_such" != refusal.Context()["parameterName"] {
        t.Fatalf("expected the name in the context, got %v", refusal.Context())
    }
}

func TestParameterOverride_AnEmptyOrPaddedName(t *testing.T) {
    for _, name := range []string{"", " ", " melody.x.text"} {
        _, overrideErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{name: "x"})
        declarationRefusal(t, overrideErr, "a parameter override names an empty or padded parameter")
    }
}

func TestParameterOverride_TheParameterOfAnApplicationKey(t *testing.T) {
    _, overrideErr := newOverriddenConfiguration(
        map[string]string{},
        []configcontract.EnvironmentKey{applicationKey("APP_TEXT", "app.text", "")},
        map[string]string{"app.text": "x"},
    )
    refusal := declarationRefusal(t, overrideErr, "the application overrides the parameter of its own environment key; write the template as the key's Default")
    if "app.text" != refusal.Context()["parameterName"] || "APP_TEXT" != refusal.Context()["environmentKey"] {
        t.Fatalf("expected the parameter and the key named, got %v", refusal.Context())
    }
}

func TestParameterOverride_KernelOverrideReadingAnUndefinedParameterRefusesAtConstruction(t *testing.T) {
    _, overrideErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{KernelHttpAddress: "%app.later%"})
    refusal := refusalInChain(t, overrideErr, "failed to resolve parameter")
    if KernelHttpAddress != refusal.Context()["parameterName"] || true != refusal.Context()["overridden"] {
        t.Fatalf("expected the overridden kernel parameter named, got %v", refusal.Context())
    }
}

func TestParameterOverride_ModuleParameterIsOverriddenWhenItsKeyIsDeclared(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "%app.user%-x"})

    if nil != configuration.Get("melody.x.text") {
        t.Fatalf("expected no parameter before the declaration")
    }

    mustDeclare(t, configuration, "x", textKey("from-key"))
    configuration.RegisterRuntime("app.user", "cron")
    mustResolve(t, configuration)

    parameter := configuration.MustGet("melody.x.text")
    if "cron-x" != parameter.String() || false == parameter.IsOverridden() {
        t.Fatalf("expected the override applied, got %q (overridden %t)", parameter.String(), parameter.IsOverridden())
    }

    if "from-key" != configuration.MustGet("MELODY_X_TEXT").String() {
        t.Fatalf("expected the key's name to keep reading the key, got %q", configuration.MustGet("MELODY_X_TEXT").String())
    }
}

func TestUnappliedParameterOverrides_NamesWhatNoKeyConsumed(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "a", "app.typo": "b", "melody.a.b": "c"})

    unapplied := configuration.UnappliedParameterOverrides()
    if 3 != len(unapplied) || "app.typo" != unapplied[0] || "melody.a.b" != unapplied[1] || "melody.x.text" != unapplied[2] {
        t.Fatalf("expected the three names sorted, got %v", unapplied)
    }

    mustDeclare(t, configuration, "x", textKey(""))

    unapplied = configuration.UnappliedParameterOverrides()
    if 2 != len(unapplied) || "app.typo" != unapplied[0] {
        t.Fatalf("expected the consumed name gone, got %v", unapplied)
    }
}

func TestOverriddenEnvironmentKeys_SetKeyIsListed(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{"MELODY_HTTP_ADDRESS": ":8080"}, nil, map[string]string{KernelHttpAddress: ":9090"})

    overridden := configuration.OverriddenEnvironmentKeys()
    if 1 != len(overridden) || (configcontract.OverriddenEnvironmentKey{Key: "MELODY_HTTP_ADDRESS", Parameter: KernelHttpAddress, Owner: frameworkEnvironmentKeyOwner}) != overridden[0] {
        t.Fatalf("expected the framework key listed, got %v", overridden)
    }
}

func TestOverriddenEnvironmentKeys_EmptyLineIsListed(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{"MELODY_HTTP_ADDRESS": ""}, nil, map[string]string{KernelHttpAddress: ":9090"})

    if 1 != len(configuration.OverriddenEnvironmentKeys()) {
        t.Fatalf("expected an empty line to count, got %v", configuration.OverriddenEnvironmentKeys())
    }
}

func TestOverriddenEnvironmentKeys_AbsentKeyIsNot(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{KernelHttpAddress: ":9090"})

    if 0 != len(configuration.OverriddenEnvironmentKeys()) {
        t.Fatalf("expected nothing listed, got %v", configuration.OverriddenEnvironmentKeys())
    }
}

func TestOverriddenEnvironmentKeys_OverrideReadingItsOwnKeyIsNot(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{"MELODY_HTTP_ADDRESS": "host"}, nil, map[string]string{KernelHttpAddress: "%env(MELODY_HTTP_ADDRESS)%:9090"})

    if 0 != len(configuration.OverriddenEnvironmentKeys()) {
        t.Fatalf("expected the self-read exempt, got %v", configuration.OverriddenEnvironmentKeys())
    }

    if "host:9090" != configuration.Http().Address() {
        t.Fatalf("expected the override to read its key, got %q", configuration.Http().Address())
    }
}

func TestOverriddenEnvironmentKeys_SortedAndNamesNoValue(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{"MELODY_HTTP_ADDRESS": ":8080", "MELODY_X_TEXT": "secret-ish"},
        nil,
        map[string]string{KernelHttpAddress: ":9090", "melody.x.text": "x"},
    )
    mustDeclare(t, configuration, "x", textKey(""))

    overridden := configuration.OverriddenEnvironmentKeys()
    if 2 != len(overridden) || "MELODY_HTTP_ADDRESS" != overridden[0].Key || "MELODY_X_TEXT" != overridden[1].Key || "x" != overridden[1].Owner || "melody.x.text" != overridden[1].Parameter {
        t.Fatalf("expected both keys sorted, got %v", overridden)
    }
}

func TestSecretKey_OverrideToASecretKeyIsSecret(t *testing.T) {
    passwordKey := applicationKey("APP_PASSWORD", "app.password", "")
    passwordKey.Secret = true

    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{"APP_PASSWORD": "pw"},
        []configcontract.EnvironmentKey{passwordKey},
        map[string]string{KernelLogsDirectory: "/var/%env(APP_PASSWORD)%", "melody.x.text": "%env(APP_PASSWORD)%"},
    )
    mustDeclare(t, configuration, "x", textKey(""))

    if false == configuration.MustGet(KernelLogsDirectory).IsSecret() || false == configuration.MustGet("melody.x.text").IsSecret() {
        t.Fatalf("expected both overrides secret")
    }

    if true == configuration.MustGet("MELODY_X_TEXT").IsSecret() {
        t.Fatalf("expected the key the override stopped reading left unmarked")
    }
}

func TestSecretKey_AnOverriddenSecretModuleParameterStaysSecret(t *testing.T) {
    configuration := mustOverriddenConfiguration(
        t,
        map[string]string{"APP_DB_PASSWORD": "pw"},
        []configcontract.EnvironmentKey{applicationKey("APP_DB_PASSWORD", "app.db_password", "")},
        map[string]string{"melody.x.password": "%env(APP_DB_PASSWORD)%"},
    )
    configuration.RegisterRuntime("app.reader", "%melody.x.password%")
    mustDeclare(t, configuration, "x", secretKey())
    mustResolve(t, configuration)

    if false == configuration.MustGet("melody.x.password").IsSecret() || false == configuration.MustGet("app.reader").IsSecret() {
        t.Fatalf("expected the overridden secret parameter and its reader redacted")
    }

    if true == configuration.MustGet("app.db_password").IsSecret() {
        t.Fatalf("expected the mark not to travel back to the source the override reads")
    }
}

func TestSecretKey_AnOverriddenParameterOfANonSecretKeyIsNotMarked(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "literal"})
    mustDeclare(t, configuration, "x", textKey(""))

    if true == configuration.MustGet("melody.x.text").IsSecret() {
        t.Fatalf("expected no mark")
    }
}

func TestParameter_OverriddenIsNotDefaultAndKeepsItsKey(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{KernelHttpAddress: ":9090"})

    parameter := configuration.MustGet(KernelHttpAddress)
    if false == parameter.IsOverridden() || true == parameter.IsDefault() || "MELODY_HTTP_ADDRESS" != parameter.EnvironmentKey() || ":9090" != parameter.Template() {
        t.Fatalf("expected an overridden parameter keeping its key, got overridden %t default %t key %q template %v", parameter.IsOverridden(), parameter.IsDefault(), parameter.EnvironmentKey(), parameter.Template())
    }

    if parameter := configuration.MustGet(KernelHttpReadTimeout); true == parameter.IsOverridden() || false == parameter.IsDefault() {
        t.Fatalf("expected a parameter left alone unchanged")
    }
}

func TestParameterOverride_CliNameAndDescriptionReachTheView(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{KernelCliName: "shop", KernelCliDescription: "the shop"})

    if "shop" != configuration.Cli().Name() || "the shop" != configuration.Cli().Description() {
        t.Fatalf("expected the overridden cli name and description, got %q %q", configuration.Cli().Name(), configuration.Cli().Description())
    }
}

func TestParameterOverride_KernelOverrideReadingAnUndeclaredAbsentKeyRefusesAtConstruction(t *testing.T) {
    _, overrideErr := newOverriddenConfiguration(map[string]string{}, nil, map[string]string{KernelHttpAddress: "%env(APP_TYPO)%"})
    refusal := refusalInChain(t, overrideErr, "failed to resolve parameter")
    if KernelHttpAddress != refusal.Context()["parameterName"] || true != refusal.Context()["overridden"] {
        t.Fatalf("expected the overridden kernel parameter named, got %v", refusal.Context())
    }
}

func TestParameterOverride_ModuleOverrideReadingAnUndeclaredKeyRefusesAtTheResolve(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "%env(APP_TYPO)%"})
    mustDeclare(t, configuration, "x", textKey(""))

    refusal := refusalInChain(t, configuration.Resolve(), "failed to resolve parameter")
    if "melody.x.text" != refusal.Context()["parameterName"] || true != refusal.Context()["overridden"] {
        t.Fatalf("expected the overridden module parameter named, got %v", refusal.Context())
    }
}

func TestParameterOverride_ARefusedOverrideLeavesTheTablesAsTheyWere(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{"MELODY_X_TEXT": "%app.later%"}, nil, map[string]string{"melody.x.text": "%env("})
    configuration.RegisterRuntime("app.later", "now")

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", textKey("")), "a declared environment key could not be resolved at its declaration")
    if true != refusal.Context()["overridden"] {
        t.Fatalf("expected the override named as the failing text, got %v", refusal.Context())
    }

    loaded := configuration.parameters["MELODY_X_TEXT"]
    if nil == loaded || "%app.later%" != loaded.template || false == loaded.deferred.Load() {
        t.Fatalf("expected the loaded parameter untouched, still deferred")
    }

    if nil != configuration.Get("melody.x.text") || 1 != len(configuration.UnappliedParameterOverrides()) {
        t.Fatalf("expected no parameter and the override still pending")
    }
}

func TestParameterOverride_TheKeysOwnTextFailingIsNotTheOverride(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "literal"})

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", textKey("%env(")), "a declared environment key could not be resolved at its declaration")
    if _, isMarked := refusal.Context()["overridden"]; true == isMarked {
        t.Fatalf("expected the key's own text not blamed on the override, got %v", refusal.Context())
    }
}

func TestParameterOverride_ASecretValueRefusedByARuleIsWithheld(t *testing.T) {
    tokenKey := applicationKey("APP_TOKEN", "app.token", "")
    tokenKey.Secret = true

    _, overrideErr := newOverriddenConfiguration(
        map[string]string{"APP_TOKEN": "s3cr3t"},
        []configcontract.EnvironmentKey{tokenKey},
        map[string]string{KernelHttpReadTimeout: "%env(APP_TOKEN)%", KernelHttpSessionEmptyOnOutage: "%env(APP_TOKEN)%"},
    )
    refusal := refusalInChain(t, overrideErr, "invalid environment value")
    if nil != refusal.CauseErr() {
        t.Fatalf("expected the cause quoting the secret withheld, got %v", refusal.CauseErr())
    }

    _, switchErr := newOverriddenConfiguration(
        map[string]string{"APP_TOKEN": "s3cr3t"},
        []configcontract.EnvironmentKey{tokenKey},
        map[string]string{KernelHttpSessionEmptyOnOutage: "%env(APP_TOKEN)%"},
    )
    if switchRefusal := refusalInChain(t, switchErr, "invalid environment value"); nil != switchRefusal.CauseErr() {
        t.Fatalf("expected the switch cause withheld, got %v", switchRefusal.CauseErr())
    }
}

func TestParameterOverride_AConversionErrorNamesTheOverriddenParameter(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "abc"})
    mustDeclare(t, configuration, "x", textKey(""))

    _, intErr := configuration.MustGet("melody.x.text").Int()
    refusal := refusalInChain(t, intErr, "parameter is not a valid 'int'")
    if "melody.x.text" != refusal.Context()["parameterName"] {
        t.Fatalf("expected the parse error to name the overridden parameter, got %v", refusal.Context())
    }
}

func TestEnvironmentKeyParameter_AnswersFrameworkModuleAndApplicationKeys(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, []configcontract.EnvironmentKey{applicationKey("APP_TEXT", "app.text", "")}, nil)
    mustDeclare(t, configuration, "x", textKey(""))

    for key, parameterName := range map[string]string{"MELODY_HTTP_ADDRESS": KernelHttpAddress, "MELODY_X_TEXT": "melody.x.text", "APP_TEXT": "app.text"} {
        if answered, isDeclared := configuration.EnvironmentKeyParameter(key); false == isDeclared || parameterName != answered {
            t.Fatalf("expected %s to feed %s, got %q %t", key, parameterName, answered, isDeclared)
        }
    }

    if _, isDeclared := configuration.EnvironmentKeyParameter("APP_UNKNOWN"); true == isDeclared {
        t.Fatalf("expected an unknown key answered false")
    }
}

func TestSecretKey_AMarkOnTheKeysValueDoesNotHopToTheOverride(t *testing.T) {
    configuration := mustOverriddenConfiguration(t, map[string]string{}, nil, map[string]string{"melody.x.text": "literal"})
    configuration.RegisterRuntime("app.reader", "%melody.x.text%")
    mustDeclare(t, configuration, "x", textKey(""))
    configuration.MarkSecret("MELODY_X_TEXT")
    mustResolve(t, configuration)

    if true == configuration.MustGet("melody.x.text").IsSecret() || true == configuration.MustGet("app.reader").IsSecret() {
        t.Fatalf("expected a mark on the key's value not to reach the override that does not read it")
    }
}
