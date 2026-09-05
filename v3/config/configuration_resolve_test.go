package config

import (
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

/* a self-referential env value expands to itself plus extra characters on every pass; the env-substitution branch is not covered by the parameter-recursion cycle guard, so without the pass bound the fixed-point loop never terminates */
func TestResolveTemplate_SelfReferentialEnvValueReportsCircularReference(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_A": "x%env(APP_A)%",
            },
        },
        parameters: ParameterMap{},
    }

    type resolveOutcome struct {
        value string
        err   error
    }

    done := make(chan resolveOutcome, 1)

    go func() {
        value, err := configuration.resolveTemplate(
            "x%env(APP_A)%",
            "app.a",
            make(map[string]bool),
            make(map[string]bool),
        )

        done <- resolveOutcome{value: value, err: err}
    }()

    select {
    case outcome := <-done:
        if nil == outcome.err {
            t.Fatalf("expected a circular reference error, got value %q", outcome.value)
        }

        if false == strings.Contains(outcome.err.Error(), "circular reference") {
            t.Fatalf("expected a circular reference error, got %v", outcome.err)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("resolveTemplate never returned: the self-referential env value looped forever")
    }
}

/* an undefined env placeholder must be reported by key only; the raw parameter value routinely carries inline credentials that would otherwise reach logs through the exception cause-context chain */
func TestResolveTemplate_UndefinedEnvironmentKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "mysql://root:S3cretPassword@tcp(db:3306)/app?tls=%env(DB_TLS)%"

    _, err := configuration.resolveTemplate(secretValue, "database.url", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined environment key error")
    }

    context := contextOfError(t, err)

    assertContextOmitsSecret(t, context, "S3cretPassword")

    if "DB_TLS" != context["environmentKey"] {
        t.Fatalf("expected the offending environment key in the context, got %v", context["environmentKey"])
    }
}

/* an undefined parameter placeholder must be reported by key only, never with the raw value that may embed credentials */
func TestResolveTemplate_UndefinedParameterKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "postgres://user:P4ssPhrase@host:5432/db?opt=%missing_parameter%"

    _, err := configuration.resolveTemplate(secretValue, "database.dsn", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    assertContextOmitsSecret(t, context, "P4ssPhrase")

    if "missing_parameter" != context["parameterKey"] {
        t.Fatalf("expected the offending parameter key in the context, got %v", context["parameterKey"])
    }
}

func contextOfError(t *testing.T, err error) map[string]any {
    t.Helper()

    exceptionError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected an *exception.Error, got %T", err)
    }

    return exceptionError.Context()
}

func assertContextOmitsSecret(t *testing.T, context map[string]any, secret string) {
    t.Helper()

    if _, present := context["value"]; true == present {
        t.Fatalf("error context must not embed the raw parameter value")
    }

    if true == strings.Contains(fmt.Sprintf("%v", context), secret) {
        t.Fatalf("error context leaked the inline credential: %v", context)
    }
}

func TestEnvPlaceholderPattern_AcceptsDefaultProcessorForms(t *testing.T) {
    if false == envPlaceholderPattern.MatchString("%env(default::AWS_ENDPOINT_URL)%") {
        t.Fatalf("expected pattern to accept the empty fallback form")
    }

    if false == envPlaceholderPattern.MatchString("%env(default:app.fallback:AWS_ENDPOINT_URL)%") {
        t.Fatalf("expected pattern to accept the parameter fallback form")
    }
}

/* a single colon is the most likely misspelling of the default processor; the strict pattern cannot match it, so without the shape guard it would survive resolution as literal text and reach the service as the string "%env(default:KEY)%" */
func TestResolveTemplate_MalformedEnvPlaceholderIsReported(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(default:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected a malformed placeholder error")
    }

    context := contextOfError(t, err)

    if "%env(default:AWS_ENDPOINT_URL)%" != context["placeholder"] {
        t.Fatalf("expected the offending placeholder in the context, got %v", context["placeholder"])
    }
}

