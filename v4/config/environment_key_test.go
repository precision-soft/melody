package config

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/configseam"
)

var countKeyRule = bound.Rule[int]{Name: "ignored", Default: 3, Minimum: 1}

func countKey() configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_COUNT",
        Parameter: "melody.x.count",
        Field:     "x.ModuleConfig.Count",
        Rule:      LimitKeyRule(countKeyRule),
    }
}

func textKey(defaultText string) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_TEXT",
        Parameter: "melody.x.text",
        Field:     "x.ModuleConfig.Text",
        Default:   defaultText,
    }
}

func secretKey() configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_PASSWORD",
        Parameter: "melody.x.password",
        Field:     "x.ModuleConfig.Password",
        Secret:    true,
    }
}

func mustDeclare(t *testing.T, configuration *Configuration, owner string, key configcontract.EnvironmentKey) {
    t.Helper()

    if declareErr := configuration.DeclareEnvironmentKey(owner, key); nil != declareErr {
        t.Fatalf("unexpected declaration error: %v", declareErr)
    }
}

func declarationRefusal(t *testing.T, err error, message string) *exception.Error {
    t.Helper()

    refusal, isRefusal := err.(*exception.Error)
    if false == isRefusal || message != refusal.Message() {
        t.Fatalf("expected %q, got %v", message, err)
    }

    return refusal
}

func mustResolve(t *testing.T, configuration *Configuration) {
    t.Helper()

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }
}

func TestDeclareEnvironmentKey_AnAbsentKeyTakesTheDefault(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", textKey("fallback"))
    mustResolve(t, configuration)

    if nil != configuration.Get("MELODY_X_TEXT") {
        t.Fatalf("expected no parameter under the key")
    }

    byParameter := configuration.Get("melody.x.text")
    if "fallback" != byParameter.String() || false == byParameter.IsDefault() || "MELODY_X_TEXT" != byParameter.EnvironmentKey() {
        t.Fatalf("expected the default, got %q default=%v key=%q", byParameter.String(), byParameter.IsDefault(), byParameter.EnvironmentKey())
    }
}

func TestDeclareEnvironmentKey_APresentKeyWinsOverTheDefault(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TEXT": "from env"})

    mustDeclare(t, configuration, "x", textKey("fallback"))
    mustResolve(t, configuration)

    byParameter := configuration.Get("melody.x.text")

    if "from env" != byParameter.String() || true == byParameter.IsDefault() {
        t.Fatalf("expected the .env value, got %q default=%v", byParameter.String(), byParameter.IsDefault())
    }
}

func TestDeclareEnvironmentKey_ATemplatedDefaultResolvesAtTheDeclaration_AForwardReferenceAtBoot(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", textKey("%kernel.logs_directory%/x"))

    expected := configuration.Get(KernelLogsDirectory).String() + "/x"
    if expected != configuration.Get("melody.x.text").String() {
        t.Fatalf("expected %q right after the declaration, got %q", expected, configuration.Get("melody.x.text").String())
    }

    forward := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, forward, "x", textKey("%app.later%/x"))

    if refusal := refusalOfRead(func() { _ = forward.Get("melody.x.text").String() }); nil == refusal || false == strings.Contains(refusal.Message(), "deferred to boot") {
        t.Fatalf("expected the read of a forward reference refused before the boot resolve, got %v", refusal)
    }

    forward.RegisterRuntime("app.later", "later")
    mustResolve(t, forward)

    if "later/x" != forward.Get("melody.x.text").String() {
        t.Fatalf("expected the boot pass to settle the forward reference, got %q", forward.Get("melody.x.text").String())
    }
}

func refusalOfRead(read func()) (refusal *exception.Error) {
    defer func() {
        refusal, _ = recover().(*exception.Error)
    }()

    read()

    return nil
}

