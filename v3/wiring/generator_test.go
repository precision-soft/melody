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
        Name("maxAttempts", "fixture.max_attempts").
        Name("refreshBudget", "fixture.refresh_budget").
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

/* the committed fixture output is compiled by the ordinary build, so comparing against it is what proves the generator still emits type-correct Go rather than merely well-formed text */
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

/* the project indents with four spaces rather than tabs, and the generated file is read and reviewed like every other one */
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

    for _, name := range []string{"NewGenericHolder", "NewVariadicService", "NewMigrationRunner", "NewToken"} {
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

/* a bind that matches no argument is the common misspelling, and the failure it causes otherwise surfaces far from its cause */
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

/* a global bind silently reaches every constructor declaring an argument of that name, which is the footgun the reach report exists to expose */
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

/* checking the bind target against the declared parameters turns a typo in a parameter name into a generation failure instead of a panic at boot */
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

/* nil is not a value of a struct type, so the error paths of a non-pointer provider have to return a declared zero value or the generated file does not compile */
func TestGenerate_ReturnsADeclaredZeroValueForANonPointerConstructor(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if false == strings.Contains(source, "var zeroValue domain.AuditTrail") {
        t.Fatalf("expected the non-pointer provider to declare a zero value")
    }

    if false == strings.Contains(source, "return zeroValue, repositoryErr") {
        t.Fatalf("expected the error path to return the declared zero value")
    }
}

/* a Go conversion wraps silently, so a parameter of -1 handed to a uint8 argument would otherwise become 255 instead of an error naming the parameter */
func TestGenerate_GuardsANarrowingScalarConversion(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if false == strings.Contains(source, "if maxAttemptsValue < 0 || math.MaxUint8 < maxAttemptsValue {") {
        t.Fatalf("expected the narrowing conversion to be range-guarded")
    }

    if false == strings.Contains(source, "a configuration parameter does not fit the constructor argument") {
        t.Fatalf("expected the guard to fail with a named error")
    }
}

/* the framework config package is imported under a fixed alias by every provider body, so a scanned dependency living in that same package must render through that alias instead of claiming "config" for itself */
func TestGenerate_RendersAFrameworkConfigDependencyThroughTheReservedAlias(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if false == strings.Contains(source, "FromResolverByType[*melodyconfig.Parameter]") {
        t.Fatalf("expected the framework config dependency to use the reserved alias")
    }
}

func TestGenerate_ExcludesAFileTheBuildExcludes(t *testing.T) {
    source, report := generateFixture(t, newFixtureBindSet())

    if true == strings.Contains(source, "NewBuildExcluded") {
        t.Fatalf("expected the build-excluded constructor to be absent from the wiring")
    }

    for _, skipped := range report.Skipped {
        if "NewBuildExcluded" == skipped.Name {
            t.Fatalf("expected the build-excluded file to be excluded rather than reported as unwireable")
        }
    }
}

/* a function with no providers still has to compile: an import block naming the container package that no body uses would fail the build of an application whose scan found nothing yet */
func TestGenerate_EmptyScanEmitsACompilableFile(t *testing.T) {
    bindSet := NewBindSet()
    bindSet.Package(
        "github.com/precision-soft/melody/v3/wiring/internal/fixture/wiring",
        "wiring/internal/fixture/wiring",
    )

    source, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "wiring",
        FunctionName:     "RegisterNothing",
        BindSet:          bindSet,
    })
    if nil != generateErr {
        t.Fatalf("expected the empty scan to generate, got %v", generateErr)
    }

    if 0 != report.ConstructorCount {
        t.Fatalf("expected no constructors, got %d", report.ConstructorCount)
    }

    if true == strings.Contains(source, containerImportPath+"\"") {
        t.Fatalf("expected the unused container import to be absent")
    }

    if _, parseErr := parser.ParseFile(token.NewFileSet(), "wiring_gen.go", source, parser.AllErrors); nil != parseErr {
        t.Fatalf("the empty generated source does not parse: %v", parseErr)
    }
}

func TestResolveArguments_ReportsADirectiveBindThatMatchedNoArgument(t *testing.T) {
    bindSet := NewBindSet()
    packageBinding := bindSet.Package("example.com/domain", "domain")

    constructor := &Constructor{
        Name:           "NewOrphanDirective",
        DirectiveBinds: map[string]string{"missing": "fixture.missing"},
        Arguments:      make([]*Argument, 0),
    }

    _, unusedDirectiveBinds, resolveErr := resolveArguments(
        constructor,
        packageBinding,
        &GenerateRequest{BindSet: bindSet},
        make(map[string]bool),
        &GenerateReport{GlobalBindReach: make(map[string][]string)},
    )
    if nil != resolveErr {
        t.Fatalf("expected the resolution to succeed, got %v", resolveErr)
    }

    if 1 != len(unusedDirectiveBinds) || "NewOrphanDirective.missing" != unusedDirectiveBinds[0] {
        t.Fatalf("expected the orphan directive bind to be reported, got %v", unusedDirectiveBinds)
    }
}

