package container

import (
    "reflect"
    "testing"
    collisionalpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
)

func TestCanonicalServiceType_FilesValuesUnderTheirPointerAndLeavesInterfacesAlone(t *testing.T) {
    valueType := reflect.TypeOf(utilityProbe{})
    pointerType := reflect.TypeOf(&utilityProbe{})

    if pointerType != canonicalServiceType(valueType) {
        t.Fatalf("expected a value type to canonicalise to its pointer, got %v", canonicalServiceType(valueType))
    }

    if pointerType != canonicalServiceType(pointerType) {
        t.Fatalf("expected a pointer type to be its own canonical form, got %v", canonicalServiceType(pointerType))
    }

    interfaceType := reflect.TypeOf((*utilityProbeInterface)(nil)).Elem()
    if interfaceType != canonicalServiceType(interfaceType) {
        t.Fatalf("expected an interface to be its own canonical form, got %v", canonicalServiceType(interfaceType))
    }

    if nil != canonicalServiceType(nil) {
        t.Fatalf("expected a nil type to canonicalise to nil")
    }
}

func TestTypeIdentityKey_SeparatesSameNamedTypesOfDifferentPackages(t *testing.T) {
    alphaKey := typeIdentityKey(reflect.TypeOf(&collisionalpha.Bus{}))
    betaKey := typeIdentityKey(reflect.TypeOf(&collisionbeta.Bus{}))

    if alphaKey == betaKey {
        t.Fatalf("expected two same-named types of different packages to hold distinct keys, both read %q", alphaKey)
    }

    if reflect.TypeOf(&collisionalpha.Bus{}).String() != reflect.TypeOf(&collisionbeta.Bus{}).String() {
        t.Fatalf("expected the two probe types to share a String(), which is what makes this test meaningful")
    }

    if "" != typeIdentityKey(nil) {
        t.Fatalf("expected a nil type to key as the empty string, got %q", typeIdentityKey(nil))
    }
}

func TestTypeIdentityKey_ReachesTheNamedTypeThroughEveryPointer(t *testing.T) {
    singleKey := typeIdentityKey(reflect.TypeOf(&utilityProbe{}))
    doubleKey := typeIdentityKey(reflect.TypeOf(new(*utilityProbe)))

    if singleKey == doubleKey {
        t.Fatalf("expected two different pointer depths to keep distinct keys, both read %q", singleKey)
    }

    if reflect.TypeOf(&utilityProbe{}).PkgPath() != "" {
        t.Fatalf("a pointer type reports no package path of its own; the key has to reach the element for it")
    }
}

func TestDefaultServiceNameForType_QualifiesTheNameWithTheImportPath(t *testing.T) {
    alphaName := defaultServiceNameForType(reflect.TypeOf(&collisionalpha.Bus{}))
    betaName := defaultServiceNameForType(reflect.TypeOf(&collisionbeta.Bus{}))

    if alphaName == betaName {
        t.Fatalf("expected distinct derived names, both read %q", alphaName)
    }

    if "" == alphaName || "" == betaName {
        t.Fatalf("expected both names to be derived, got %q and %q", alphaName, betaName)
    }

    if alphaName != defaultServiceNameForType(reflect.TypeOf(collisionalpha.Bus{})) {
        t.Fatalf("expected the value type to derive the same name as its pointer")
    }

    if "" != defaultServiceNameForType(nil) {
        t.Fatalf("expected a nil type to derive no name")
    }
}

func TestUniqueTypeName_FallsBackToTheSpellingForUnqualifiedTypes(t *testing.T) {
    if "*int" != uniqueTypeName(reflect.TypeOf(new(int))) {
        t.Fatalf("unexpected name for a pointer to a builtin: %q", uniqueTypeName(reflect.TypeOf(new(int))))
    }

    if "*struct {}" != uniqueTypeName(reflect.TypeOf(&struct{}{})) {
        t.Fatalf("unexpected name for a pointer to an unnamed struct: %q", uniqueTypeName(reflect.TypeOf(&struct{}{})))
    }
}

func TestIsAnyType_CatchesTheEmptyInterfaceAndNothingElse(t *testing.T) {
    anyType := reflect.TypeOf((*any)(nil)).Elem()
    if false == isAnyType(anyType) {
        t.Fatalf("expected the empty interface to be recognised")
    }

    namedInterfaceType := reflect.TypeOf((*utilityProbeInterface)(nil)).Elem()
    if true == isAnyType(namedInterfaceType) {
        t.Fatalf("expected an interface carrying methods not to be read as any")
    }

    if true == isAnyType(reflect.TypeOf(&utilityProbe{})) {
        t.Fatalf("expected a concrete type not to be read as any")
    }

    if true == isAnyType(nil) {
        t.Fatalf("expected a nil type not to be read as any")
    }
}

func TestOverrideValueFitsRegisteredType_JudgesRawAssignabilityAndCanonicalIdentity(t *testing.T) {
    greeterType := reflect.TypeOf((*fitsProbeGreeter)(nil)).Elem()

    if false == overrideValueFitsRegisteredType(reflect.TypeOf(&fitsProbeImplementer{}), greeterType) {
        t.Fatalf("expected an implementing pointer to fit the registered interface")
    }

    if true == overrideValueFitsRegisteredType(reflect.TypeOf(&fitsProbeOutsider{}), greeterType) {
        t.Fatalf("expected a non-implementing value to be refused for the registered interface")
    }

    if false == overrideValueFitsRegisteredType(reflect.TypeOf(""), reflect.PointerTo(reflect.TypeOf(""))) {
        t.Fatalf("expected a raw value to fit the canonical key its own registration stores under")
    }

    if true == overrideValueFitsRegisteredType(reflect.TypeOf(""), reflect.PointerTo(reflect.TypeOf(0))) {
        t.Fatalf("expected a foreign value type to be refused")
    }

    if false == overrideValueFitsRegisteredType(reflect.TypeOf(&fitsProbeImplementer{}), reflect.TypeOf(&fitsProbeImplementer{})) {
        t.Fatalf("expected the identical pointer type to fit")
    }
}