func TestDeclareEnvironmentKey_AReferenceByKeyOrByParameterResolvesWithoutTheLine(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    configuration.RegisterRuntime("app.by_key", "[%env(MELODY_X_TEXT)%]")
    configuration.RegisterRuntime("app.by_parameter", "[%melody.x.text%]")
    mustDeclare(t, configuration, "x", textKey("fallback"))
    mustResolve(t, configuration)

    for _, name := range []string{"app.by_key", "app.by_parameter"} {
        if "[fallback]" != configuration.Get(name).String() {
            t.Fatalf("%s: expected the default, got %q", name, configuration.Get(name).String())
        }
    }

    environmentReader := mustConfigurationFromValues(t, map[string]string{})
    environmentReader.RegisterRuntime("app.by_environment", "%env(MELODY_X_TEXT)%")
    mustDeclare(t, environmentReader, "x", textKey("fallback"))
    mustResolve(t, environmentReader)

    if "fallback" != environmentReader.Get("app.by_environment").String() {
        t.Fatalf("expected %%env(MELODY_X_TEXT)%% to read the declared default, where no file set the key, got %q", environmentReader.Get("app.by_environment").String())
    }
}

func TestDeclareEnvironmentKey_ASecretKeyMarksItsParameterAndEveryReader(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_PASSWORD": "hunter2"})
    configuration.RegisterRuntime("app.before_by_key", "u:%env(MELODY_X_PASSWORD)%")
    configuration.RegisterRuntime("app.before_by_parameter", "u:%melody.x.password%")
    mustDeclare(t, configuration, "x", secretKey())
    configuration.RegisterRuntime("app.after_by_key", "u:%env(MELODY_X_PASSWORD)%")
    configuration.RegisterRuntime("app.after_by_parameter", "u:%melody.x.password%")
    mustResolve(t, configuration)

    for _, name := range []string{"melody.x.password", "app.before_by_key", "app.before_by_parameter", "app.after_by_key", "app.after_by_parameter"} {
        if false == configuration.Get(name).IsSecret() {
            t.Fatalf("expected %s secret", name)
        }
    }

    marked := mustConfigurationFromValues(t, map[string]string{})
    marked.RegisterRuntime("app.reader", "u:%env(MELODY_X_PASSWORD)%")
    mustDeclare(t, marked, "x", secretKey())
    marked.MarkSecret("melody.x.password")
    if false == marked.Get("app.reader").IsSecret() {
        t.Fatalf("expected the reader of the key marked through the parameter's spelling")
    }
}

func TestDeclareEnvironmentKey_AMarkThroughTheParameterNameReachesAReaderOfTheKey(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TEXT": "v"})
    configuration.RegisterRuntime("app.reader", "u:%env(MELODY_X_TEXT)%")
    mustDeclare(t, configuration, "x", textKey(""))

    configuration.MarkSecret("melody.x.text")

    if false == configuration.Get("app.reader").IsSecret() {
        t.Fatalf("expected the reader of the key marked through the parameter name")
    }
}

func TestDeclareEnvironmentKey_AMarkAfterTheResolveReachesAReaderOfEitherSpelling(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TEXT": "v"})
    configuration.RegisterRuntime("app.reader_by_parameter", "u:%melody.x.text%")
    configuration.RegisterRuntime("app.reader_by_key", "u:%env(MELODY_X_TEXT)%")
    mustDeclare(t, configuration, "x", textKey(""))
    mustResolve(t, configuration)

    configuration.MarkSecret("melody.x.text")

    if false == configuration.Get("app.reader_by_parameter").IsSecret() || false == configuration.Get("app.reader_by_key").IsSecret() {
        t.Fatalf("expected a mark on the parameter, after the resolve, to reach the readers of the parameter and of the key")
    }
}

func TestDeclareEnvironmentKey_ASecretKeyMarksAValueAssembledFromIt(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{".env": "MELODY_X_PASSWORD=hunter2\nAPP_DSN=amqp://user:${MELODY_X_PASSWORD}@host\n"})
    environment, environmentErr := NewEnvironment(context.Background(), source)
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, t.TempDir(), ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    configuration.RegisterRuntime("app.dsn", "%env(APP_DSN)%")
    mustDeclare(t, configuration, "x", secretKey())
    mustResolve(t, configuration)

    if false == configuration.Get("app.dsn").IsSecret() {
        t.Fatalf("expected the value assembled from the secret key marked")
    }
}

func TestDeclareEnvironmentKey_RefusesAKeyTheFrameworkHolds(t *testing.T) {
    for frameworkKey := range frameworkEnvironmentKeyIndex {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        key := textKey("")
        key.Key = frameworkKey

        refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "an environment key is declared that another owner already holds")
        if "the framework" != refusal.Context()["holder"] || frameworkKey != refusal.Context()["environmentKey"] {
            t.Fatalf("%s: expected the framework named, got %v", frameworkKey, refusal.Context())
        }
    }
}

