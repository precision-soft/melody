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
