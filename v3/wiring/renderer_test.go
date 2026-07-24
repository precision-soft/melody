package wiring

import (
    "testing"
)

/* @info two scanned packages routinely share a base name — a contract package under each domain — so the alias written into the generated file cannot be the package name alone */
func TestImportAliasTable_DisambiguatesPackagesSharingABaseName(t *testing.T) {
    importAliases := newImportAliasTable()

    firstAlias := importAliases.alias("github.com/acme/app/billing/contract")
    secondAlias := importAliases.alias("github.com/acme/app/shipping/contract")
    thirdAlias := importAliases.alias("github.com/acme/app/audit/contract")

    if firstAlias == secondAlias || firstAlias == thirdAlias || secondAlias == thirdAlias {
        t.Fatalf("expected distinct aliases, got %q, %q and %q", firstAlias, secondAlias, thirdAlias)
    }

    if firstAlias != importAliases.alias("github.com/acme/app/billing/contract") {
        t.Fatalf("expected the alias of a path to be stable")
    }
}

func TestImportAliasTable_DoesNotCollideWithAReservedAlias(t *testing.T) {
    importAliases := newImportAliasTable()

    importAliases.reserve(containerContractImportPath, containerContractImportAlias)

    alias := importAliases.alias("github.com/acme/app/containercontract")

    if containerContractImportAlias == alias {
        t.Fatalf("expected the reserved alias to be left alone, got %q", alias)
    }
}

/* @info the provider bodies spell the closure parameters, the fixed locals, the conversion type names, the error of every signature, the string and any of every guard's error context, and the nil/true/false literals; an import alias claiming one of them would be shadowed inside every closure, so the table steps over them */
func TestImportAliasTable_StepsOverTheEmittedIdentifiers(t *testing.T) {
    cases := map[string]string{
        "github.com/acme/app/resolver":      "resolver2",
        "github.com/acme/app/registrar":     "registrar2",
        "github.com/acme/app/configuration": "configuration2",
        "github.com/acme/app/zeroValue":     "zeroValue2",
        "github.com/acme/app/uint32":        "uint322",
        "github.com/acme/app/int":           "int2",
        "github.com/acme/app/bool":          "bool2",
        "github.com/acme/app/float64":       "float642",
        "github.com/acme/app/string":        "string2",
        "github.com/acme/app/any":           "any2",
        "github.com/acme/app/error":         "error2",
        "github.com/acme/app/nil":           "nil2",
    }

    for importPath, expected := range cases {
        importAliases := newImportAliasTable()
        if alias := importAliases.alias(importPath); expected != alias {
            t.Fatalf("expected %q to alias as %q, got %q", importPath, expected, alias)
        }
    }
}

/* @info a directory may be named after a Go keyword (app/interface is legal on disk); the bare keyword cannot appear as an import alias, so it is pushed into the suffixed form */
func TestImportAliasTable_StepsOverAGoKeywordBase(t *testing.T) {
    importAliases := newImportAliasTable()

    if alias := importAliases.alias("github.com/acme/app/interface"); "interface2" != alias {
        t.Fatalf("expected the keyword base to be suffixed, got %q", alias)
    }
}

func TestSanitizeAlias_ProducesAUsableIdentifier(t *testing.T) {
    cases := map[string]string{
        "domain":   "domain",
        "go-kit":   "go_kit",
        "v3":       "v3",
        "2fa":      "pkg2fa",
        "some.pkg": "some_pkg",
    }

    for input, expected := range cases {
        if expected != sanitizeAlias(input) {
            t.Fatalf("expected %q to sanitize to %q, got %q", input, expected, sanitizeAlias(input))
        }
    }
}

/* @info the qualifier written at the constructor belongs to the scanned file, not to the generated one, so rendering has to swap it for the alias the generated file imports the path under */
func TestRenderType_SwapsTheSourceQualifierForTheGeneratedAlias(t *testing.T) {
    importAliases := newImportAliasTable()
    importAliases.reserve("github.com/acme/app/billing/contract", "billingcontract")

    rendered := renderType(
        &TypeReference{
            Expression: "contract.Logger",
            Qualifier:  "contract",
            ImportPath: "github.com/acme/app/billing/contract",
        },
        importAliases,
    )

    if "billingcontract.Logger" != rendered {
        t.Fatalf("unexpected rendering %q", rendered)
    }
}

func TestRenderType_KeepsThePointerPrefix(t *testing.T) {
    importAliases := newImportAliasTable()
    importAliases.reserve("github.com/acme/app/domain", "domain")

    rendered := renderType(
        &TypeReference{
            Expression: "*domain.UserService",
            Qualifier:  "domain",
            ImportPath: "github.com/acme/app/domain",
            IsPointer:  true,
        },
        importAliases,
    )

    if "*domain.UserService" != rendered {
        t.Fatalf("unexpected rendering %q", rendered)
    }
}

func TestRenderType_LeavesABuiltinTypeAlone(t *testing.T) {
    if "string" != renderType(&TypeReference{Expression: "string"}, newImportAliasTable()) {
        t.Fatalf("expected a builtin type to render unchanged")
    }
}