func TestDeclareEnvironmentKey_RefusesAModuleKeyOutsideTheNamespace(t *testing.T) {
    for _, invalidKey := range []string{"AMQP_DSN", "APP_X", "melody_x", "MELODY_", "MELODY_a", "MELODY_X-Y", " MELODY_X", ""} {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        key := textKey("")
        key.Key = invalidKey

        declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a module's environment key is not MELODY_ followed by upper-case letters, digits and underscores")
    }
}

func TestDeclareEnvironmentKey_AdmitsAnApplicationNamespace(t *testing.T) {
    for _, admittedKey := range []string{"APP_X", "AMQP_DSN", "X", "X1_2"} {
        configuration := mustConfigurationFromValues(t, map[string]string{admittedKey: "v"})
        key := textKey("")
        key.Key = admittedKey

        mustDeclare(t, configuration, applicationEnvironmentKeyOwner, key)

        if "v" != configuration.MustGet("melody.x.text").String() {
            t.Fatalf("%s: expected the line read through the parameter, got %q", admittedKey, configuration.MustGet("melody.x.text").String())
        }
    }
}

func TestDeclareEnvironmentKey_RefusesALowerCaseApplicationKey(t *testing.T) {
    for _, invalidKey := range []string{"app_x", "1X", "_X", "APP_x", "APP_X-Y", " APP_X", ""} {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        key := textKey("")
        key.Key = invalidKey

        declarationRefusal(t, configuration.DeclareEnvironmentKey(applicationEnvironmentKeyOwner, key), "a declared environment key is not an upper-case letter followed by upper-case letters, digits and underscores")
    }
}

func TestDeclareEnvironmentKey_RefusesAParameterNameOrAFieldItCannotHold(t *testing.T) {
    for _, testCase := range []struct {
        name      string
        parameter string
        field     string
        expected  string
    }{
        {"an empty parameter", "", "x.F", "a declared environment key names an empty or padded parameter"},
        {"a padded parameter", " melody.x.text", "x.F", "a declared environment key names an empty or padded parameter"},
        {"a kernel parameter", "kernel.x", "x.F", "a declared environment key feeds a parameter in the reserved kernel. namespace"},
        {"the key itself", "MELODY_X_TEXT", "x.F", "a declared environment key feeds a parameter of its own name"},
        {"an empty field", "melody.x.text", "", "a declared environment key names an empty or padded field"},
        {"a padded field", "melody.x.text", "x.F ", "a declared environment key names an empty or padded field"},
    } {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        key := textKey("")
        key.Parameter = testCase.parameter
        key.Field = testCase.field

        declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), testCase.expected)
    }
}

func TestDeclareEnvironmentKey_RefusesASecretWithADefault(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    key := secretKey()
    key.Default = "guest"

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a default is declared for a secret environment key")
    if "MELODY_X_PASSWORD" != refusal.Context()["environmentKey"] || "x.ModuleConfig.Password" != refusal.Context()["field"] {
        t.Fatalf("expected the key and the field named, got %v", refusal.Context())
    }

    if nil != configuration.Get("melody.x.password") {
        t.Fatalf("expected nothing stored for a refused declaration")
    }
}

func TestDeclareEnvironmentKey_RefusesADefaultItsOwnRuleRefuses(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    key := countKey()
    key.Default = "0"

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a declared environment key's default is refused by its own rule")
    assertLimitRefusal(t, refusal, "MELODY_X_COUNT", "x.ModuleConfig.Count")

    templated := mustConfigurationFromValues(t, map[string]string{})
    key.Default = "%app.count%"
    mustDeclare(t, templated, "x", key)
}

func TestDeclareEnvironmentKey_RefusesOneKeyDeclaredTwice(t *testing.T) {
    for _, secondOwner := range []string{"x", "y"} {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        mustDeclare(t, configuration, "x", textKey("a"))

        second := textKey("a")
        second.Parameter = "melody.y.text"

        if "x" == secondOwner {
            refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey(secondOwner, second), "an environment key is declared that the same owner already holds")
            if _, namesHolder := refusal.Context()["holder"]; true == namesHolder || "x" != refusal.Context()["owner"] {
                t.Fatalf("expected the owner named and no holder, got %v", refusal.Context())
            }

            continue
        }

        refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey(secondOwner, second), "an environment key is declared that another owner already holds")
        if "x" != refusal.Context()["holder"] || secondOwner != refusal.Context()["owner"] {
            t.Fatalf("%s: expected both owners named, got %v", secondOwner, refusal.Context())
        }
    }
}