func TestResolveTemplate_DefaultProcessorYieldsEmptyStringForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    value, err := configuration.resolveTemplate(
        "%env(default::AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the undefined key to be tolerated, got %v", err)
    }

    if "" != value {
        t.Fatalf("expected an empty value, got %q", value)
    }
}

func TestResolveTemplate_DefaultProcessorFallsBackToParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "aws.default_endpoint": NewParameter("", "http://localstack:4566", "http://localstack:4566", false),
        },
    }

    value, err := configuration.resolveTemplate(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the fallback parameter to resolve, got %v", err)
    }

    if "http://localstack:4566" != value {
        t.Fatalf("expected the fallback parameter value, got %q", value)
    }
}

func TestResolveTemplate_DefaultProcessorPrefersDefinedEnvironmentValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "AWS_ENDPOINT_URL": "https://s3.eu-central-1.amazonaws.com",
            },
        },
        parameters: ParameterMap{
            "aws.default_endpoint": NewParameter("", "http://localstack:4566", "http://localstack:4566", false),
        },
    }

    value, err := configuration.resolveTemplate(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected resolution to succeed, got %v", err)
    }

    if "https://s3.eu-central-1.amazonaws.com" != value {
        t.Fatalf("expected the environment value to win over the fallback, got %q", value)
    }
}

/* the default processor must stay opt-in: a plain placeholder that silently degraded to an empty string would boot the application with an unset credential instead of refusing to start */
func TestResolveTemplate_PlainPlaceholderStillFailsForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected an undefined environment key error")
    }

    context := contextOfError(t, err)

    if "AWS_ENDPOINT_URL" != context["environmentKey"] {
        t.Fatalf("expected the offending environment key in the context, got %v", context["environmentKey"])
    }
}

/* the fallback is handed back as a parameter placeholder, so an undefined fallback must surface the parameter branch error rather than resolve to the literal text */
func TestResolveTemplate_DefaultProcessorReportsUndefinedFallbackParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(default:aws.missing_fallback:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    if "aws.missing_fallback" != context["parameterKey"] {
        t.Fatalf("expected the offending fallback key in the context, got %v", context["parameterKey"])
    }
}

/* a value holding a literal percent — a generated password is the common case — is written with the percent doubled; without the escape it reads as a parameter reference and the boot fails, which is what the resolution and validation messages have to point at */
func TestConfiguration_EnvironmentValueWithLiteralPercentIsWrittenDoubled(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%%ss%%word",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the escaped value to resolve, got %v", newConfigurationErr)
    }

    if "pa%ss%word" != configuration.Get("DATABASE_PASSWORD").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("DATABASE_PASSWORD").String())
    }
}

/* an unescaped literal percent reads as a parameter reference, and an undefined reference is indistinguishable from a forward reference to a parameter the composition root registers next — so the constructor defers it and the boot resolve is where the typo fails, with the message that points at the escape */
func TestConfiguration_UnescapedLiteralPercentFailsTheBootResolveWithAnActionableMessage(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%ss%word",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the unresolved reference, got %v", newConfigurationErr)
    }

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the boot resolve to fail on the unescaped value")
    }

    /* the resolve error wraps the resolution failure, and Error() reports only its own message, so the actionable one is found by walking the cause chain */
    messages := ""
    for err := resolveErr; nil != err; err = errors.Unwrap(err) {
        messages = messages + err.Error() + "\n"
    }

    if false == strings.Contains(messages, "%%") {
        t.Fatalf("expected the cause chain to point at the escape, got %s", messages)
    }
}

/* the deferral exists for exactly this flow: a .env value referencing a parameter the composition root registers between construction and boot used to kill the process inside the constructor, before the registration it referenced could ever run */
func TestConfiguration_AForwardReferenceDefersAndTheBootResolveSettlesIt(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_GREETING": "hello %app.user%",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the forward reference, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.user", "operator")

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the boot resolve to settle the deferred reference, got %v", resolveErr)
    }

    if "hello operator" != configuration.Get("APP_GREETING").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("APP_GREETING").String())
    }
}