/* a directive bind on a service argument is the same silent loss: the bind is dead because only a scalar is filled from a parameter */
func TestResolveArguments_ReportsADirectiveBindOnAServiceArgument(t *testing.T) {
    bindSet := NewBindSet()
    packageBinding := bindSet.Package("example.com/domain", "domain")

    constructor := &Constructor{
        Name:           "NewServiceBound",
        DirectiveBinds: map[string]string{"repository": "fixture.repository"},
        Arguments: []*Argument{
            {
                Name:     "repository",
                Type:     &TypeReference{Expression: "*domain.UserRepository", Qualifier: "domain", IsPointer: true},
                IsScalar: false,
            },
        },
    }

    _, unusedDirectiveBinds, resolveErr := resolveArguments(
        constructor,
        packageBinding,
        &GenerateRequest{BindSet: bindSet},
        make(map[string]bool),
        &GenerateReport{GlobalBindReach: make(map[string][]string)},
    )
    if nil != resolveErr {
        t.Fatalf("expected the resolution to succeed, got %v", resolveErr)
    }

    if 1 != len(unusedDirectiveBinds) || "NewServiceBound.repository" != unusedDirectiveBinds[0] {
        t.Fatalf("expected the service-argument directive bind to be reported, got %v", unusedDirectiveBinds)
    }
}

/* both names land verbatim in the generated source, so a non-identifier, a keyword, a name the generated file already spells, or an identifier the spec refuses in that position (the blank identifier, init) fails generation at its cause instead of emitting a file that cannot parse, compile, or be called */
func TestGenerate_RejectsAFunctionNameTheGeneratedFileCannotCarry(t *testing.T) {
    for _, functionName := range []string{"melodycontainer", "error", "func", "foo bar", "3services", "a.b", "Register(", "Register//", "_", "init"} {
        _, _, generateErr := Generate(&GenerateRequest{
            ProjectDirectory: fixtureProjectDir,
            PackageName:      "wiring",
            FunctionName:     functionName,
            BindSet:          newFixtureBindSet(),
        })
        if nil == generateErr {
            t.Fatalf("expected the function name %q to be rejected", functionName)
        }
    }
}

func TestGenerate_EmptyFunctionNameFallsBackToTheDefault(t *testing.T) {
    source, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "wiring",
        FunctionName:     "",
        BindSet:          newFixtureBindSet(),
    })
    if nil != generateErr {
        t.Fatalf("expected the empty function name to fall back to the default, got %v", generateErr)
    }

    if false == strings.Contains(source, "func RegisterGeneratedServices(") {
        t.Fatalf("expected the default function name in the generated source")
    }
}

func TestGenerate_RejectsAPackageNameTheGeneratedFileCannotCarry(t *testing.T) {
    for _, packageName := range []string{"foo bar", "3pkg", "a.b", "func", "", "_"} {
        _, _, generateErr := Generate(&GenerateRequest{
            ProjectDirectory: fixtureProjectDir,
            PackageName:      packageName,
            FunctionName:     "RegisterFixtureServices",
            BindSet:          newFixtureBindSet(),
        })
        if nil == generateErr {
            t.Fatalf("expected the package name %q to be rejected", packageName)
        }
    }
}

const (
    scopedFixtureImportPath = "github.com/precision-soft/melody/v3/wiring/internal/fixture/scoped"
    scopedFixtureDirectory  = "wiring/internal/fixture/scoped"
    scopedFixtureGoldenFile = "internal/fixture/wiringscoped/wiring_gen.go"
)

func generateScopedFixture(t *testing.T, functionName string, scopedFunctionName string) (string, *GenerateReport, error) {
    t.Helper()

    bindSet := NewBindSet()
    bindSet.Package(scopedFixtureImportPath, scopedFixtureDirectory)

    return Generate(&GenerateRequest{
        ProjectDirectory:   fixtureProjectDir,
        PackageName:        "wiringscoped",
        FunctionName:       functionName,
        ScopedFunctionName: scopedFunctionName,
        BindSet:            bindSet,
    })
}