func TestDeclareEnvironmentKey_RefusesTwoKeysOfOneParameter(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", textKey("a"))

    second := textKey("a")
    second.Key = "MELODY_Y_TEXT"

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("y", second), "a declared environment key feeds a parameter another environment key already feeds")
    if "x" != refusal.Context()["holder"] || "MELODY_X_TEXT" != refusal.Context()["feedingEnvironmentKey"] || "y" != refusal.Context()["owner"] {
        t.Fatalf("expected both owners and the feeding key named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_RefusesAParameterAlreadyRegistered(t *testing.T) {
    handMapped := mustConfigurationFromValues(t, map[string]string{"APP_X": "v"})
    handMapped.RegisterRuntime("melody.x.text", "%env(APP_X)%")
    declarationRefusal(t, handMapped.DeclareEnvironmentKey("x", textKey("")), "the parameter a declared environment key feeds is already registered")
}

func TestDeclareEnvironmentKey_RefusedAfterTheBootResolve(t *testing.T) {
    resolved := mustConfigurationFromValues(t, map[string]string{})
    mustResolve(t, resolved)
    declarationRefusal(t, resolved.DeclareEnvironmentKey("x", textKey("")), "an environment key is declared after the configuration resolved")

    serving := mustConfigurationFromValues(t, map[string]string{})
    serving.MarkServing(configseam.Token{})
    declarationRefusal(t, serving.DeclareEnvironmentKey("x", textKey("")), "an environment key is declared after the configuration resolved")
}

func refusalWithContextKey(err error, key string) *exception.Error {
    for current := err; nil != current; current = errors.Unwrap(current) {
        if refusal, isRefusal := current.(*exception.Error); true == isRefusal {
            if _, present := refusal.Context()[key]; true == present {
                return refusal
            }
        }
    }

    return nil
}

func TestCheckEnvironmentKeys_NamesTheKeyTheModuleAndTheField(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_COUNT": "0"})
    mustDeclare(t, configuration, "x", countKey())
    mustResolve(t, configuration)

    checkErr := configuration.CheckEnvironmentKeys()
    refusal := declarationRefusal(t, checkErr, "invalid environment value")

    context := refusal.Context()
    if "MELODY_X_COUNT" != context["environmentKey"] || "x" != context["owner"] || "x.ModuleConfig.Count" != context["field"] || "melody.x.count" != context["parameterName"] {
        t.Fatalf("expected the key, the module, the field and the parameter named, got %v", context)
    }

    cause := refusal.CauseErr()
    if nil == cause || "the limit is below its minimum" != cause.Error() || "x.ModuleConfig.Count" != contextOfError(t, cause)["field"] {
        t.Fatalf("expected the rule's refusal naming the field in the chain, got %v", cause)
    }
}

func TestCheckEnvironmentKeys_JudgesTheResolvedValue(t *testing.T) {
    for _, testCase := range []struct {
        count   string
        refused bool
    }{
        {"0", true},
        {"5", false},
    } {
        configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_COUNT": "%env(APP_COUNT)%", "APP_COUNT": testCase.count})
        mustDeclare(t, configuration, "x", countKey())
        mustResolve(t, configuration)

        if checkErr := configuration.CheckEnvironmentKeys(); testCase.refused != (nil != checkErr) {
            t.Fatalf("APP_COUNT=%s: expected refused=%v, got %v", testCase.count, testCase.refused, checkErr)
        }
    }
}

func TestCheckEnvironmentKeys_AnEmptyLineIsAbsentUnderARule(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_COUNT": ""})
    mustDeclare(t, configuration, "x", countKey())
    mustResolve(t, configuration)

    if checkErr := configuration.CheckEnvironmentKeys(); nil != checkErr {
        t.Fatalf("expected an empty line admitted as absent, got %v", checkErr)
    }

    resolved, resolveErr := ResolveLimitParameter(configuration.MustGet("melody.x.count"), "MELODY_X_COUNT", countKeyRule)
    if nil != resolveErr || 3 != resolved.Value {
        t.Fatalf("expected the rule's default, got %v %v", resolved, resolveErr)
    }
}

func TestDeclareEnvironmentKey_AnEmptyLineUnderARuleTakesTheDeclaredDefault(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_COUNT": "", "MELODY_X_POLICY": "  ", "MELODY_X_SWITCH": "", "MELODY_X_TEXT": ""})

    configuration.mutex.Lock()
    configuration.propagateEnvironmentKeySecretMarkLocked("MELODY_X_COUNT")
    configuration.mutex.Unlock()

    count := countKey()
    count.Default = "7"
    mustDeclare(t, configuration, "x", count)
    mustDeclare(t, configuration, "x", configcontract.EnvironmentKey{Key: "MELODY_X_POLICY", Parameter: "melody.x.policy", Field: "x.F", Default: "Never", Rule: ChoiceKeyRule([]string{"OnFailure", "Never"})})
    mustDeclare(t, configuration, "x", configcontract.EnvironmentKey{Key: "MELODY_X_SWITCH", Parameter: "melody.x.switch", Field: "x.F", Default: "true", Rule: SwitchKeyRule()})
    mustDeclare(t, configuration, "x", textKey("fallback"))
    mustResolve(t, configuration)

    if checkErr := configuration.CheckEnvironmentKeys(); nil != checkErr {
        t.Fatalf("unexpected refusal %v", checkErr)
    }

    resolved, resolveErr := ResolveLimitParameter(configuration.MustGet("melody.x.count"), "MELODY_X_COUNT", countKeyRule)
    if nil != resolveErr || 7 != resolved.Value || false == configuration.Get("melody.x.count").IsDefault() {
        t.Fatalf("expected the declared default, got %v %v", resolved, resolveErr)
    }

    if false == configuration.Get("melody.x.count").IsSecret() {
        t.Fatalf("expected a mark the empty line carried kept by the default")
    }

    if "Never" != configuration.Get("melody.x.policy").String() {
        t.Fatalf("expected the declared choice, got %q", configuration.Get("melody.x.policy").String())
    }

    switchValue, switchErr := ParameterSwitch(configuration.MustGet("melody.x.switch"), "MELODY_X_SWITCH")
    if nil != switchErr || false == switchValue.IsSet || false == switchValue.Value {
        t.Fatalf("expected the declared switch, got %+v %v", switchValue, switchErr)
    }

    if "" != configuration.Get("melody.x.text").String() {
        t.Fatalf("expected a free-text key to keep the empty value, got %q", configuration.Get("melody.x.text").String())
    }
}

type configurationReadingRule struct {
    configuration *Configuration
}

func (instance configurationReadingRule) Check(text string, field string) error {
    _ = instance.configuration.Get(KernelEnvironment)

    return nil
}

func (instance configurationReadingRule) Describe() string {
    return "reads the configuration"
}

func TestDeclareEnvironmentKey_ARuleMayReadTheConfiguration(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TEXT": "v"})
    key := textKey("d")
    key.Rule = configurationReadingRule{configuration: configuration}

    done := make(chan error, 1)
    go func() {
        if declareErr := configuration.DeclareEnvironmentKey("x", key); nil != declareErr {
            done <- declareErr

            return
        }

        if resolveErr := configuration.Resolve(); nil != resolveErr {
            done <- resolveErr

            return
        }

        done <- configuration.CheckEnvironmentKeys()
    }()

    select {
    case runErr := <-done:
        if nil != runErr {
            t.Fatalf("unexpected error %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("expected a rule reading the configuration to return")
    }
}

func TestCheckEnvironmentKeys_ASecretKeyWithholdsTheCause(t *testing.T) {
    for _, declaredSecret := range []bool{true, false} {
        configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_PASSWORD": "424242"})
        key := secretKey()
        key.Secret = declaredSecret
        key.Rule = LimitKeyRule(bound.Rule[int]{Default: 1, Minimum: 1, Maximum: 10})
        mustDeclare(t, configuration, "x", key)
        if false == declaredSecret {
            configuration.MarkSecret("melody.x.password")
        }
        mustResolve(t, configuration)

        checkErr := configuration.CheckEnvironmentKeys()
        refusal := declarationRefusal(t, checkErr, "invalid environment value")
        if nil != refusal.CauseErr() {
            t.Fatalf("declared secret %v: expected the cause withheld, got %v", declaredSecret, refusal.CauseErr())
        }

        if true == strings.Contains(causeChain(checkErr)+fmt.Sprint(refusal.Context()), "424242") {
            t.Fatalf("declared secret %v: expected the value nowhere in the refusal", declaredSecret)
        }
    }
}

func TestCheckEnvironmentKeys_FailsOnTheSameKeyEveryRun(t *testing.T) {
    for run := 0; run < 50; run++ {
        configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_COUNT": "0", "MELODY_W_COUNT": "0"})
        mustDeclare(t, configuration, "x", countKey())

        second := countKey()
        second.Key = "MELODY_W_COUNT"
        second.Parameter = "melody.w.count"
        mustDeclare(t, configuration, "w", second)
        mustResolve(t, configuration)

        if refusal := refusalWithContextKey(configuration.CheckEnvironmentKeys(), "owner"); nil == refusal || "MELODY_W_COUNT" != refusal.Context()["environmentKey"] {
            t.Fatalf("run %d: expected the first key in sorted order, got %v", run, refusal)
        }
    }
}

func TestUndeclaredEnvironmentKeys_NamesWhatNobodyDeclared(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{
        "MELODY_TYPO":             "x",
        "MELODY_INACTIVE_DSN":     "secret",
        HttpAddressKey:            ":8080",
        "MELODY_X_TEXT":           "declared",
        "APP_X":                   "application",
        "XMELODY_NOT_IN_NAMESPACE": "x",
    })
    mustDeclare(t, configuration, "x", textKey(""))

    undeclared := configuration.UndeclaredEnvironmentKeys()
    if 2 != len(undeclared) || "MELODY_INACTIVE_DSN" != undeclared[0] || "MELODY_TYPO" != undeclared[1] {
        t.Fatalf("expected the two undeclared MELODY_ keys sorted, got %v", undeclared)
    }
}

func TestNewConfiguration_TwoConfigurationsDoNotShareDeclarations(t *testing.T) {
    first := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, first, "x", textKey("a"))

    second := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TEXT": "v"})
    if nil != second.Get("melody.x.text") {
        t.Fatalf("expected the second configuration to hold no parameter of the first's declaration")
    }

    if undeclared := second.UndeclaredEnvironmentKeys(); 1 != len(undeclared) || "MELODY_X_TEXT" != undeclared[0] {
        t.Fatalf("expected the key undeclared on the second configuration, got %v", undeclared)
    }

    mustDeclare(t, second, "x", textKey("a"))
}

func TestDeclareEnvironmentKey_ValueIsReadableBeforeTheBootResolve(t *testing.T) {
    for values, expected := range map[string]string{"": "fallback", "from env": "from env"} {
        environmentValues := map[string]string{}
        if "" != values {
            environmentValues["MELODY_X_TEXT"] = values
        }

        configuration := mustConfigurationFromValues(t, environmentValues)
        mustDeclare(t, configuration, "x", textKey("fallback"))

        if expected != configuration.Get("melody.x.text").String() {
            t.Fatalf("expected %q right after the declaration, got %q", expected, configuration.Get("melody.x.text").String())
        }
    }
}

func TestDeclareEnvironmentKey_ADefaultReadingAKeyDeclaredLaterSettlesAtBoot(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    reader := textKey("%env(MELODY_Y_TEXT)%")
    mustDeclare(t, configuration, "x", reader)
    mustDeclare(t, configuration, "y", configcontract.EnvironmentKey{Key: "MELODY_Y_TEXT", Parameter: "melody.y.text", Field: "y.ModuleConfig.Text", Default: "y"})
    mustResolve(t, configuration)

    if "y" != configuration.Get("melody.x.text").String() {
        t.Fatalf("expected the key declared later read at boot, got %q", configuration.Get("melody.x.text").String())
    }
}

func TestDeclareEnvironmentKey_AnUnresolvableTextRefusesAndLeavesTheTables(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_SWITCH": ""})

    declarationRefusal(t, configuration.DeclareEnvironmentKey("x", switchKey("%env(A))%")), "a declared environment key could not be resolved at its declaration")

    if nil != configuration.Get("melody.x.switch") || nil != configuration.Get("MELODY_X_SWITCH") {
        t.Fatalf("expected the tables as they were, got %v", configuration.Get("melody.x.switch"))
    }

    if _, isDeclared := configuration.declaredEnvironmentKeys["MELODY_X_SWITCH"]; true == isDeclared {
        t.Fatalf("expected the declaration taken back")
    }
}

func newConfigurationWithApplicationKeys(values map[string]string, keys []configcontract.EnvironmentKey) (*Configuration, error) {
    environment, environmentErr := NewEnvironment(context.Background(), &testEnvironmentSource{values: values})
    if nil != environmentErr {
        return nil, environmentErr
    }

    return NewConfiguration(environment, "/tmp/melody", ConfigurationConfig{EnvironmentKeys: keys})
}

func applicationKey(key string, parameter string, defaultText string) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       key,
        Parameter: parameter,
        Field:     "ApplicationConfig.EnvironmentKeys",
        Default:   defaultText,
    }
}