/* the window between construction and boot is exactly where a deferred parameter must be unreadable: it still holds the raw template, and an accessor serving %app.user% as though it were the value is the silent half the loud refusal exists to prevent */
func TestConfiguration_ADeferredParameterIsUnreadableUntilTheBootResolveSettlesIt(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_GREETING": "hello %app.user%",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the forward reference, got %v", newConfigurationErr)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the read of a deferred parameter to refuse")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %T", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "deferred to boot") {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }
    }()

    _ = configuration.Get("APP_GREETING").String()
}

/* a kernel.* parameter is registered by melody itself before the constructor's pass, so an undefined reference in one is a settled error no later registration repairs; deferring it would only move the failure into whichever kernel view reads it next */
func TestResolveAll_TheTolerantPassDoesNotDeferAReservedParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "kernel.probe": NewParameter("KERNEL_PROBE", "%missing.parameter%", nil, false),
        },
    }

    resolveErr := configuration.resolveAll(true)
    if nil == resolveErr {
        t.Fatalf("expected the tolerant pass to refuse an unresolved reference in a reserved parameter")
    }

    if false == strings.Contains(resolveErr.Error(), "failed to resolve parameter") {
        t.Fatalf("unexpected refusal message: %q", resolveErr.Error())
    }
}

/* an environment value is a template of its own: its doubled percents are its literals and must survive the splice as data instead of being rescanned as the parameter reference %ss% */
func TestResolveTemplate_EnvValueDoubledPercentsResolveToLiterals(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_PASSWORD": "pa%%ss%%word",
            },
        },
        parameters: ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "%env(APP_PASSWORD)%",
        "app.password",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the env value to resolve, got %v", resolveErr)
    }

    if "pa%ss%word" != value {
        t.Fatalf("expected the doubled percents to resolve to literals, got %q", value)
    }
}

/* a referenced parameter's resolved value is data: a password holding a literal percent must splice into the dsn that reads it without being rescanned as a reference */
func TestResolveTemplate_ReferencedValueWithLiteralPercentSplicesAsData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.password": NewParameter("APP_PASSWORD", "pa%%ss%%word", "pa%%ss%%word", false),
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "mysql://root:%app.password%@db/app",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the reference to resolve, got %v", resolveErr)
    }

    if "mysql://root:pa%ss%word@db/app" != escapedValue {
        t.Fatalf("expected the referenced percents to survive as data, got %q", escapedValue)
    }
}

/* marking the env-registered parameter is how a credential read through %env(KEY)% is declared, and the marking must travel to the reader exactly as it does on the parameter branch */
func TestResolveTemplate_SecretTravelsThroughAnEnvPlaceholder(t *testing.T) {
    passwordParameter := NewParameter("MYSQL_PASSWORD", "s3cret", "s3cret", false)
    passwordParameter.isSecret.Store(true)

    databaseUrlParameter := NewParameter("", "root:%env(MYSQL_PASSWORD)%@db", "", false)

    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "MYSQL_PASSWORD": "s3cret",
            },
        },
        parameters: ParameterMap{
            "MYSQL_PASSWORD": passwordParameter,
            "database.url":   databaseUrlParameter,
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "root:%env(MYSQL_PASSWORD)%@db",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the env reference to resolve, got %v", resolveErr)
    }

    if "root:s3cret@db" != escapedValue {
        t.Fatalf("unexpected resolved value %q", escapedValue)
    }

    if false == databaseUrlParameter.IsSecret() {
        t.Fatalf("expected the secret marking to travel through the env placeholder")
    }
}

/* an env value carrying a single-percent malformed shape still fails: values are templates by design, and the doubled-percent escape is the supported way to write a literal percent */
func TestResolveTemplate_InjectedMalformedShapeStillFails(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_RAW": "x%env(a:b)%y",
            },
        },
        parameters: ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(APP_RAW)%",
        "app.raw",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the malformed injected placeholder to be reported")
    }

    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

