package wiring

import (
    "errors"
    "go/parser"
    "go/token"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
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

    if false == strings.Contains(source, `MustGet("fixture.reporting_url")`) {
        t.Fatalf("expected the bind directive to supply the reporting url parameter")
    }
}

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

func TestGenerate_ReturnsADeclaredZeroValueForANonPointerConstructor(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if false == strings.Contains(source, "var zeroValue domain.AuditTrail") {
        t.Fatalf("expected the non-pointer provider to declare a zero value")
    }

    if false == strings.Contains(source, "return zeroValue, repositoryErr") {
        t.Fatalf("expected the error path to return the declared zero value")
    }
}

func TestGenerate_GuardsANarrowingScalarConversion(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if false == strings.Contains(source, "if maxAttemptsValue < 0 || math.MaxUint8 < maxAttemptsValue {") {
        t.Fatalf("expected the narrowing conversion to be range-guarded")
    }

    if false == strings.Contains(source, "a configuration parameter does not fit the constructor argument") {
        t.Fatalf("expected the guard to fail with a named error")
    }
}

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

func TestGenerate_OmitsTheScopedFunctionWhenNothingIsScoped(t *testing.T) {
    source, _ := generateFixture(t, newFixtureBindSet())

    if true == strings.Contains(source, "ScopedRegistrar") {
        t.Fatalf("expected no scoped function for a fixture that declares nothing scoped, got:\n%s", source)
    }
}

func TestGenerate_RefusesAScopedFunctionNameEqualToTheFunctionName(t *testing.T) {
    _, _, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "RegisterScopedFixtureServices")
    if nil == generateErr {
        t.Fatalf("expected the duplicate function name to be refused")
    }

    if "the generated function names must differ, or the file declares the same function twice" != generateErr.Error() {
        t.Fatalf("unexpected refusal message: %q", generateErr.Error())
    }
}

func TestGenerate_RefusesAScopedFunctionNameTheGeneratedFileCannotCarry(t *testing.T) {
    _, _, generateErr := generateScopedFixture(t, "RegisterScopedFixtureServices", "func")
    if nil == generateErr {
        t.Fatalf("expected the unusable scoped function name to be refused")
    }

    if "the generated scoped function name cannot be declared and referenced in the generated file" != generateErr.Error() {
        t.Fatalf("unexpected refusal message: %q", generateErr.Error())
    }
}

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