func TestNewConfiguration_ApplicationKey_DefaultRuleAndSecretAsAModule(t *testing.T) {
    countApplicationKey := applicationKey("APP_COUNT", "app.count", "")
    countApplicationKey.Rule = LimitKeyRule(countKeyRule)

    passwordApplicationKey := applicationKey("APP_PASSWORD", "app.password", "")
    passwordApplicationKey.Secret = true

    configuration, configurationErr := newConfigurationWithApplicationKeys(
        map[string]string{"APP_COUNT": "0", "APP_PASSWORD": "application-secret"},
        []configcontract.EnvironmentKey{
            applicationKey("APP_FIRST", "app.first", "first-default"),
            applicationKey("APP_SECOND", "app.second", "%env(APP_FIRST)%"),
            countApplicationKey,
            passwordApplicationKey,
        },
    )
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    if "first-default" != configuration.MustGet("app.first").String() {
        t.Fatalf("expected the default through the parameter, got %q", configuration.MustGet("app.first").String())
    }

    if "first-default" != configuration.MustGet("app.second").String() {
        t.Fatalf("expected a default reading an earlier application key answered out of the constructor, got %q", configuration.MustGet("app.second").String())
    }

    if false == configuration.MustGet("app.password").IsSecret() {
        t.Fatalf("expected the secret application key to mark its parameter")
    }

    configuration.RegisterRuntime("app.password.reader", "dsn://%env(APP_PASSWORD)%@host")
    mustResolve(t, configuration)
    if false == configuration.MustGet("app.password.reader").IsSecret() {
        t.Fatalf("expected a reader of the secret application key marked")
    }

    refusal := refusalWithContextKey(configuration.CheckEnvironmentKeys(), "owner")
    if nil == refusal || "invalid environment value" != refusal.Message() || "APP_COUNT" != refusal.Context()["environmentKey"] || "the application" != refusal.Context()["owner"] || "app.count" != refusal.Context()["parameterName"] {
        t.Fatalf("expected the rule refusal naming the application key and its owner, got %v", refusal)
    }
}

