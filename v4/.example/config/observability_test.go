package config

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"

    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

type spyBootWarningRegistrar struct {
    declared map[string]bool
    warnings []melodyapplicationcontract.BootWarning
}

func (instance *spyBootWarningRegistrar) DeclareBootWarning(name string, config melodyapplicationcontract.BootWarningConfig) {
    if nil == instance.declared {
        instance.declared = make(map[string]bool)
    }

    instance.declared[name] = config.WrittenInCli
}

func (instance *spyBootWarningRegistrar) AddBootWarning(warning melodyapplicationcontract.BootWarning) {
    instance.warnings = append(instance.warnings, warning)
}

func productionMetricsModule(t *testing.T, token string) *Module {
    t.Helper()

    return moduleWithEnvironment(t, map[string]string{melodyconfig.EnvironmentNameKey: melodyconfig.EnvironmentProduction, environmentKeyMetricsToken: token})
}

/* outside development the committed token, padded or not, refuses the boot naming the key and never the value */
func TestBuildMetricsAuth_RefusesTheCommittedTokenOutsideDevelopment(t *testing.T) {
    for _, value := range []string{developmentMetricsToken, " " + developmentMetricsToken + " "} {
        moduleInstance := productionMetricsModule(t, value)

        refusal := recoveredRefusal(t, func() { moduleInstance.buildMetricsAuth() })
        if nil == refusal || false == strings.HasPrefix(refusal.Error(), environmentKeyMetricsToken+" ") {
            t.Fatalf("%q: expected the boot refused naming the key, got %v", value, refusal)
        }

        logged := fmt.Sprintf("%v %v", refusal, melodyexception.LogContext(refusal, melodyexception.LogContextConfig{}))
        if true == strings.Contains(logged, developmentMetricsToken) {
            t.Fatalf("the refusal carries the value: %v", logged)
        }
    }

    moduleInstance := productionMetricsModule(t, "a-scrape-token-of-its-own")
    if refusal := recoveredRefusal(t, func() { moduleInstance.buildMetricsAuth() }); nil != refusal {
        t.Fatalf("expected a token of its own to boot outside development, got %v", refusal)
    }

    if "a-scrape-token-of-its-own" != moduleInstance.metricsToken || metricsScraperRole != moduleInstance.metricsAccessAttribute() {
        t.Fatalf("expected the token of its own guarding /metrics, got %q %q", moduleInstance.metricsToken, moduleInstance.metricsAccessAttribute())
    }
}

/* the refused constant is the value .env commits, so the refusal cannot drift from the file */
func TestDevelopmentMetricsToken_IsTheValueEnvCommits(t *testing.T) {
    content, readErr := os.ReadFile(filepath.Join("..", ".env"))
    if nil != readErr {
        t.Fatalf("read .env: %v", readErr)
    }

    for _, line := range strings.Split(string(content), "\n") {
        if value, found := strings.CutPrefix(strings.TrimSpace(line), environmentKeyMetricsToken+"="); true == found {
            if developmentMetricsToken != value {
                t.Fatalf("expected the committed constant to be the .env value of %s", environmentKeyMetricsToken)
            }

            return
        }
    }

    t.Fatalf("expected .env to commit %s", environmentKeyMetricsToken)
}

func TestBuildMetricsAuth_KeepsAnEmptyTokenPublicOutsideDevelopment(t *testing.T) {
    moduleInstance := productionMetricsModule(t, "")

    if refusal := recoveredRefusal(t, func() { moduleInstance.buildMetricsAuth() }); nil != refusal {
        t.Fatalf("expected an empty token to boot outside development, got %v", refusal)
    }

    if "" != moduleInstance.metricsToken || melodysecuritycontract.AttributePublicAccess != moduleInstance.metricsAccessAttribute() {
        t.Fatalf("expected /metrics public, got %q %q", moduleInstance.metricsToken, moduleInstance.metricsAccessAttribute())
    }
}