func TestGenerate_ScopedShadowOfAContainerTypeCarriesTheReplacesOption(t *testing.T) {
    shadowDirectory := t.TempDir()

    writeFixtureFile(t, shadowDirectory, "domain/service.go", `package domain

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

    shadowSource, _, shadowErr := Generate(&GenerateRequest{
        ProjectDirectory: shadowDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil != shadowErr {
        t.Fatalf("expected the scoped shadow to generate, got %v", shadowErr)
    }

    if false == strings.Contains(shadowSource, "WithReplacesContainerService()") {
        t.Fatalf("expected the scoped shadow registration to carry the replaces option, got:\n%s", shadowSource)
    }

    plainDirectory := t.TempDir()

    writeFixtureFile(t, plainDirectory, "domain/service.go", `package domain

//melody:scoped
func NewRequestClock() *Clock {
    return &Clock{}
}

type Clock struct {
}
`)

    plainSource, _, plainErr := Generate(&GenerateRequest{
        ProjectDirectory: plainDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil != plainErr {
        t.Fatalf("expected the unshadowed scoped registration to generate, got %v", plainErr)
    }

    if true == strings.Contains(plainSource, "WithReplacesContainerService()") {
        t.Fatalf("expected the unshadowed scoped registration to render without the replaces option, got:\n%s", plainSource)
    }
}

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

func TestServiceTypeIdentityKey_ValueTypeAndItsPointerShareOneKey(t *testing.T) {
    valueConstructor := &Constructor{
        ImportPath: "example.com/app/domain",
        ReturnType: &TypeReference{
            Expression: "domain.Foo",
            ImportPath: "example.com/app/domain",
            IsPointer:  false,
        },
    }
    pointerConstructor := &Constructor{
        ImportPath: "example.com/app/domain",
        ReturnType: &TypeReference{
            Expression: "*domain.Foo",
            ImportPath: "example.com/app/domain",
            IsPointer:  true,
        },
    }

    if serviceTypeIdentityKey(valueConstructor) != serviceTypeIdentityKey(pointerConstructor) {
        t.Fatalf(
            "expected a value type and its pointer to share one identity key; got %q and %q",
            serviceTypeIdentityKey(valueConstructor),
            serviceTypeIdentityKey(pointerConstructor),
        )
    }
}

func TestServiceIdentityKeys_ANamedRegistrationClaimsItsNameAndItsType(t *testing.T) {
    namedConstructor := &Constructor{
        ImportPath:            "example.com/app/domain",
        ServiceNameIdentifier: "ServiceMailer",
        ReturnType: &TypeReference{
            Expression: "*domain.Mailer",
            ImportPath: "example.com/app/domain",
            IsPointer:  true,
        },
    }

    keys := serviceIdentityKeys(namedConstructor)
    if 2 != len(keys) {
        t.Fatalf("expected a named registration to claim two identities, got %v", keys)
    }

    if "name example.com/app/domain.ServiceMailer" != keys[0] {
        t.Fatalf("expected the constant to be claimed, got %q", keys[0])
    }

    if serviceTypeIdentityKey(namedConstructor) != keys[1] {
        t.Fatalf("expected the returned type to be claimed, got %q", keys[1])
    }
}

func TestGenerate_RefusesTwoNamedConstructorsReturningOneType(t *testing.T) {
    projectDirectory := t.TempDir()

    writeFixtureFile(t, projectDirectory, "domain/service.go", `package domain

const (
    ServiceMailer       = "app.mailer"
    ServiceBackupMailer = "app.mailer.backup"
)

//melody:service ServiceMailer
func NewMailer() *Mailer {
    return &Mailer{}
}

//melody:service ServiceBackupMailer
func NewBackupMailer() *Mailer {
    return &Mailer{}
}

type Mailer struct {
}
`)

    _, _, generateErr := Generate(&GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      "config",
        BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
    })
    if nil == generateErr {
        t.Fatalf("expected two named constructors returning one type to be refused")
    }

    if false == strings.Contains(generateErr.Error(), "two constructors register the same service") {
        t.Fatalf("unexpected error: %v", generateErr)
    }

    var typedErr *exception.Error
    if false == errors.As(generateErr, &typedErr) {
        t.Fatalf("expected the refusal to carry a context, got %v", generateErr)
    }

    context := typedErr.Context()
    first, _ := context["first"].(string)
    second, _ := context["second"].(string)

    named := first + " " + second
    if false == strings.Contains(named, "NewMailer (") || false == strings.Contains(named, "NewBackupMailer (") {
        t.Fatalf("expected the refusal to name both sites, got %q and %q", first, second)
    }
}

func TestGenerate_ScopedShadowOfANamedContainerServiceCarriesTheReplacesOption(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        source string
    }{
        {
            name: "container constructor first",
            source: `package domain

const ServiceClock = "app.clock"

//melody:service ServiceClock
func NewClock() *Clock {
    return &Clock{}
}

//melody:scoped
func NewRequestClock() *Clock {
    return &Clock{}
}

type Clock struct {
}
`,
        },
        {
            name: "scoped constructor first",
            source: `package domain

const ServiceClock = "app.clock"

//melody:scoped
func NewRequestClock() *Clock {
    return &Clock{}
}

//melody:service ServiceClock
func NewClock() *Clock {
    return &Clock{}
}

type Clock struct {
}
`,
        },
        {
            name: "the named side is the scoped one",
            source: `package domain

const ServiceRequestClock = "app.clock.request"

func NewClock() *Clock {
    return &Clock{}
}

//melody:scoped
//melody:service ServiceRequestClock
func NewRequestClock() *Clock {
    return &Clock{}
}

type Clock struct {
}
`,
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            projectDirectory := t.TempDir()

            writeFixtureFile(t, projectDirectory, "domain/service.go", testCase.source)

            source, report, generateErr := Generate(&GenerateRequest{
                ProjectDirectory: projectDirectory,
                PackageName:      "config",
                BindSet:          bindSetWithPackage("github.com/acme/app/domain", "domain"),
            })
            if nil != generateErr {
                t.Fatalf("expected the scoped shadow to generate, got %v", generateErr)
            }

            if 1 != report.ConstructorCount || 1 != report.ScopedConstructorCount {
                t.Fatalf("expected one constructor per lifetime, got %d and %d", report.ConstructorCount, report.ScopedConstructorCount)
            }

            if false == strings.Contains(source, "WithReplacesContainerService()") {
                t.Fatalf("expected the scoped shadow registration to carry the replaces option, got:\n%s", source)
            }
        })
    }
}

type replayClock struct {
}

func TestGenerate_ScopedShadowOfANamedContainerServiceBootsOnlyWithTheReplacesOption(t *testing.T) {
    registerContainerService := func(target containercontract.Container) error {
        return container.Register[*replayClock](
            target,
            "app.clock",
            func(resolver containercontract.Resolver) (*replayClock, error) {
                return &replayClock{}, nil
            },
        )
    }

    registerScopedService := func(target containercontract.Container, options ...containercontract.RegisterOption) error {
        return container.RegisterScopedType[*replayClock](
            target,
            func(resolver containercontract.Resolver) (*replayClock, error) {
                return &replayClock{}, nil
            },
            options...,
        )
    }

    for _, testCase := range []struct {
        name             string
        containerIsFirst bool
    }{
        {name: "container registration first", containerIsFirst: true},
        {name: "scoped registration first", containerIsFirst: false},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            bare := container.NewContainer()
            defer bare.Close()

            var bareErr error
            if true == testCase.containerIsFirst {
                if registerErr := registerContainerService(bare); nil != registerErr {
                    t.Fatalf("the first registration must succeed, got %v", registerErr)
                }

                bareErr = registerScopedService(bare)
            } else {
                if registerErr := registerScopedService(bare); nil != registerErr {
                    t.Fatalf("the first registration must succeed, got %v", registerErr)
                }

                bareErr = registerContainerService(bare)
            }

            if nil == bareErr {
                t.Fatalf("expected the container to refuse the unmarked shadow")
            }

            replacing := container.NewContainer()
            defer replacing.Close()

            var replacingErr error
            if true == testCase.containerIsFirst {
                if registerErr := registerContainerService(replacing); nil != registerErr {
                    t.Fatalf("the first registration must succeed, got %v", registerErr)
                }

                replacingErr = registerScopedService(replacing, container.WithReplacesContainerService())
            } else {
                if registerErr := registerScopedService(replacing, container.WithReplacesContainerService()); nil != registerErr {
                    t.Fatalf("the first registration must succeed, got %v", registerErr)
                }

                replacingErr = registerContainerService(replacing)
            }

            if nil != replacingErr {
                t.Fatalf("expected the marked shadow to be admitted, got %v", replacingErr)
            }
        })
    }
}