/* the committed scoped fixture output is compiled by the ordinary build, so comparing against it is what proves the scoped emission is type-correct Go — that MustRegisterScoped really takes a ScopedRegistrar, and that the two functions can coexist in one file */
func TestGenerate_MatchesTheCommittedScopedFixtureOutput(t *testing.T) {
    source, report, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "")
    if nil != generateErr {
        t.Fatalf("expected the scoped fixture to generate, got %v", generateErr)
    }

    if 1 != report.ConstructorCount {
        t.Fatalf("expected one container constructor, got %d", report.ConstructorCount)
    }

    if 2 != report.ScopedConstructorCount {
        t.Fatalf("expected two scoped constructors, got %d", report.ScopedConstructorCount)
    }

    golden, readErr := os.ReadFile(scopedFixtureGoldenFile)
    if nil != readErr {
        t.Fatalf("could not read the scoped golden file: %v", readErr)
    }

    if string(golden) != source {
        t.Fatalf("the generated scoped wiring drifted from the committed fixture; regenerate %s", scopedFixtureGoldenFile)
    }
}

/* a scoped constructor emitted into the container function would be registered as a process singleton, which is the silent half of the lifetime mistake: it would be built once and never closed with the request it was written for. */
func TestGenerate_EmitsScopedRegistrationsInTheirOwnFunction(t *testing.T) {
    source, _, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "")
    if nil != generateErr {
        t.Fatalf("expected the scoped fixture to generate, got %v", generateErr)
    }

    containerFunctionIndex := strings.Index(source, "func RegisterScopedFixtureServices(registrar containercontract.Registrar) {")
    scopedFunctionIndex := strings.Index(source, "func RegisterScopedFixtureServicesScoped(registrar containercontract.ScopedRegistrar) {")

    if 0 > containerFunctionIndex || 0 > scopedFunctionIndex {
        t.Fatalf("expected both registration functions to be emitted, got:\n%s", source)
    }

    trailIndex := strings.Index(source, "scoped.ServiceRequestTrail")
    if trailIndex < scopedFunctionIndex {
        t.Fatalf("expected the scoped constructor to be emitted inside the scoped function, got:\n%s", source)
    }

    writerIndex := strings.Index(source, "scoped.NewProcessWriter")
    if writerIndex > scopedFunctionIndex {
        t.Fatalf("expected the container constructor to stay in the container function, got:\n%s", source)
    }

    if false == strings.Contains(source, ".MustRegisterScoped(") {
        t.Fatalf("expected a named scoped registration to use MustRegisterScoped, got:\n%s", source)
    }

    if false == strings.Contains(source, ".MustRegisterScopedType(") {
        t.Fatalf("expected a type-only scoped registration to use MustRegisterScopedType, got:\n%s", source)
    }
}

/* a project that declares nothing scoped must keep regenerating the file it already had, or every consumer of the generator sees a spurious diff and a function nobody calls. */
func TestGenerate_OmitsTheScopedFunctionWhenNothingIsScoped(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if true == strings.Contains(source, "ScopedRegistrar") {
        t.Fatalf("expected no scoped function for a fixture that declares nothing scoped, got:\n%s", source)
    }
}

/* the two functions share one file, so one name for both would declare the same function twice and not compile; which lifetime survived would be whichever the renderer wrote last. */
func TestGenerate_RefusesAScopedFunctionNameEqualToTheFunctionName(t *testing.T) {
    _, _, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "RegisterScopedFixtureServices")
    if nil == generateErr {
        t.Fatalf("expected the duplicate function name to be refused")
    }

    if "the generated function names must differ, or the file declares the same function twice" != generateErr.Error() {
        t.Fatalf("unexpected refusal message: %q", generateErr.Error())
    }
}

/* the scoped name lands verbatim in the generated source exactly as the container one does, so it needs the same refusal for a name the file cannot declare. */
func TestGenerate_RefusesAScopedFunctionNameTheGeneratedFileCannotCarry(t *testing.T) {
    _, _, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "func")
    if nil == generateErr {
        t.Fatalf("expected the unusable scoped function name to be refused")
    }

    if "the generated scoped function name cannot be declared and referenced in the generated file" != generateErr.Error() {
        t.Fatalf("unexpected refusal message: %q", generateErr.Error())
    }
}

/* two constructors that claim one container key panic at the first boot of the generated file, far from the generation that reported success; the collision fails here, naming both sites. */
func TestGenerate_RefusesTwoConstructorsRegisteringOneType(t *testing.T) {
    projectDirectory := t.TempDir()

    writeFixtureFile(t, projectDirectory, "domain/service.go", `package domain

func NewUserRepository() *UserRepository {
    return &UserRepository{}
}

func NewCachedUserRepository() *UserRepository {
    return &UserRepository{}
}

type UserRepository struct {
}
`)

    _, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil == generateErr {
        t.Fatalf("expected the colliding registrations to be refused")
    }

    message := generateErr.Error()
    if false == strings.Contains(message, "two constructors register the same service") {
        t.Fatalf("unexpected error: %v", generateErr)
    }
}