func TestBuildMetricsAuth_AcceptsTheCommittedTokenInDevelopment(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyMetricsToken: developmentMetricsToken})

    if refusal := recoveredRefusal(t, func() { moduleInstance.buildMetricsAuth() }); nil != refusal {
        t.Fatalf("expected the committed token accepted in development, got %v", refusal)
    }

    if developmentMetricsToken != moduleInstance.metricsToken {
        t.Fatalf("expected the committed token kept, got %q", moduleInstance.metricsToken)
    }
}

/* a whitespace token is read as empty in both environments: public, never a "Bearer   " credential */
func TestBuildMetricsAuth_ReadsAWhitespaceTokenAsEmpty(t *testing.T) {
    for _, moduleInstance := range []*Module{
        productionMetricsModule(t, "   "),
        moduleWithEnvironment(t, map[string]string{environmentKeyMetricsToken: " \t "}),
    } {
        if refusal := recoveredRefusal(t, func() { moduleInstance.buildMetricsAuth() }); nil != refusal {
            t.Fatalf("expected a whitespace token to boot, got %v", refusal)
        }

        if "" != moduleInstance.metricsToken || melodysecuritycontract.AttributePublicAccess != moduleInstance.metricsAccessAttribute() {
            t.Fatalf("expected a whitespace token read as empty, got %q", moduleInstance.metricsToken)
        }
    }

    moduleInstance := productionMetricsModule(t, " padded-token ")
    moduleInstance.buildMetricsAuth()
    if "padded-token" != moduleInstance.metricsToken {
        t.Fatalf("expected the token trimmed, got %q", moduleInstance.metricsToken)
    }
}

/* no committed default: an empty key in development stays empty, /metrics public */
func TestBuildMetricsAuth_TakesNoFallbackInDevelopmentWhenEmpty(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyMetricsToken: ""})
    moduleInstance.buildMetricsAuth()

    if "" != moduleInstance.metricsToken || melodysecuritycontract.AttributePublicAccess != moduleInstance.metricsAccessAttribute() {
        t.Fatalf("expected no fallback to the committed token, got %q", moduleInstance.metricsToken)
    }
}

/* the code is declared, written in cli, in every environment; one warning names the key and the path, never a value, for an empty token outside development */
func TestRegisterBootWarnings_WarnsOfAPublicExpositionOutsideDevelopmentOnly(t *testing.T) {
    if "example.metrics.public" != MetricsPublicBootWarningName {
        t.Fatalf("expected the code example.metrics.public, got %q", MetricsPublicBootWarningName)
    }

    for _, testCase := range []struct {
        name     string
        module   *Module
        expected int
    }{
        {"production, empty token", productionMetricsModule(t, ""), 1},
        {"production, token of its own", productionMetricsModule(t, "a-scrape-token-of-its-own"), 0},
        {"development, empty token", moduleWithEnvironment(t, map[string]string{environmentKeyMetricsToken: ""}), 0},
        {"development, committed token", moduleWithEnvironment(t, map[string]string{environmentKeyMetricsToken: developmentMetricsToken}), 0},
    } {
        testCase.module.buildMetricsAuth()

        registrar := &spyBootWarningRegistrar{}
        testCase.module.RegisterBootWarnings(registrar)

        if writtenInCli, declared := registrar.declared[MetricsPublicBootWarningName]; false == declared || false == writtenInCli || 1 != len(registrar.declared) {
            t.Fatalf("%s: expected the one code declared written in cli, got %v", testCase.name, registrar.declared)
        }

        if testCase.expected != len(registrar.warnings) {
            t.Fatalf("%s: expected %d warnings, got %v", testCase.name, testCase.expected, registrar.warnings)
        }

        for _, warning := range registrar.warnings {
            if MetricsPublicBootWarningName != warning.Name || metricsPublicBootWarningMessage != warning.Message {
                t.Fatalf("%s: unexpected warning %v", testCase.name, warning)
            }

            if environmentKeyMetricsToken != warning.Context["environmentKey"] || metricsPath != warning.Context["path"] || 2 != len(warning.Context) {
                t.Fatalf("%s: expected the context to name the key and the path alone, got %v", testCase.name, warning.Context)
            }
        }
    }
}