func TestNewConfiguration_ApplicationKeyRefusesMelodyNamespace(t *testing.T) {
    _, configurationErr := newConfigurationWithApplicationKeys(map[string]string{}, []configcontract.EnvironmentKey{applicationKey("MELODY_APP_X", "app.x", "")})

    refusal := declarationRefusal(t, configurationErr, "an application's environment key is in the framework's MELODY_ namespace")
    if "MELODY_APP_X" != refusal.Context()["environmentKey"] || "the application" != refusal.Context()["owner"] {
        t.Fatalf("expected the key and the owner named, got %v", refusal.Context())
    }
}

func TestNewConfiguration_ApplicationKeyTwiceIsRefused(t *testing.T) {
    _, configurationErr := newConfigurationWithApplicationKeys(map[string]string{}, []configcontract.EnvironmentKey{
        applicationKey("APP_X", "app.x", ""),
        applicationKey("APP_X", "app.y", ""),
    })

    refusal := declarationRefusal(t, configurationErr, "an environment key is declared that the same owner already holds")
    if _, namesHolder := refusal.Context()["holder"]; true == namesHolder || "the application" != refusal.Context()["owner"] {
        t.Fatalf("expected the application named and no holder, got %v", refusal.Context())
    }
}

func TestNewConfiguration_ApplicationKeyFeedingAKernelParameterOrRetiringAFrameworkKeyIsRefused(t *testing.T) {
    _, configurationErr := newConfigurationWithApplicationKeys(map[string]string{}, []configcontract.EnvironmentKey{applicationKey("APP_X", "kernel.http_address", "")})
    declarationRefusal(t, configurationErr, "a declared environment key feeds a parameter in the reserved kernel. namespace")

    _, retiredErr := newConfigurationWithApplicationKeys(map[string]string{}, []configcontract.EnvironmentKey{
        {Key: "APP_X", Parameter: "app.x", Field: "ApplicationConfig.EnvironmentKeys", Retired: []string{"MELODY_HTTP_ADDRESS"}},
    })
    refusal := declarationRefusal(t, retiredErr, "an environment key is retired that another owner holds")
    if "the framework" != refusal.Context()["holder"] {
        t.Fatalf("expected the framework named as holder, got %v", refusal.Context())
    }
}