/* a self-reference is a cycle like any other: the scanner reports it at resolve time instead of leaving the placeholder behind as literal text for an after-the-fact check to catch */
func TestResolveTemplate_SelfReferenceIsACircularReference(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.a": NewParameter("", "%app.b%_%app.a%", "", false),
            "app.b": NewParameter("", "pa%%ss", "", false),
        },
    }

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the self-reference to be reported at resolve time")
    }

    if false == strings.Contains(resolveErr.Error()+causeChain(resolveErr), "circular parameter reference") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

func causeChain(err error) string {
    messages := ""
    for cause := err; nil != cause; cause = errors.Unwrap(cause) {
        messages = messages + cause.Error() + "\n"
    }

    return messages
}

/* a literal percent written with the doubled escape survives a splice as data, and adjacent references resolve instead of swallowing each other */
func TestResolveTemplate_SplicedLiteralsAndAdjacentReferencesResolve(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.a": NewParameter("", "x_%app.b%", "", false),
            "app.b": NewParameter("", "pa%%ss%%word", "", false),
            "app.c": NewParameter("", "%app.a%%app.b%", "", false),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the resolution to succeed, got %v", resolveErr)
    }

    if "x_pa%ss%word" != configuration.getInternalParameter("app.a").String() {
        t.Fatalf("unexpected resolved value %q", configuration.getInternalParameter("app.a").String())
    }

    /* the adjacency %app.a%%app.b% resolves both references, where an escape-first reading would have swallowed the touching percents */
    if "x_pa%ss%wordpa%ss%word" != configuration.getInternalParameter("app.c").String() {
        t.Fatalf("unexpected adjacent resolution %q", configuration.getInternalParameter("app.c").String())
    }
}

/* the default processor accepts a single-character fallback name, so the parameter pattern has to resolve it too instead of leaving %a% as literal text */
func TestResolveTemplate_SingleCharacterParameterReferenceResolves(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "a": NewParameter("", "short", "", false),
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "%env(default:a:MISSING_KEY)%",
        "app.value",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the single-character fallback to resolve, got %v", resolveErr)
    }

    if "short" != escapedValue {
        t.Fatalf("expected the fallback parameter to resolve, got %q", escapedValue)
    }
}

/* the candidate ends where the placeholder grammar ends: a literal %env( must stay data even when a different, well-formed placeholder closes later in the same value */
func TestResolveTemplate_LiteralEnvPrefixBesideARealPlaceholderIsData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_NAME": "melody",
            },
        },
        parameters: ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "write %env( around the key; app=%env(APP_NAME)%",
        "app.hint",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the literal prefix to stay data, got %v", resolveErr)
    }

    if "write %env( around the key; app=melody" != value {
        t.Fatalf("unexpected resolved value %q", value)
    }
}

/* the project directory is a filesystem path, not a template: a literal percent in it survives a reference and the parameter itself is never overwritten */
func TestResolveTemplate_ProjectDirectoryReferenceIsData(t *testing.T) {
    projectDirectoryParameter := NewParameter("", "/srv/app%1", "/srv/app%1", true)

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            KernelProjectDir:  projectDirectoryParameter,
            "kernel.logs_dir": NewParameter("", "%kernel.project_dir%/var/log", "", true),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the reference to resolve, got %v", resolveErr)
    }

    if "/srv/app%1/var/log" != configuration.getInternalParameter("kernel.logs_dir").String() {
        t.Fatalf("unexpected logs dir %q", configuration.getInternalParameter("kernel.logs_dir").String())
    }

    if "/srv/app%1" != projectDirectoryParameter.String() {
        t.Fatalf("expected the project directory to stay untouched, got %q", projectDirectoryParameter.String())
    }
}

