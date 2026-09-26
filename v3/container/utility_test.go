package container

import (
    "reflect"
    "testing"

    collisionalpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
)

type utilityProbe struct{}

type utilityProbeInterface interface {
    Probe()
}

/* every registration and every by-type lookup goes through this canonicalisation: a service declared from a provider returning *T is filed under *T, so asking with T has to reach the same entry or Has and Get disagree about the same container. An interface is its own canonical form — wrapping it in a pointer would file it under a type no caller ever asks with. */
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

/* the identity key is what the creation guard and the teardown are keyed on, so two DIFFERENT types sharing one key mean false cycles at resolution and merged nodes at close — the defect the container session repaired by refusing the second such registration. String() alone shares a key between same-named types of different packages; the import path is what separates them. */
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

/* the key reaches the named type through any depth of pointer, so *T and **T of one declaration key to the same package while keeping their own spellings apart. */
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

/* the derived service name is what a by-type registration is filed under, and it has to be import-path qualified for the reason the identity key is: two same-named types would otherwise take one name, and the second registration would be refused as a duplicate of a service it has nothing to do with. */
func TestDefaultServiceNameForType_QualifiesTheNameWithTheImportPath(t *testing.T) {
    alphaName := defaultServiceNameForType(reflect.TypeOf(&collisionalpha.Bus{}))
    betaName := defaultServiceNameForType(reflect.TypeOf(&collisionbeta.Bus{}))

    if alphaName == betaName {
        t.Fatalf("expected distinct derived names, both read %q", alphaName)
    }

    if "" == alphaName || "" == betaName {
        t.Fatalf("expected both names to be derived, got %q and %q", alphaName, betaName)
    }

    /* the canonicalisation happens inside, so asking with the value type derives the pointer's name */
    if alphaName != defaultServiceNameForType(reflect.TypeOf(collisionalpha.Bus{})) {
        t.Fatalf("expected the value type to derive the same name as its pointer")
    }

    if "" != defaultServiceNameForType(nil) {
        t.Fatalf("expected a nil type to derive no name")
    }
}

/* a builtin or unnamed type has no import path to qualify with and keeps its own spelling — the fallback is what stops the name from becoming a bare dot. */
func TestUniqueTypeName_FallsBackToTheSpellingForUnqualifiedTypes(t *testing.T) {
    if "*int" != uniqueTypeName(reflect.TypeOf(new(int))) {
        t.Fatalf("unexpected name for a pointer to a builtin: %q", uniqueTypeName(reflect.TypeOf(new(int))))
    }

    if "*struct {}" != uniqueTypeName(reflect.TypeOf(&struct{}{})) {
        t.Fatalf("unexpected name for a pointer to an unnamed struct: %q", uniqueTypeName(reflect.TypeOf(&struct{}{})))
    }
}

/* the any refusal is what keeps a service from being filed under the empty interface, which every value satisfies — the by-type resolution would then answer whichever registration reached the map first, for any type asked. A NON-empty interface is a legitimate service type and must not be caught by it. */
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

type fitsProbeOutsider struct{}

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

/* the key of an UNNAMED composite type — a slice, a map, a channel of a named type — reaches the import path of the named element it is built from; read through pointers alone it carried no path, so a slice of one package's Bus and a slice of another package's Bus of the same short name shared one key, and a declaration on the first passed as registered through the second. */
func TestTypeIdentityKey_KeepsTwoSameNamedPackagesApartOnACompositeType(t *testing.T) {
    pairs := [][2]reflect.Type{
        {reflect.TypeOf(&[]collisionalpha.Bus{}), reflect.TypeOf(&[]collisionbeta.Bus{})},
        {reflect.TypeOf(map[string]*collisionalpha.Bus{}), reflect.TypeOf(map[string]*collisionbeta.Bus{})},
        {reflect.TypeOf(make(chan collisionalpha.Bus)), reflect.TypeOf(make(chan collisionbeta.Bus))},
        {reflect.TypeOf([2]collisionalpha.Bus{}), reflect.TypeOf([2]collisionbeta.Bus{})},
    }

    for _, pair := range pairs {
        if pair[0].String() != pair[1].String() {
            t.Fatalf("expected the two probe types to share a String(), which is what makes this test meaningful, got %q and %q", pair[0].String(), pair[1].String())
        }

        if typeIdentityKey(pair[0]) == typeIdentityKey(pair[1]) {
            t.Fatalf("expected %s of two same-named packages to hold distinct keys, both read %q", pair[0].String(), typeIdentityKey(pair[0]))
        }
    }
}

/* the keys of named types, of pointers to them and of a builtin are exactly what they were: the identity key is what the creation guard, the type index and every teardown node key are keyed on, so a key that moved would be a node that moved. */
func TestTypeIdentityKey_LeavesTheKeysOfNamedTypesAndPointersUnchanged(t *testing.T) {
    alphaPath := "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"

    expected := map[reflect.Type]string{
        reflect.TypeOf(&collisionalpha.Bus{}):      alphaPath + "\x00*contract.Bus",
        reflect.TypeOf(collisionalpha.Bus{}):       alphaPath + "\x00contract.Bus",
        reflect.TypeOf(new(*collisionalpha.Bus)):   alphaPath + "\x00**contract.Bus",
        reflect.TypeOf(""):                         "\x00string",
        reflect.TypeOf(func() {}):                  "\x00func()",
    }

    for targetType, expectedKey := range expected {
        if expectedKey != typeIdentityKey(targetType) {
            t.Fatalf("expected the key of %s to read %q, got %q", targetType.String(), expectedKey, typeIdentityKey(targetType))
        }
    }
}