/* @info an accessor returns the widest type of its family, so only a narrower argument needs a conversion in the generated provider */
func TestScalarAccessorAndConversion_PairUpPerType(t *testing.T) {
    cases := []struct {
        typeReference   *TypeReference
        accessor        string
        isFallible      bool
        needsConversion bool
    }{
        {&TypeReference{Expression: "string"}, "MustString()", false, false},
        {&TypeReference{Expression: "bool"}, "Bool()", true, false},
        {&TypeReference{Expression: "int"}, "Int()", true, false},
        {&TypeReference{Expression: "int64"}, "Int()", true, true},
        {&TypeReference{Expression: "float64"}, "Float()", true, false},
        {&TypeReference{Expression: "float32"}, "Float()", true, true},
        {&TypeReference{Expression: "time.Duration", Qualifier: "time", ImportPath: "time"}, "Duration()", true, false},
        /* the classification follows the resolved import path: an aliased stdlib duration keeps its accessor, a foreign package named time does not borrow it */
        {&TypeReference{Expression: "stdtime.Duration", Qualifier: "stdtime", ImportPath: "time"}, "Duration()", true, false},
        {&TypeReference{Expression: "time.Duration", Qualifier: "time", ImportPath: "example.com/faketime"}, "Int()", true, true},
    }

    for _, testCase := range cases {
        accessor, isFallible := scalarAccessor(testCase.typeReference)

        if testCase.accessor != accessor || testCase.isFallible != isFallible {
            t.Fatalf("unexpected accessor for %s: %q fallible=%t", testCase.typeReference.Expression, accessor, isFallible)
        }

        if testCase.needsConversion != scalarNeedsConversion(testCase.typeReference) {
            t.Fatalf("unexpected conversion decision for %s", testCase.typeReference.Expression)
        }
    }
}

/* @info float32(v) silently turns an out-of-range float64 into an infinity the way an integer conversion wraps, so the narrowing guard must cover it with the MaxFloat32 magnitude bounds */
func TestScalarNarrowingRange_GuardsFloat32(t *testing.T) {
    bounds, hasRange := scalarNarrowingRange("float32")
    if false == hasRange {
        t.Fatalf("expected float32 to carry a narrowing range")
    }

    expectedLower := "-" + mathImportAlias + ".MaxFloat32"
    expectedUpper := mathImportAlias + ".MaxFloat32"
    if expectedLower != bounds.lowerBound || expectedUpper != bounds.upperBound {
        t.Fatalf("unexpected float32 bounds %q and %q", bounds.lowerBound, bounds.upperBound)
    }

    if _, hasWideRange := scalarNarrowingRange("float64"); true == hasWideRange {
        t.Fatalf("expected float64 to need no narrowing range")
    }
}

/* @info a pointer-to-scalar argument is not scalar-classified, so its element type is rendered verbatim in FromResolverByType; a directory named after any scalar (app/int) must therefore never claim the bare name as its import alias */
func TestImportAliasTable_StepsOverEveryScalarTypeName(t *testing.T) {
    importAliases := newImportAliasTable()

    for scalarName := range scalarTypeNames {
        if false == importAliases.takenAlias[scalarName] {
            t.Fatalf("expected the alias table to step over the scalar type name %q", scalarName)
        }
    }
}

/* @info the generated function's own name is a package-block identifier, and Go forbids declaring it again as a file-block import alias */
func TestImportAliasTable_ReserveIdentifierPushesTheBaseIntoTheSuffixedForm(t *testing.T) {
    importAliases := newImportAliasTable()
    importAliases.reserveIdentifier("register")

    if alias := importAliases.alias("github.com/acme/app/register"); "register2" != alias {
        t.Fatalf("expected the reserved function name to push the alias onto register2, got %q", alias)
    }
}

/* @info a constructor parameter legally named after a predeclared identifier (any, string, nil) would become a generated local shadowing it ahead of the guards that spell it, so the variable namer steps over the same emitted-identifier list the alias table does */
func TestAssignVariableNames_StepsOverTheEmittedIdentifiers(t *testing.T) {
    importAliases := newImportAliasTable()

    constructor := &Constructor{
        ImportPath: "github.com/acme/app/service",
        ReturnType: &TypeReference{Expression: "service.Pool", Qualifier: "service", ImportPath: "github.com/acme/app/service"},
    }

    resolvedArguments := []*resolvedArgument{
        {argument: &Argument{Name: "any", Type: &TypeReference{Expression: "contract.Logger", Qualifier: "contract", ImportPath: "github.com/acme/app/contract"}}},
        {argument: &Argument{Name: "port", Type: &TypeReference{Expression: "uint16"}, IsScalar: true}},
    }

    assignVariableNames(constructor, resolvedArguments, importAliases)

    if "any2" != resolvedArguments[0].variableName {
        t.Fatalf("expected the local named after the predeclared any to step onto any2, got %q", resolvedArguments[0].variableName)
    }

    if "port" != resolvedArguments[1].variableName {
        t.Fatalf("expected an ordinary parameter name to stay put, got %q", resolvedArguments[1].variableName)
    }
}