/* a misspelled placeholder that still closes (%env(FOO-BAR)%) is reported, while a literal "%env(" whose closer belongs to a different placeholder stays data — the candidate ends at the first ")%" no percent interrupts */
func TestResolveTemplate_ClosedMisspelledPlaceholderIsStillReported(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(FOO-BAR)%",
        "app.value",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the closed misspelled placeholder to be reported")
    }

    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

func TestRegisterRuntime_ConcurrentWithReadsOfAReferencedParameter(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"ACME_HOST": "acme.example"},
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.host", "%env(ACME_HOST)%")
        },
    )

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 1000; iteration++ {
            configuration.RegisterRuntime(fmt.Sprintf("app.derived.%d", iteration), "%app.host%:8080")
        }
    }()

    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 1000; iteration++ {
            if "acme.example" != configuration.Get("app.host").String() {
                t.Errorf("expected the referenced parameter to keep its resolved value")

                return
            }
        }
    }()

    waitGroup.Wait()
}

/* A post-boot Resolve cannot reconfigure a running application: every service copied out the values it needed while it was built and none of them looks again. What it still does is rewrite the whole parameter store underneath readers entitled to treat it as settled, so once the application runs it is refused instead of half-honoured. */
func TestConfiguration_ResolveIsRefusedOnceServing(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_TAG": "tag",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.tag", "%env(APP_TAG)%")

    /* the documented manual construction resolves before it serves and has to keep working */
    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing()

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected a resolve to be refused once the application serves")
    }

    if false == strings.Contains(resolveErr.Error(), "begun serving") {
        t.Fatalf("expected the refusal to say why, got %q", resolveErr.Error())
    }

    if "tag" != configuration.MustGet("app.tag").MustString() {
        t.Fatalf("expected the value resolved before serving to be untouched, got %q", configuration.MustGet("app.tag").MustString())
    }
}

/* Registering a parameter after boot still works: it resolves itself on registration, which is what keeps a late module functioning without reopening the whole store. */
func TestConfiguration_RegisterRuntimeStillResolvesOnceServing(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_TAG": "tag",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing()

    configuration.RegisterRuntime("app.late", "%env(APP_TAG)%")

    if "tag" != configuration.MustGet("app.late").MustString() {
        t.Fatalf("expected the late parameter to resolve on registration, got %q", configuration.MustGet("app.late").MustString())
    }
}

/* the candidate runs to the real ")%" closer: a ")" that closes nothing does not end the search, so %env(A))% is reported as the malformed placeholder it is instead of surviving as literal text */
func TestResolveTemplate_EnvPlaceholderWithInnerParenthesisIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{"A": "x"}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(A))%",
        "app.a",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the placeholder with the inner parenthesis to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("expected the malformed placeholder report, got: %v", resolveErr)
    }
}

/* a %env( that never closes is an error, not data: the forgotten closing percent left postgres://user:%env(DB_PASS)@db connecting with the literal placeholder as its password, and nothing said so */
func TestResolveTemplate_UnterminatedEnvPlaceholderIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{"DB_PASS": "secret"}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "postgres://user:%env(DB_PASS)@db/app",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unterminated placeholder to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "unterminated environment placeholder") {
        t.Fatalf("expected the unterminated placeholder report, got: %v", resolveErr)
    }
}

/* a name-shaped run a percent opened and nothing closed is a reference with a typo: %app-name% used to survive as literal text while the contract already demands a literal percent be doubled */
func TestResolveTemplate_UnclosedParameterReferenceIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.name": NewParameter("", "melody", "melody", false),
        },
    }

    _, resolveErr := configuration.resolveTemplate(
        "service-%app-name%",
        "app.banner",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unclosed reference to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "malformed parameter reference") {
        t.Fatalf("expected the malformed reference report, got: %v", resolveErr)
    }

    /* the sentence alone does not say WHICH percent was refused: the trailing percent of this same template opens no reference and, with the guard reading the flag the other way round, produces the identical sentence — so the named reference is the observable that tells the two paths apart */
    if "%app" != contextOfError(t, resolveErr)["reference"] {
        t.Fatalf("expected the name-shaped run to be the refused reference, got: %v", contextOfError(t, resolveErr)["reference"])
    }
}

