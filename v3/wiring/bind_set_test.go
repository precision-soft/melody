package wiring

import (
    "testing"
)

/* the declaration ORDER is what the generator reports an unmatched bind by, and a map has none: re-binding a name already declared must keep its first position rather than move it to the end, or the report would reorder between runs over the same declaration */
func TestBindSet_NameKeepsTheDeclarationOrderAndTheLastValue(t *testing.T) {
    bindSet := NewBindSet()

    chained := bindSet.Name("address", "parameter.address").Name("timeout", "parameter.timeout").Name("address", "parameter.address.other")

    if bindSet != chained {
        t.Fatal("expected Name to answer the set it was called on")
    }

    names := bindSet.GlobalBindNames()
    if 2 != len(names) || "address" != names[0] || "timeout" != names[1] {
        t.Fatalf("expected the declaration order to survive a re-bind, got %v", names)
    }

    parameterName, exists := bindSet.GlobalBind("address")
    if false == exists || "parameter.address.other" != parameterName {
        t.Fatalf("expected the last value of a re-bound name, got %q exists=%v", parameterName, exists)
    }

    if parameterName, exists := bindSet.GlobalBind("absent"); true == exists || "" != parameterName {
        t.Fatalf("expected an undeclared name to be absent, got %q", parameterName)
    }
}

/* every reader hands out a copy: the generator walks these lists while it renders, and a caller that reordered or truncated what it read would be rewriting the declaration the report is measured against */
func TestBindSet_ReadersHandOutCopies(t *testing.T) {
    bindSet := NewBindSet()
    bindSet.Name("address", "parameter.address")
    bindSet.Package("example.com/domain", "domain")

    names := bindSet.GlobalBindNames()
    names[0] = "rewritten"
    if "address" != bindSet.GlobalBindNames()[0] {
        t.Fatalf("expected the set to keep its own bind names, got %v", bindSet.GlobalBindNames())
    }

    packages := bindSet.Packages()
    packages[0] = nil
    if nil == bindSet.Packages()[0] {
        t.Fatal("expected the set to keep its own package list")
    }
}

func TestBindSet_PackageDeclaresAScannedPackageInOrder(t *testing.T) {
    bindSet := NewBindSet()

    domain := bindSet.Package("example.com/domain", "domain")
    infrastructure := bindSet.Package("example.com/infrastructure", "infrastructure")

    packages := bindSet.Packages()
    if 2 != len(packages) || domain != packages[0] || infrastructure != packages[1] {
        t.Fatalf("expected both packages in declaration order, got %v", packages)
    }

    if "example.com/domain" != domain.ImportPath() || "domain" != domain.Directory() {
        t.Fatalf("expected the import path and directory to be carried, got %q %q", domain.ImportPath(), domain.Directory())
    }

    /* a fresh package binding declares nothing, and the readers must answer empty rather than nil so the generator can range over them without guarding */
    if nil == domain.BindNames() || 0 != len(domain.BindNames()) {
        t.Fatalf("expected no binds on a fresh package, got %v", domain.BindNames())
    }

    if nil == domain.Excludes() || 0 != len(domain.Excludes()) {
        t.Fatalf("expected no excludes on a fresh package, got %v", domain.Excludes())
    }
}

func TestPackageBinding_NameAndExcludeChainAndKeepTheirOrder(t *testing.T) {
    packageBinding := NewBindSet().Package("example.com/domain", "domain")

    chained := packageBinding.Name("address", "parameter.address").Name("timeout", "parameter.timeout").Name("address", "parameter.address.other")
    if packageBinding != chained {
        t.Fatal("expected Name to answer the package binding it was called on")
    }

    names := packageBinding.BindNames()
    if 2 != len(names) || "address" != names[0] || "timeout" != names[1] {
        t.Fatalf("expected the declaration order to survive a re-bind, got %v", names)
    }

    parameterName, exists := packageBinding.Bind("address")
    if false == exists || "parameter.address.other" != parameterName {
        t.Fatalf("expected the last value of a re-bound name, got %q exists=%v", parameterName, exists)
    }

    if parameterName, exists := packageBinding.Bind("absent"); true == exists || "" != parameterName {
        t.Fatalf("expected an undeclared name to be absent, got %q", parameterName)
    }

    excludeChained := packageBinding.Exclude("*Repository").Exclude("*Factory")
    if packageBinding != excludeChained {
        t.Fatal("expected Exclude to answer the package binding it was called on")
    }

    excludes := packageBinding.Excludes()
    if 2 != len(excludes) || "*Repository" != excludes[0] || "*Factory" != excludes[1] {
        t.Fatalf("expected the excludes in declaration order, got %v", excludes)
    }
}

func TestPackageBinding_ReadersHandOutCopies(t *testing.T) {
    packageBinding := NewBindSet().Package("example.com/domain", "domain")
    packageBinding.Name("address", "parameter.address").Exclude("*Repository")

    names := packageBinding.BindNames()
    names[0] = "rewritten"
    if "address" != packageBinding.BindNames()[0] {
        t.Fatalf("expected the package to keep its own bind names, got %v", packageBinding.BindNames())
    }

    excludes := packageBinding.Excludes()
    excludes[0] = "rewritten"
    if "*Repository" != packageBinding.Excludes()[0] {
        t.Fatalf("expected the package to keep its own excludes, got %v", packageBinding.Excludes())
    }
}
