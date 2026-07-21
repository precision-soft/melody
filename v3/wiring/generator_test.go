package wiring

import (
    "go/parser"
    "go/token"
    "os"
    "strings"
    "testing"
)

const (
    fixtureImportPath = "github.com/precision-soft/melody/v3/wiring/internal/fixture/domain"
    fixtureDirectory  = "wiring/internal/fixture/domain"
    fixtureGoldenFile = "internal/fixture/wiring/wiring_gen.go"
    fixtureProjectDir = ".."
)

func newFixtureBindSet() *BindSet {
    bindSet := NewBindSet()

    bindSet.Name("stageEnv", "fixture.stage_env")

    bindSet.Package(fixtureImportPath, fixtureDirectory).
        Name("sessionTtl", "fixture.session_ttl").
        Name("apiUrl", "fixture.api_url").
        Name("retryCount", "fixture.retry_count").
        Exclude("*Fixture")

    return bindSet
}

func generateFixture(t *testing.T, bindSet *BindSet) (string, *GenerateReport) {
    t.Helper()

    source, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "wiring",
        FunctionName:     "RegisterFixtureServices",
        BindSet:          bindSet,
    })
    if nil != generateErr {
        t.Fatalf("expected the fixture to generate, got %v", generateErr)
    }

    return source, report
}

/* @info the committed fixture output is compiled by the ordinary build, so comparing against it is what proves the generator still emits type-correct Go rather than merely well-formed text */
func TestGenerate_MatchesTheCommittedFixtureOutput(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    golden, readErr := os.ReadFile(fixtureGoldenFile)
    if nil != readErr {
        t.Fatalf("could not read the golden file: %v", readErr)
    }

    if string(golden) != source {
        t.Fatalf("the generated wiring drifted from the committed fixture; regenerate %s", fixtureGoldenFile)
    }
}

func TestGenerate_ProducesParsableSource(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    _, parseErr := parser.ParseFile(token.NewFileSet(), "wiring_gen.go", source, parser.AllErrors)
    if nil != parseErr {
        t.Fatalf("the generated source does not parse: %v", parseErr)
    }
}

/* @info the project indents with four spaces rather than tabs, and the generated file is read and reviewed like every other one */
func TestGenerate_IndentsWithSpaces(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if true == strings.Contains(source, "\t") {
        t.Fatalf("the generated source must not contain tabs")
    }
}

func TestGenerate_ReportsSkippedConstructorsWithAReason(t *testing.T) {
    _, report := generateFixture(t, newFixtureBindSet())

    reasonByName := make(map[string]string)
    for _, skipped := range report.Skipped {
        reasonByName[skipped.Name] = skipped.Reason
    }

    for _, name := range []string{"NewGenericHolder", "NewVariadicService"} {
        reason, exists := reasonByName[name]
        if false == exists {
            t.Fatalf("expected %s to be reported as skipped", name)
        }

        if "" == reason {
            t.Fatalf("expected a reason for skipping %s", name)
        }
    }
}

func TestGenerate_HonoursDirectivesAndExcludes(t *testing.T) {
    source, report := generateFixture(t, newFixtureBindSet())

    /* the ignore directive and the exclude pattern both remove a constructor without reporting it as an unwireable skip */
    for _, name := range []string{"NewExcludedByDirective", "NewUserFixture"} {
        if true == strings.Contains(source, name) {
            t.Fatalf("expected %s to be absent from the generated wiring", name)
        }

        for _, skipped := range report.Skipped {
            if name == skipped.Name {
                t.Fatalf("expected %s to be excluded rather than reported as unwireable", name)
            }
        }
    }

    /* the bind directive fills an argument no declared bind covers */
    if false == strings.Contains(source, `MustGet("fixture.reporting_url")`) {
        t.Fatalf("expected the bind directive to supply the reporting url parameter")
    }
}

/* @info a bind that matches no argument is the common misspelling, and the failure it causes otherwise surfaces far from its cause */
func TestGenerate_ReportsBindsThatMatchedNothing(t *testing.T) {
    bindSet := newFixtureBindSet()
    bindSet.Name("airbnbClientID", "fixture.airbnb_client_id")
    bindSet.Packages()[0].Name("sesionTtl", "fixture.session_ttl")

    _, report := generateFixture(t, bindSet)

    unused := strings.Join(report.UnusedBinds, " ")

    if false == strings.Contains(unused, "airbnbClientID") {
        t.Fatalf("expected the unmatched global bind to be reported, got %v", report.UnusedBinds)
    }

    if false == strings.Contains(unused, "sesionTtl") {
        t.Fatalf("expected the unmatched package bind to be reported, got %v", report.UnusedBinds)
    }
}

/* @info a global bind silently reaches every constructor declaring an argument of that name, which is the footgun the reach report exists to expose */
func TestGenerate_ReportsTheReachOfEveryGlobalBind(t *testing.T) {
    _, report := generateFixture(t, newFixtureBindSet())

    reached, exists := report.GlobalBindReach["stageEnv"]
    if false == exists {
        t.Fatalf("expected the global bind reach to be reported")
    }

    if 1 != len(reached) || "NewUserService" != reached[0] {
        t.Fatalf("unexpected reach for the global bind: %v", reached)
    }
}

func TestGenerate_FailsWhenAScalarArgumentHasNoBind(t *testing.T) {
    bindSet := NewBindSet()
    bindSet.Package(fixtureImportPath, fixtureDirectory).Exclude("*Fixture")

    _, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "wiring",
        BindSet:          bindSet,
    })
    if nil == generateErr {
        t.Fatalf("expected generation to refuse an unbound scalar argument")
    }

    if false == strings.Contains(generateErr.Error(), "no bind covers a scalar constructor argument") {
        t.Fatalf("unexpected error: %v", generateErr)
    }
}

/* @info checking the bind target against the declared parameters turns a typo in a parameter name into a generation failure instead of a panic at boot */
func TestGenerate_FailsWhenABindTargetsAnUndeclaredParameter(t *testing.T) {
    _, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "wiring",
        BindSet:          newFixtureBindSet(),
        DeclaredParameters: map[string]bool{
            "fixture.stage_env":   true,
            "fixture.session_ttl": true,
            "fixture.api_url":     true,
            "fixture.retry_count": true,
        },
    })
    if nil == generateErr {
        t.Fatalf("expected generation to refuse a bind targeting an undeclared parameter")
    }

    if false == strings.Contains(generateErr.Error(), "does not declare") {
        t.Fatalf("unexpected error: %v", generateErr)
    }
}

func TestGenerate_RequiresABindSet(t *testing.T) {
    _, _, generateErr := Generate(&GenerateRequest{ProjectDirectory: fixtureProjectDir})
    if nil == generateErr {
        t.Fatalf("expected generation to require a bind set")
    }
}