/* a percent in front of a character no name may start with stays data: the refusal of unclosed references must not reach genuine literals */
func TestResolveTemplate_PercentBeforeNonNameCharacterStaysData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "growth of 50% overall",
        "app.note",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the literal percent to survive, got: %v", resolveErr)
    }
    if "growth of 50% overall" != value {
        t.Fatalf("unexpected value: %q", value)
    }
}

/* a referenced parameter whose environment value is not a string is reported by type alone: a signing key registered as bytes is exactly what a template would reference, and the raw value must not reach the logs */
func TestResolveTemplate_NonStringReferenceErrorOmitsTheValue(t *testing.T) {
    secretBytes := "0123456789abcdef"

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.signing_key": NewParameter("", []byte(secretBytes), []byte(secretBytes), false),
        },
    }

    _, resolveErr := configuration.resolveTemplate(
        "key=%app.signing_key%",
        "app.assembled",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the non-string reference to be refused")
    }

    var exceptionErr *exception.Error
    if false == errors.As(resolveErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", resolveErr)
    }
    if _, valuePresent := exceptionErr.Context()["environmentValue"]; true == valuePresent {
        t.Fatalf("expected the raw value to stay out of the error context")
    }
    if "[]uint8" != exceptionErr.Context()["environmentValueType"] {
        t.Fatalf("expected the type to identify the value, got: %v", exceptionErr.Context()["environmentValueType"])
    }
    if true == strings.Contains(fmt.Sprintf("%v", exceptionErr.Context()), secretBytes) {
        t.Fatalf("expected the secret bytes to stay out of the rendered context")
    }
}

func TestResolveAll_TwoBrokenTemplatesFailOnTheSortedFirstParameterEveryTime(t *testing.T) {
    for iteration := 0; iteration < 30; iteration++ {
        configuration := &Configuration{
            environment: &Environment{
                values: map[string]string{},
            },
            parameters: ParameterMap{
                "zulu.broken":  NewParameter("ZULU_BROKEN", "%missing.one%", nil, false),
                "alpha.broken": NewParameter("ALPHA_BROKEN", "%missing.two%", nil, false),
            },
        }

        resolveErr := configuration.resolveAll(false)
        if nil == resolveErr {
            t.Fatalf("expected the broken templates to be refused")
        }

        melodyErr, isMelodyErr := resolveErr.(*exception.Error)
        if false == isMelodyErr {
            t.Fatalf("expected a melody error, got %T", resolveErr)
        }

        if "alpha.broken" != melodyErr.Context()["parameter"] {
            t.Fatalf("iteration %d: expected the failure to name the sorted-first parameter, got %v", iteration, melodyErr.Context()["parameter"])
        }
    }
}

func TestResolve_TheFailureNamesTheEnvironmentKeyBesideTheInternalAlias(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            "MELODY_LOG_PATH": "%app.name%/application.log",
        },
    }

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, t.TempDir())
    if nil == configurationErr {
        t.Fatal("expected the undefined reference to fail the construction")
    }

    resolutionContext := map[string]any(nil)

    for current := error(configurationErr); nil != current; current = errors.Unwrap(current) {
        melodyErr, isMelodyErr := current.(*exception.Error)
        if false == isMelodyErr {
            continue
        }

        if "failed to resolve parameter" == melodyErr.Error() {
            resolutionContext = melodyErr.Context()

            break
        }
    }

    if nil == resolutionContext {
        t.Fatalf("expected the parameter resolution failure in the chain, got %v", configurationErr)
    }

    if KernelLogPath != resolutionContext["parameter"] {
        t.Fatalf("expected the internal alias kept, got %v", resolutionContext["parameter"])
    }

    if "MELODY_LOG_PATH" != resolutionContext["environmentKey"] {
        t.Fatalf("expected the key the operator actually wrote, got %v", resolutionContext["environmentKey"])
    }
}