/* the same key rule holds for named registrations: two constructors naming one exported constant register under one service name. */
func TestGenerate_RefusesTwoConstructorsNamingOneServiceConstant(t *testing.T) {
    projectDirectory := t.TempDir()

    writeFixtureFile(t, projectDirectory, "domain/service.go", `package domain

const ServiceMailer = "app.mailer"

//melody:service ServiceMailer
func NewMailer() *Mailer {
    return &Mailer{}
}

//melody:service ServiceMailer
func NewBackupMailer() *BackupMailer {
    return &BackupMailer{}
}

type Mailer struct {
}

type BackupMailer struct {
}
`)

    _, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil == generateErr {
        t.Fatalf("expected the colliding named registrations to be refused")
    }

    if false == strings.Contains(generateErr.Error(), "two constructors register the same service") {
        t.Fatalf("unexpected error: %v", generateErr)
    }
}

/* the two lifetimes register through different registrars, and a scoped registration of a type is what deliberately shadows the container one inside a scope — one type across the two lifetimes is not a collision. */
func TestGenerate_AllowsOneTypeAcrossTheTwoLifetimes(t *testing.T) {
    projectDirectory := t.TempDir()

    writeFixtureFile(t, projectDirectory, "domain/service.go", `package domain

func NewClock() *Clock {
    return &Clock{}
}

//melody:scoped
func NewRequestClock() *Clock {
    return &Clock{}
}

type Clock struct {
}
`)

    _, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil != generateErr {
        t.Fatalf("expected the two lifetimes to coexist, got %v", generateErr)
    }

    if 1 != report.ConstructorCount || 1 != report.ScopedConstructorCount {
        t.Fatalf("expected one constructor per lifetime, got %d and %d", report.ConstructorCount, report.ScopedConstructorCount)
    }
}

/* an empty import path renders an import of "", and an empty directory joins to the project root and silently scans the whole tree as one package. */
func TestGenerate_RefusesAPackageBindingWithAnEmptyHalf(t *testing.T) {
    for _, testCase := range []struct {
        importPath string
        directory  string
    }{
        {"", "domain"},
        {"github.com/acme/app/domain", ""},
    } {
        _, _, generateErr := Generate(&GenerateRequest{
            ProjectDirectory: t.TempDir(),
            PackageName:      "config",
            BindSet:          bindSetWithPackage(testCase.importPath, testCase.directory),
        })
        if nil == generateErr {
            t.Fatalf("expected the binding %q/%q to be refused", testCase.importPath, testCase.directory)
        }

        if false == strings.Contains(generateErr.Error(), "a package binding must declare both an import path and a directory") {
            t.Fatalf("unexpected error: %v", generateErr)
        }
    }
}

/* the generator's contract is to say when it could not check the bind targets rather than silently assume every target exists; the flag is raised only when a bind actually went unchecked. */
func TestGenerate_ReportsUncheckedBindTargets(t *testing.T) {
    projectDirectory := t.TempDir()

    writeFixtureFile(t, projectDirectory, "domain/service.go", `package domain

func NewRepository(dsn string) *Repository {
    return &Repository{}
}

type Repository struct {
}
`)

    bindSet := NewBindSet().Name("dsn", "app.dsn")
    bindSet.Package("github.com/acme/app/domain", "domain")

    request := &GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      "config",
        BindSet:          bindSet,
    }

    _, report, generateErr := Generate(request)
    if nil != generateErr {
        t.Fatalf("generate: %v", generateErr)
    }

    if false == report.BindTargetsUnchecked {
        t.Fatalf("expected the unchecked bind targets to be reported")
    }

    request.DeclaredParameters = map[string]bool{"app.dsn": true}

    _, report, generateErr = Generate(request)
    if nil != generateErr {
        t.Fatalf("generate with declared parameters: %v", generateErr)
    }

    if true == report.BindTargetsUnchecked {
        t.Fatalf("expected a checked bind not to raise the flag")
    }
}

/* an unused exclude reaches the report carrying its package's import path, the way an unused bind does, so the strict refusal names where the dead pattern was declared. */
func TestGenerate_ReportsAnUnusedExcludeWithItsImportPath(t *testing.T) {
    bindSet := newFixtureBindSet()
    bindSet.Packages()[0].Exclude("*Respository")

    _, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: fixtureProjectDir,
        PackageName:      "config",
        BindSet:          bindSet,
    })
    if nil != generateErr {
        t.Fatalf("generate: %v", generateErr)
    }

    if 1 != len(report.UnusedExcludes) || fixtureImportPath+".*Respository" != report.UnusedExcludes[0] {
        t.Fatalf("unexpected unused excludes: %v", report.UnusedExcludes)
    }
}