/* declared before the construction's resolve, an application key's default reaches a .env line that reads it, where a module's key, declared at boot, is refused there */
func TestNewConfiguration_ALineReadsAnApplicationKeysDefaultAtConstruction(t *testing.T) {
    configuration, configurationErr := newConfigurationWithApplicationKeys(
        map[string]string{"APP_LINK": "https://%env(APP_HOST)%/path"},
        []configcontract.EnvironmentKey{applicationKey("APP_HOST", "app.host", "example.org"), applicationKey("APP_LINK", "app.link", "")},
    )
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    if "https://example.org/path" != configuration.MustGet("app.link").String() {
        t.Fatalf("expected the line to read the declared default, got %q", configuration.MustGet("app.link").String())
    }
}

func TestDeclareEnvironmentKey_AModuleIsRefusedAKeyTheApplicationHoldsOrRetired(t *testing.T) {
    applicationKeys := []configcontract.EnvironmentKey{
        {Key: "APP_TEXT", Parameter: "app.text", Field: "ApplicationConfig.EnvironmentKeys", Retired: []string{"MELODY_X_TEXT"}},
        applicationKey("APP_OTHER", "melody.x.text", ""),
    }

    held, heldErr := newConfigurationWithApplicationKeys(map[string]string{}, applicationKeys[:1])
    if nil != heldErr {
        t.Fatalf("unexpected configuration error: %v", heldErr)
    }
    refusal := declarationRefusal(t, held.DeclareEnvironmentKey("x", textKey("")), "an environment key is declared that another owner retired")
    if "the application" != refusal.Context()["holder"] || "x" != refusal.Context()["owner"] {
        t.Fatalf("expected the application named as holder, got %v", refusal.Context())
    }

    fed, fedErr := newConfigurationWithApplicationKeys(map[string]string{}, applicationKeys[1:])
    if nil != fedErr {
        t.Fatalf("unexpected configuration error: %v", fedErr)
    }
    refusal = declarationRefusal(t, fed.DeclareEnvironmentKey("x", textKey("")), "a declared environment key feeds a parameter another environment key already feeds")
    if "the application" != refusal.Context()["holder"] || "APP_OTHER" != refusal.Context()["feedingEnvironmentKey"] {
        t.Fatalf("expected the application's key named, got %v", refusal.Context())
    }
}

func TestEnvironmentValue_AnswersTheLoadedLine(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"APP_SET": "a", "APP_BLANK": ""})

    if value, isSet := configuration.EnvironmentValue("APP_SET"); false == isSet || "a" != value {
        t.Fatalf("expected the line, got %q %v", value, isSet)
    }

    if value, isSet := configuration.EnvironmentValue("APP_BLANK"); false == isSet || "" != value {
        t.Fatalf("expected a blank line present, got %q %v", value, isSet)
    }

    if _, isSet := configuration.EnvironmentValue(HttpAddressKey); true == isSet {
        t.Fatalf("expected a declared default not to count as a line")
    }
}

func TestEnvironmentKey_ALowerCaseLineLoadsAndReadsButIsNotDeclarable(t *testing.T) {
    configuration, configurationErr := newConfigurationWithApplicationKeys(map[string]string{"database_url": "dsn"}, nil)
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    configuration.RegisterRuntime("app.database_url", "%env(database_url)%")
    mustResolve(t, configuration)

    if "dsn" != configuration.Get("app.database_url").String() {
        t.Fatalf("expected the lower-case line read through %%env()%%")
    }

    _, declareErr := newConfigurationWithApplicationKeys(map[string]string{}, []configcontract.EnvironmentKey{applicationKey("database_url", "app.database_url", "")})
    declarationRefusal(t, declareErr, "a declared environment key is not an upper-case letter followed by upper-case letters, digits and underscores")
}
