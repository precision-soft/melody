package openapi

import (
    "encoding/json"
    neturl "net/url"
    "os"
    "os/exec"
    "reflect"
    "regexp"
    "sort"
    "strings"
    "testing"
    "time"
)

type selfEmbedNode struct {
    *selfEmbedNode
    Value string `json:"value"`
}

type mutualEmbedA struct {
    *mutualEmbedB
    A string `json:"a"`
}

type mutualEmbedB struct {
    *mutualEmbedA
    B string `json:"b"`
}

func TestBuildSchema_SelfEmbedDoesNotHang(t *testing.T) {
    done := make(chan struct{})
    go func() {
        buildSchema(reflect.TypeOf(selfEmbedNode{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(3 * time.Second):
        t.Fatal("buildSchema hung on a self-embedded struct: embed-promotion walk lacks a visited-type guard")
    }
}

func TestBuildSchema_MutualEmbedDoesNotHang(t *testing.T) {
    done := make(chan struct{})
    go func() {
        buildSchema(reflect.TypeOf(mutualEmbedA{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(3 * time.Second):
        t.Fatal("buildSchema hung on mutually-embedded structs: embed-promotion walk lacks a visited-type guard")
    }
}

func TestBuildSchema_ByteSliceIsBase64String(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    byteSlice := buildSchema(reflect.TypeOf([]byte{}), components, names, visited)
    if "string" != byteSlice.Type || "byte" != byteSlice.Format {
        t.Fatalf("expected []byte to be {string, byte}, got {%q, %q}", byteSlice.Type, byteSlice.Format)
    }

    stringSlice := buildSchema(reflect.TypeOf([]string{}), components, names, visited)
    if "array" != stringSlice.Type {
        t.Fatalf("expected []string to stay an array, got %q", stringSlice.Type)
    }

    byteArray := buildSchema(reflect.TypeOf([4]byte{}), components, names, visited)
    if "array" != byteArray.Type {
        t.Fatalf("expected [4]byte to stay an array, got %q", byteArray.Type)
    }
}

func TestBuildSchema_AmbiguousOwnJsonNameDropped(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    collisionType := reflect.StructOf([]reflect.StructField{
        {Name: "First", Type: stringType, Tag: `json:"dup"`},
        {Name: "Second", Type: stringType, Tag: `json:"dup"`},
        {Name: "Unique", Type: stringType, Tag: `json:"unique"`},
    })

    schema := buildSchema(collisionType, components, names, visited)

    if _, present := schema.Properties["dup"]; true == present {
        t.Fatalf("expected the ambiguous duplicate json name to be dropped to match encoding/json, got %+v", schema.Properties)
    }
    if _, present := schema.Properties["unique"]; false == present {
        t.Fatalf("expected the unique field to remain, got %+v", schema.Properties)
    }
}

func TestBuildSchema_ParenthesizedValidationConstraintsEmitted(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Name", Type: stringType, Tag: `json:"name" validate:"notBlank,min(value=3),max(value=64)"`},
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex(pattern=^a{1,2}$)"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    name := schema.Properties["name"]
    if nil == name.MinLength || 3 != *name.MinLength {
        t.Fatalf("expected MinLength 3 from min(value=3), got %v", name.MinLength)
    }
    if nil == name.MaxLength || 64 != *name.MaxLength {
        t.Fatalf("expected MaxLength 64 from max(value=64), got %v", name.MaxLength)
    }

    code := schema.Properties["code"]
    if "^a{1,2}$" != code.Pattern {
        t.Fatalf("expected Pattern ^a{1,2}$ from regex(pattern=^a{1,2}$) (comma inside braces preserved), got %q", code.Pattern)
    }
}

/* @info character-class and non-empty constraints must reach the spec */

func TestBuildSchema_CharacterClassConstraintsEmitPattern(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Letters", Type: stringType, Tag: `json:"letters" validate:"alpha"`},
        {Name: "Digits", Type: stringType, Tag: `json:"digits" validate:"numeric"`},
        {Name: "Mixed", Type: stringType, Tag: `json:"mixed" validate:"alphanumeric"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important the validator short-circuits on an empty string (it accepts ""), so the advertised class uses a * quantifier (which also matches "") rather than + which would reject the "" the validator accepts */
    if "^[a-zA-Z]*$" != schema.Properties["letters"].Pattern {
        t.Fatalf("expected alpha to advertise pattern ^[a-zA-Z]*$ (the validator accepts \"\"), got %q", schema.Properties["letters"].Pattern)
    }
    if "^[0-9]*$" != schema.Properties["digits"].Pattern {
        t.Fatalf("expected numeric to advertise pattern ^[0-9]*$, got %q", schema.Properties["digits"].Pattern)
    }
    if "^[a-zA-Z0-9]*$" != schema.Properties["mixed"].Pattern {
        t.Fatalf("expected alphanumeric to advertise pattern ^[a-zA-Z0-9]*$, got %q", schema.Properties["mixed"].Pattern)
    }
}

func TestBuildSchema_MultiplePatternRulesEmittedAsAllOf(t *testing.T) {
    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex=^[a-z]+$,alpha"`},
    })

    schema := buildSchema(requestType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    code := schema.Properties["code"]
    if nil == code {
        t.Fatalf("expected a code property")
    }

    /* @important the validator enforces BOTH the regex and the alpha pattern; a single OpenAPI `pattern` can hold only one (RE2 has no lookahead to AND them), so both must be advertised as allOf members rather than silently dropping all but the last */
    if 2 != len(code.AllOf) {
        t.Fatalf("expected two pattern rules to be advertised as two allOf members, got %d: %+v", len(code.AllOf), code)
    }

    advertised := map[string]bool{}
    for _, member := range code.AllOf {
        advertised[member.Pattern] = true
    }
    if false == advertised["^[a-z]+$"] || false == advertised["^[a-zA-Z]*$"] {
        t.Fatalf("expected both the regex and the alpha pattern to be advertised as allOf members, got %#v", advertised)
    }
    if "" != code.Pattern {
        t.Fatalf("expected the single pattern slot to stay empty when multiple patterns are emitted via allOf, got %q", code.Pattern)
    }
}

func TestBuildSchema_NotBlankAndNotEmptyEmitMinLength(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Name", Type: stringType, Tag: `json:"name" validate:"notBlank"`},
        {Name: "Title", Type: stringType, Tag: `json:"title" validate:"notEmpty"`},
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"notBlank,min(value=4)"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    if name := schema.Properties["name"]; nil == name.MinLength || 1 != *name.MinLength {
        t.Fatalf("expected notBlank to advertise minLength 1 so the spec rejects \"\" like the validator does, got %v", name.MinLength)
    }
    if title := schema.Properties["title"]; nil == title.MinLength || 1 != *title.MinLength {
        t.Fatalf("expected notEmpty to advertise minLength 1, got %v", title.MinLength)
    }

    /* @important an explicit min must win over the notBlank floor regardless of tag order */
    if code := schema.Properties["code"]; nil == code.MinLength || 4 != *code.MinLength {
        t.Fatalf("expected an explicit min(value=4) to override the notBlank minLength floor, got %v", code.MinLength)
    }
}

func TestBuildSchema_NotEmptyEmitsCollectionFloors(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    sliceType := reflect.TypeOf([]string{})
    mapType := reflect.TypeOf(map[string]string{})
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Tags", Type: sliceType, Tag: `json:"tags" validate:"notEmpty"`},
        {Name: "Labels", Type: mapType, Tag: `json:"labels" validate:"notEmpty"`},
        {Name: "Plain", Type: sliceType, Tag: `json:"plain"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important the validator rejects an empty slice/map, so the spec must advertise the matching collection floor rather than the string-only minLength */
    if tags := schema.Properties["tags"]; nil == tags.MinItems || 1 != *tags.MinItems {
        t.Fatalf("expected notEmpty on an array field to advertise minItems 1, got %v", tags.MinItems)
    }
    if tags := schema.Properties["tags"]; nil != tags.MinLength {
        t.Fatalf("expected notEmpty on an array field not to advertise a string minLength, got %v", tags.MinLength)
    }
    if labels := schema.Properties["labels"]; nil == labels.MinProperties || 1 != *labels.MinProperties {
        t.Fatalf("expected notEmpty on a map field to advertise minProperties 1, got %v", labels.MinProperties)
    }

    /* @important an untagged collection keeps no floor, proving the minItems comes from notEmpty and not from the array shape itself */
    if plain := schema.Properties["plain"]; nil != plain.MinItems {
        t.Fatalf("expected an untagged array field to advertise no minItems, got %v", plain.MinItems)
    }
}

func TestBuildSchema_NotEmptyOnFixedArrayIsVacuous(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    arrayType := reflect.TypeOf([2]int{})
    pointerArrayType := reflect.TypeOf(&[2]int{})
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Pair", Type: arrayType, Tag: `json:"pair" validate:"notEmpty"`},
        {Name: "MaybePair", Type: pointerArrayType, Tag: `json:"maybe_pair" validate:"notEmpty"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* the validator's length check on a fixed array can never fail, so the server accepts any array length; the spec must neither require the field nor floor its length */
    if pair := schema.Properties["pair"]; nil != pair.MinItems {
        t.Fatalf("expected notEmpty on a fixed-length array to advertise no minItems, got %v", pair.MinItems)
    }

    for _, name := range schema.Required {
        if "pair" == name {
            t.Fatalf("expected a non-pointer fixed-length array not to be required under notEmpty")
        }
    }

    /* a *[N]T stays required: the validator rejects the nil pointer */
    maybeRequired := false
    for _, name := range schema.Required {
        if "maybe_pair" == name {
            maybeRequired = true
        }
    }
    if false == maybeRequired {
        t.Fatalf("expected a pointer-to-fixed-array under notEmpty to stay required")
    }

    /* but its floor is the same vacuous measurement: once the nil was admitted, len(*[N]T) is N whatever the payload spelled — [] decodes zero-filled and passes — so minItems 1 would advertise a rejection the server never issues */
    if maybePair := schema.Properties["maybe_pair"]; nil != maybePair.MinItems {
        t.Fatalf("expected notEmpty behind a pointer to a fixed-length array to advertise no minItems, got %v", maybePair.MinItems)
    }
}

/* @info the vacuity of notEmpty on a fixed-length array must not dismantle the unsatisfiable marking another rule placed: dropping minItems 1 beside a maxItems 0 would leave the empty array advertised as valid while the validator rejects every payload */
func TestBuildSchema_FixedArrayNotEmptyKeepsUnsatisfiableMarking(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Codes", Type: reflect.TypeOf([3]int{}), Tag: `json:"codes" validate:"notEmpty,min=abc"`},
    })

    schema := buildSchema(requestType, components, names, visited)
    codes := schema.Properties["codes"]

    /* a malformed bound fails the field closed for every value, so the empty window must survive intact */
    if nil == codes.MinItems || 1 != *codes.MinItems {
        t.Fatalf("expected the unsatisfiable marking to keep minItems 1, got %v", codes.MinItems)
    }

    if nil == codes.MaxItems || 0 != *codes.MaxItems {
        t.Fatalf("expected the unsatisfiable marking to keep maxItems 0, got %v", codes.MaxItems)
    }
}

func TestBuildSchema_NotEmptyOnScalarIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    intType := reflect.TypeOf(0)
    floatType := reflect.TypeOf(float64(0))
    boolType := reflect.TypeOf(false)
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Count", Type: intType, Tag: `json:"count" validate:"notEmpty"`},
        {Name: "Ratio", Type: floatType, Tag: `json:"ratio" validate:"notEmpty"`},
        {Name: "Flag", Type: boolType, Tag: `json:"flag" validate:"notEmpty"`},
        {Name: "Plain", Type: intType, Tag: `json:"plain"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important notEmpty's validator rejects any non string/array/slice/map kind outright (constraint_not_empty.go default branch), so an integer/number field carrying it is unsatisfiable server-side; the spec must advertise the same impossible exclusive range (> 0 and < 0) instead of an unconstrained scalar a client would trust */
    count := schema.Properties["count"]
    if nil == count.Minimum || 0 != *count.Minimum || nil == count.Maximum || 0 != *count.Maximum ||
        nil == count.ExclusiveMinimum || false == *count.ExclusiveMinimum || nil == count.ExclusiveMaximum || false == *count.ExclusiveMaximum {
        t.Fatalf("expected notEmpty on an integer field to advertise an unsatisfiable exclusive range (> 0 and < 0), got %+v", count)
    }
    ratio := schema.Properties["ratio"]
    if nil == ratio.Minimum || nil == ratio.Maximum || nil == ratio.ExclusiveMinimum || nil == ratio.ExclusiveMaximum {
        t.Fatalf("expected notEmpty on a number field to advertise an unsatisfiable exclusive range, got %+v", ratio)
    }

    /* @important a boolean has no numeric or length facet to contradict, and an empty enum is invalid under the OpenAPI 3.0 meta-schema (enum requires minItems 1), so notEmpty advertises two contradictory single-value enums under allOf — a value can be neither both true and false, yet each enum is non-empty and spec-valid */
    flag := schema.Properties["flag"]
    if 2 != len(flag.AllOf) {
        t.Fatalf("expected notEmpty on a boolean field to advertise two contradictory allOf enums (unsatisfiable), got %+v", flag)
    }
    if nil == flag.AllOf[0].Enum || 1 != len(*flag.AllOf[0].Enum) || true != (*flag.AllOf[0].Enum)[0] ||
        nil == flag.AllOf[1].Enum || 1 != len(*flag.AllOf[1].Enum) || false != (*flag.AllOf[1].Enum)[0] {
        t.Fatalf("expected the boolean allOf to require both true and false (each a non-empty enum, so spec-valid), got %+v and %+v", flag.AllOf[0], flag.AllOf[1])
    }
    if nil != flag.Enum {
        t.Fatalf("expected no empty top-level enum on the boolean schema (invalid under OAS 3.0), got %+v", flag.Enum)
    }

    /* @important an untagged scalar keeps no constraint, proving the unsatisfiable markers come from notEmpty and not the scalar shape itself */
    if plain := schema.Properties["plain"]; nil != plain.Minimum || nil != plain.Maximum {
        t.Fatalf("expected an untagged integer field to advertise no numeric bound, got %+v", plain)
    }
}

func TestBuildSchema_NotEmptyOnTimeFieldIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    timeFieldType := reflect.TypeOf(time.Time{})
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "CreatedAt", Type: timeFieldType, Tag: `json:"createdAt" validate:"notEmpty"`},
        {Name: "UpdatedAt", Type: timeFieldType, Tag: `json:"updatedAt" validate:"notBlank"`},
        {Name: "DeletedAt", Type: timeFieldType, Tag: `json:"deletedAt"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important a time.Time renders as {type:string, format:date-time}, but notEmpty's validator reflects on the value whose kind is Struct (not string/array/slice/map) and rejects every value via its default branch (constraint_not_empty.go); the spec must advertise the same impossible length window (minLength 1, maxLength 0) instead of a satisfiable date-time string a client would trust */
    createdAt := schema.Properties["createdAt"]
    if "date-time" != createdAt.Format ||
        nil == createdAt.MinLength || 1 != *createdAt.MinLength ||
        nil == createdAt.MaxLength || 0 != *createdAt.MaxLength {
        t.Fatalf("expected notEmpty on a time.Time field to advertise an unsatisfiable date-time string (minLength 1, maxLength 0), got %+v", createdAt)
    }
    if true == createdAt.Nullable {
        t.Fatalf("expected notEmpty to clear nullable on a time.Time field, got %+v", createdAt)
    }

    /* @important notBlank's validator stringifies via fmt.Sprintf (constraint_not_blank.go), so a time.Time renders to a non-blank value and is accepted; the satisfiable date-time string with minLength 1 matches the validator and must NOT be marked unsatisfiable */
    updatedAt := schema.Properties["updatedAt"]
    if "date-time" != updatedAt.Format || nil == updatedAt.MinLength || 1 != *updatedAt.MinLength || nil != updatedAt.MaxLength {
        t.Fatalf("expected notBlank on a time.Time field to stay a satisfiable date-time string (minLength 1, no maxLength), got %+v", updatedAt)
    }

    /* @important an untagged time.Time keeps a plain date-time string, proving the unsatisfiable window comes from notEmpty and not the time.Time shape itself */
    deletedAt := schema.Properties["deletedAt"]
    if "date-time" != deletedAt.Format || nil != deletedAt.MinLength || nil != deletedAt.MaxLength {
        t.Fatalf("expected an untagged time.Time field to advertise an unconstrained date-time string, got %+v", deletedAt)
    }
}

func TestBuildSchema_RegexCharacterClassBracketPreserved(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex=^[)]a{2,3}b$"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    code := schema.Properties["code"]
    if "^[)]a{2,3}b$" != code.Pattern {
        t.Fatalf("expected the full pattern ^[)]a{2,3}b$ (a ')' inside a [...] class is a literal, and the comma in {2,3} must not split), matching the runtime validator, got %q", code.Pattern)
    }
}

func TestBuildSchema_MaxLengthAcceptsNonIntegerPrefix(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Name", Type: stringType, Tag: `json:"name" validate:"max=99.5"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    name := schema.Properties["name"]
    if nil == name.MaxLength || 99 != *name.MaxLength {
        t.Fatalf("expected MaxLength 99 from max=99.5 (validator truncates the leading integer via Sscanf), got %v", name.MaxLength)
    }
}

func TestBuildSchema_NegativeMinClampedNegativeMaxUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Low", Type: stringType, Tag: `json:"low" validate:"min=-5"`},
        {Name: "High", Type: stringType, Tag: `json:"high" validate:"max=-10"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important a negative min imposes no floor (MinLength.Validate uses len < min, never true for a negative bound, so the validator accepts every value); it stays clamped to a spec-valid minLength 0 advertising the same "no minimum" */
    low := schema.Properties["low"]
    if nil == low.MinLength || 0 != *low.MinLength {
        t.Fatalf("expected a negative min=-5 to be clamped to a spec-valid minLength 0, got %v", low.MinLength)
    }

    /* @important a negative max makes MaxLength.Validate (len > max) reject every value including "", so the field is unsatisfiable and the schema advertises an impossible length window (minLength 1, maxLength 0) rather than maxLength 0 which would advertise "" as valid */
    high := schema.Properties["high"]
    if nil == high.MinLength || 1 != *high.MinLength || nil == high.MaxLength || 0 != *high.MaxLength {
        t.Fatalf("expected a negative max=-10 to advertise an unsatisfiable string schema (minLength 1, maxLength 0), got: %+v", high)
    }
}

/* @info a negative max admits no non-null value, but MaxLength.Validate passes a nil pointer (dereferenceValue returns absent), so the validator still accepts null on a NULLABLE field. The mirror must therefore contradict only the value space and preserve the nullable advertisement — matching the integer/number/boolean branch — rather than clearing Nullable and advertising a null the validator accepts as invalid. A non-nullable field stays fully unsatisfiable. */
func TestApplyValidation_NegativeMaxOnNullableStringKeepsNullValid(t *testing.T) {
    nullable := &Schema{Type: "string", Nullable: true}
    applyValidation(nullable, "max(value=-1)", nil)

    if false == nullable.Nullable {
        t.Fatalf("expected a nullable string with a negative max to keep null valid (the validator accepts null), but Nullable was cleared")
    }
    if nil == nullable.MinLength || 1 != *nullable.MinLength || nil == nullable.MaxLength || 0 != *nullable.MaxLength {
        t.Fatalf("expected the non-null value space contradicted (minLength 1, maxLength 0), got: %+v", nullable)
    }

    nonNullable := &Schema{Type: "string"}
    applyValidation(nonNullable, "max(value=-1)", nil)

    if true == nonNullable.Nullable {
        t.Fatalf("expected a non-nullable string with a negative max to stay non-nullable")
    }
    if nil == nonNullable.MinLength || 1 != *nonNullable.MinLength || nil == nonNullable.MaxLength || 0 != *nonNullable.MaxLength {
        t.Fatalf("expected a non-nullable field advertised fully unsatisfiable (minLength 1, maxLength 0), got: %+v", nonNullable)
    }
}

func TestBuildSchema_UnsignedFieldCarriesZeroFloor(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    uintType := reflect.TypeOf(uint(0))
    intType := reflect.TypeOf(0)
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Plain", Type: uintType, Tag: `json:"plain"`},
        {Name: "Ceiling", Type: uintType, Tag: `json:"ceiling" validate:"lessThan=0"`},
        {Name: "NegativeCeiling", Type: uintType, Tag: `json:"negative_ceiling" validate:"lessThan=-3"`},
        {Name: "PositiveFloor", Type: uintType, Tag: `json:"positive_floor" validate:"greaterThan=0"`},
        {Name: "RaisedFloor", Type: uintType, Tag: `json:"raised_floor" validate:"greaterThan=5"`},
        {Name: "Signed", Type: intType, Tag: `json:"signed"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    plain := schema.Properties["plain"]
    if nil == plain.Minimum || 0 != *plain.Minimum || nil != plain.ExclusiveMinimum {
        t.Fatalf("expected a bare unsigned field to advertise an inclusive minimum 0, got %+v", plain)
    }

    ceiling := schema.Properties["ceiling"]
    if nil == ceiling.Minimum || 0 != *ceiling.Minimum || nil == ceiling.Maximum || 0 != *ceiling.Maximum ||
        nil == ceiling.ExclusiveMaximum || false == *ceiling.ExclusiveMaximum {
        t.Fatalf("expected lessThan=0 on an unsigned field to advertise an empty window (minimum 0, maximum 0 exclusive), got %+v", ceiling)
    }

    negativeCeiling := schema.Properties["negative_ceiling"]
    if nil == negativeCeiling.Minimum || 0 != *negativeCeiling.Minimum || nil == negativeCeiling.Maximum || -3 != *negativeCeiling.Maximum {
        t.Fatalf("expected lessThan=-3 on an unsigned field to advertise an empty window (minimum 0 above maximum -3), got %+v", negativeCeiling)
    }

    positiveFloor := schema.Properties["positive_floor"]
    if nil == positiveFloor.Minimum || 0 != *positiveFloor.Minimum || nil == positiveFloor.ExclusiveMinimum || false == *positiveFloor.ExclusiveMinimum {
        t.Fatalf("expected greaterThan=0 on an unsigned field to keep its exclusive zero minimum, got %+v", positiveFloor)
    }

    raisedFloor := schema.Properties["raised_floor"]
    if nil == raisedFloor.Minimum || 5 != *raisedFloor.Minimum || nil == raisedFloor.ExclusiveMinimum || false == *raisedFloor.ExclusiveMinimum {
        t.Fatalf("expected greaterThan=5 on an unsigned field to keep its exclusive minimum 5, got %+v", raisedFloor)
    }

    signed := schema.Properties["signed"]
    if nil != signed.Minimum {
        t.Fatalf("expected a bare signed field to advertise no minimum, got %+v", signed)
    }
}

/* @info a collection element carries no validate tag, so the unsigned floor belongs to the element schema; without it the items of a []uint advertise the negatives the decoder rejects */
func TestBuildSchema_UnsignedCollectionElementCarriesZeroFloor(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "List", Type: reflect.TypeOf([]uint{}), Tag: `json:"list"`},
        {Name: "Lookup", Type: reflect.TypeOf(map[string]uint32{}), Tag: `json:"lookup"`},
        {Name: "Fixed", Type: reflect.TypeOf([2]uint16{}), Tag: `json:"fixed"`},
        {Name: "Nested", Type: reflect.TypeOf([][]uint64{}), Tag: `json:"nested"`},
        {Name: "Optional", Type: reflect.TypeOf([]*uint8{}), Tag: `json:"optional"`},
        {Name: "Signed", Type: reflect.TypeOf([]int{}), Tag: `json:"signed"`},
        {Name: "Raw", Type: reflect.TypeOf([]byte{}), Tag: `json:"raw"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    carriesZeroFloor := func(elementSchema *Schema) bool {
        return nil != elementSchema && nil != elementSchema.Minimum && 0 == *elementSchema.Minimum && nil == elementSchema.ExclusiveMinimum
    }

    if false == carriesZeroFloor(schema.Properties["list"].Items) {
        t.Fatalf("expected the items of a []uint to advertise an inclusive minimum 0, got %+v", schema.Properties["list"].Items)
    }

    if false == carriesZeroFloor(schema.Properties["lookup"].AdditionalProperties) {
        t.Fatalf("expected the values of a map[string]uint32 to advertise an inclusive minimum 0, got %+v", schema.Properties["lookup"].AdditionalProperties)
    }

    if false == carriesZeroFloor(schema.Properties["fixed"].Items) {
        t.Fatalf("expected the items of a [2]uint16 to advertise an inclusive minimum 0, got %+v", schema.Properties["fixed"].Items)
    }

    /* the floor reaches the innermost element: the outer element is itself a collection, whose own items are built the same way */
    if false == carriesZeroFloor(schema.Properties["nested"].Items.Items) {
        t.Fatalf("expected the innermost items of a [][]uint64 to advertise an inclusive minimum 0, got %+v", schema.Properties["nested"].Items.Items)
    }

    /* a nullable element still cannot decode a negative number */
    if false == carriesZeroFloor(schema.Properties["optional"].Items) {
        t.Fatalf("expected the items of a []*uint8 to advertise an inclusive minimum 0, got %+v", schema.Properties["optional"].Items)
    }

    if nil != schema.Properties["signed"].Items.Minimum {
        t.Fatalf("expected the items of a []int to advertise no minimum, got %+v", schema.Properties["signed"].Items)
    }

    /* a []byte renders as a base64 string, not an array of integers, so no numeric floor applies to it */
    raw := schema.Properties["raw"]
    if "string" != raw.Type || "byte" != raw.Format || nil != raw.Minimum {
        t.Fatalf("expected a []byte to stay a byte-format string with no minimum, got %+v", raw)
    }
}

func TestBuildSchema_ParenthesizedRegexCommaInGroupPreserved(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex(pattern=^(a,b)$)"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    code := schema.Properties["code"]
    if "^(a,b)$" != code.Pattern {
        t.Fatalf("expected Pattern ^(a,b)$ from regex(pattern=^(a,b)$) (comma inside () group preserved, matching the runtime validator), got %q", code.Pattern)
    }
}

func TestBuildSchema_LiteralDashJsonNameNotOmitted(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Literal", Type: stringType, Tag: `json:"-,"`},
        {Name: "Normal", Type: stringType, Tag: `json:"normal"`},
        {Name: "Omitted", Type: stringType, Tag: `json:"-"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    if _, present := schema.Properties["-"]; false == present {
        t.Fatalf(`expected a property named "-" for the json:"-," field (encoding/json serializes it under the key "-"), got %+v`, schema.Properties)
    }
    if _, present := schema.Properties["normal"]; false == present {
        t.Fatalf("expected the normal field to be present, got %+v", schema.Properties)
    }
    if _, present := schema.Properties["Omitted"]; true == present {
        t.Fatalf(`expected the bare json:"-" field to be omitted, got %+v`, schema.Properties)
    }
}

func TestBuildSchema_EscapedCommaInRegexShorthandPreserved(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex=a\\,b,min=5"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    code := schema.Properties["code"]
    if nil == code {
        t.Fatalf("expected a schema for code, got %+v", schema.Properties)
    }
    if `a\,b` != code.Pattern {
        t.Fatalf(`expected pattern a\,b (escaped comma must not split, matching the runtime validator), got %q`, code.Pattern)
    }
    if nil == code.MinLength || 5 != *code.MinLength {
        t.Fatalf("expected MinLength 5 to survive the escaped-comma split, got %v", code.MinLength)
    }
}

func TestBuildSchema_QuotedCommaInRegexParamPreserved(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Code", Type: stringType, Tag: `json:"code" validate:"regex(pattern='a,b')"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    code := schema.Properties["code"]
    if nil == code {
        t.Fatalf("expected a schema for code, got %+v", schema.Properties)
    }
    if "'a,b'" != code.Pattern {
        t.Fatalf("expected pattern 'a,b' (quoted comma must not split, matching the runtime validator), got %q", code.Pattern)
    }
}

func TestBuildSchema_RegexShorthandEndingInParenPreserved(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Kind", Type: stringType, Tag: `json:"kind" validate:"regex=^(foo|bar)"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    kind := schema.Properties["kind"]
    if nil == kind {
        t.Fatalf("expected a schema for kind, got %+v", schema.Properties)
    }
    if "^(foo|bar)" != kind.Pattern {
        t.Fatalf("expected pattern ^(foo|bar): a shorthand regex value ending in ) must not be mis-parsed as the name(params) form, matching the runtime validator, got %q", kind.Pattern)
    }
}

func TestBuildSchema_ExplicitlyTaggedOwnFieldWinsImplicitCollision(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    collisionType := reflect.StructOf([]reflect.StructField{
        {Name: "Value", Type: reflect.TypeOf(0)},
        {Name: "Other", Type: reflect.TypeOf(""), Tag: `json:"Value"`},
    })

    schema := buildSchema(collisionType, components, names, visited)

    property, present := schema.Properties["Value"]
    if false == present {
        t.Fatalf("expected the explicitly-tagged field to win the implicit collision, got %+v", schema.Properties)
    }
    if "string" != property.Type {
        t.Fatalf("expected the tagged string field to win, got type %q", property.Type)
    }
}

type EmbedInner struct {
    A string `json:"a"`
    B string `json:"b"`
}

func TestBuildSchema_EmptyNameTagEmbeddedStructPromoted(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    outerType := reflect.StructOf([]reflect.StructField{
        {Name: "EmbedInner", Anonymous: true, Type: reflect.TypeOf(EmbedInner{}), Tag: `json:",omitempty"`},
        {Name: "C", Type: stringType, Tag: `json:"c"`},
    })

    schema := buildSchema(outerType, components, names, visited)

    for _, name := range []string{"a", "b", "c"} {
        if _, present := schema.Properties[name]; false == present {
            t.Fatalf("expected promoted property %q (encoding/json promotes an embedded struct tagged json:\",omitempty\"), got %+v", name, schema.Properties)
        }
    }

    if _, present := schema.Properties["EmbedInner"]; true == present {
        t.Fatalf("expected the embedded struct to be promoted, not emitted as a nested object property, got %+v", schema.Properties)
    }
}

type SchemaDiamondLeaf struct {
    CreatedBy string `validate:"notBlank"`
}

type SchemaDiamondMiddle struct {
    SchemaDiamondLeaf
}

type SchemaDiamondFirstBranch struct {
    SchemaDiamondMiddle
}

type SchemaDiamondSecondBranch struct {
    SchemaDiamondMiddle
}

type SchemaDiamondPayload struct {
    SchemaDiamondFirstBranch
    SchemaDiamondSecondBranch
    Title string `json:"title" validate:"notBlank"`
}

func TestBuildSchema_AStackedDiamondKeepsThePopulatedProperty(t *testing.T) {
    decoded := SchemaDiamondPayload{}

    if unmarshalErr := json.Unmarshal([]byte(`{"CreatedBy":"filled"}`), &decoded); nil != unmarshalErr {
        t.Fatalf("unexpected error: %v", unmarshalErr)
    }

    if "filled" != decoded.SchemaDiamondFirstBranch.SchemaDiamondMiddle.SchemaDiamondLeaf.CreatedBy {
        t.Fatalf(
            "expected encoding/json to populate the name promoted past the diamond, got %q",
            decoded.SchemaDiamondFirstBranch.SchemaDiamondMiddle.SchemaDiamondLeaf.CreatedBy,
        )
    }

    components := map[string]*Schema{}

    buildSchema(
        reflect.TypeOf(SchemaDiamondPayload{}),
        components,
        map[reflect.Type]string{},
        map[reflect.Type]bool{},
    )

    published, built := components["SchemaDiamondPayload"]
    if false == built {
        t.Fatalf("expected the payload to be registered as a component, got %+v", components)
    }

    if _, present := published.Properties["CreatedBy"]; false == present {
        t.Fatalf("expected the diamond-promoted property to be published, got %+v", published.Properties)
    }
}

/* @info nullable ref validation */

type emailRefTarget struct {
    Value string `json:"value"`
}

type emailRefParent struct {
    Child *emailRefTarget `json:"child" validate:"email"`
}

func TestApplyValidation_EmailFormatNotLeakedOntoNullableRefWrapper(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    buildSchema(reflect.TypeOf(emailRefParent{}), components, names, visited)

    parent := components["emailRefParent"]
    if nil == parent {
        t.Fatal("expected the parent struct to be registered in components")
    }

    child := parent.Properties["child"]
    if nil == child {
        t.Fatal("expected a 'child' property schema")
    }

    if 0 == len(child.AllOf) {
        t.Fatalf("expected the nullable struct reference to be wrapped in allOf; got %+v", child)
    }

    if "email" == child.Format {
        t.Fatal("the email format leaked onto the allOf-wrapped $ref; a validation keyword must not be a sibling of $ref/allOf")
    }
}

/* @info numeric bound */

func TestApplyValidation_EmailFormatOnlyOnStringType(t *testing.T) {
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Mail", Type: reflect.TypeOf(0), Tag: `json:"mail" validate:"email"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    mail := schema.Properties["mail"]
    if nil == mail {
        t.Fatalf("expected a mail property")
    }
    if "email" == mail.Format {
        t.Fatalf("email format must not be set on a non-string (integer) field, got format %q", mail.Format)
    }
}

func TestApplyValidation_PointerGreaterLessThanFieldIsRequired(t *testing.T) {
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Count", Type: reflect.TypeOf((*int)(nil)), Tag: `json:"count" validate:"greaterThan=5"`},
        {Name: "Limit", Type: reflect.TypeOf((*int)(nil)), Tag: `json:"limit" validate:"lessThan=9"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    for _, expected := range []string{"count", "limit"} {
        found := false
        for _, required := range schema.Required {
            if expected == required {
                found = true
                break
            }
        }
        if false == found {
            t.Fatalf("a pointer field with greaterThan/lessThan must be required (the validator rejects a nil pointer), missing %q in %#v", expected, schema.Required)
        }
    }
}

func TestApplyValidation_GreaterLessThanBoundParsesLeadingIntegerLikeValidator(t *testing.T) {
    floatType := reflect.TypeOf(float64(0))
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Low", Type: floatType, Tag: `json:"low" validate:"greaterThan=5x"`},
        {Name: "High", Type: floatType, Tag: `json:"high" validate:"lessThan=9y"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    low := schema.Properties["low"]
    if nil == low || nil == low.Minimum {
        t.Fatalf("expected a minimum on low (the validator parses the leading integer of `5x` and enforces >5), got %#v", low)
    }
    if 5.0 != *low.Minimum {
        t.Fatalf("expected minimum 5 from `greaterThan=5x`, got %v", *low.Minimum)
    }

    high := schema.Properties["high"]
    if nil == high || nil == high.Maximum {
        t.Fatalf("expected a maximum on high (the validator parses the leading integer of `9y` and enforces <9), got %#v", high)
    }
    if 9.0 != *high.Maximum {
        t.Fatalf("expected maximum 9 from `lessThan=9y`, got %v", *high.Maximum)
    }
}

func TestApplyValidation_GreaterLessThanBoundMatchesValidatorIntegerTruncation(t *testing.T) {
    floatType := reflect.TypeOf(float64(0))
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Price", Type: floatType, Tag: `json:"price" validate:"greaterThan=9.99"`},
        {Name: "Count", Type: floatType, Tag: `json:"count" validate:"lessThan=130.9"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    price := schema.Properties["price"]
    if nil == price || nil == price.Minimum {
        t.Fatalf("expected a minimum on price, got %#v", price)
    }
    if 9.0 != *price.Minimum {
        t.Fatalf("the runtime validator truncates greaterThan=9.99 to an integer bound (>9); the spec minimum must match, got %v", *price.Minimum)
    }

    count := schema.Properties["count"]
    if nil == count || nil == count.Maximum {
        t.Fatalf("expected a maximum on count, got %#v", count)
    }
    if 130.0 != *count.Maximum {
        t.Fatalf("the runtime validator truncates lessThan=130.9 to an integer bound (<130); the spec maximum must match, got %v", *count.Maximum)
    }
}

/* @info bare and valued constraint defaults */

func TestApplyValidation_BareStringConstraintsMirrorValidatorDefaults(t *testing.T) {
    minSchema := &Schema{Type: "string"}
    applyValidation(minSchema, "min", nil)
    if nil == minSchema.MinLength || 1 != *minSchema.MinLength {
        t.Fatalf("expected bare min to advertise minLength 1, got %v", minSchema.MinLength)
    }

    maxSchema := &Schema{Type: "string"}
    applyValidation(maxSchema, "max", nil)
    if nil == maxSchema.MaxLength || 100 != *maxSchema.MaxLength {
        t.Fatalf("expected bare max to advertise maxLength 100, got %v", maxSchema.MaxLength)
    }
}

func TestApplyValidation_BareNumericConstraintsMirrorValidatorDefaults(t *testing.T) {
    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan", nil)
    if nil == greaterThanSchema.Minimum || 0 != *greaterThanSchema.Minimum {
        t.Fatalf("expected bare greaterThan to advertise minimum 0, got %v", greaterThanSchema.Minimum)
    }
    if nil == greaterThanSchema.ExclusiveMinimum || false == *greaterThanSchema.ExclusiveMinimum {
        t.Fatalf("expected bare greaterThan to advertise an exclusive minimum")
    }

    lessThanSchema := &Schema{Type: "number"}
    applyValidation(lessThanSchema, "lessThan", nil)
    if nil == lessThanSchema.Maximum || 0 != *lessThanSchema.Maximum {
        t.Fatalf("expected bare lessThan to advertise maximum 0, got %v", lessThanSchema.Maximum)
    }
    if nil == lessThanSchema.ExclusiveMaximum || false == *lessThanSchema.ExclusiveMaximum {
        t.Fatalf("expected bare lessThan to advertise an exclusive maximum")
    }
}

func TestApplyValidation_ValuedConstraintsStillHonourTheirValue(t *testing.T) {
    minSchema := &Schema{Type: "string"}
    applyValidation(minSchema, "min(value=3)", nil)
    if nil == minSchema.MinLength || 3 != *minSchema.MinLength {
        t.Fatalf("expected min(value=3) to advertise minLength 3, got %v", minSchema.MinLength)
    }

    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan(value=5)", nil)
    if nil == greaterThanSchema.Minimum || 5 != *greaterThanSchema.Minimum {
        t.Fatalf("expected greaterThan(value=5) to advertise minimum 5, got %v", greaterThanSchema.Minimum)
    }
}

/* @info L2: a value-less min defaults to minLength 1 but must be raise-only — it may not lower a higher floor an earlier rule already set. The validator enforces every rule (effective floor stays 5), so advertising minLength 1 would offer values the validator rejects. Mirrors the value-less max branch, which was already tighten-only. */
func TestApplyValidation_ValuelessMinIsRaiseOnly(t *testing.T) {
    afterHigh := &Schema{Type: "string"}
    applyValidation(afterHigh, "min(value=5),min", nil)
    if nil == afterHigh.MinLength || 5 != *afterHigh.MinLength {
        t.Fatalf("expected a value-less min not to lower an existing minLength 5, got %v", afterHigh.MinLength)
    }

    beforeHigh := &Schema{Type: "string"}
    applyValidation(beforeHigh, "min,min(value=5)", nil)
    if nil == beforeHigh.MinLength || 5 != *beforeHigh.MinLength {
        t.Fatalf("expected a real min(value=5) to win over a value-less min regardless of order, got %v", beforeHigh.MinLength)
    }

    bare := &Schema{Type: "string"}
    applyValidation(bare, "min", nil)
    if nil == bare.MinLength || 1 != *bare.MinLength {
        t.Fatalf("expected a bare min alone to still advertise minLength 1, got %v", bare.MinLength)
    }
}

/* @info L3: the mirror decides a numeric bound is unsatisfiable via parseLeadingInt, and that decision must match the validator's parseIntStrict (validation/validation_rule.go) so the spec never advertises satisfiability the validator does not honour. Both share the same fmt.Sscanf("%d") acceptance; this locks parseLeadingInt against that contract over the boundary cases (a leading integer is accepted, trailing junk is tolerated, a non-numeric or empty string is rejected). */
func TestParseLeadingIntMatchesValidator(t *testing.T) {
    cases := []struct {
        input  string
        wantOk bool
        want   int
    }{
        {"5", true, 5},
        {"-5", true, -5},
        {"0", true, 0},
        {"5abc", true, 5},
        {"abc", false, 0},
        {"", false, 0},
    }

    for _, testCase := range cases {
        got, ok := parseLeadingInt(testCase.input)
        if ok != testCase.wantOk || (true == ok && got != testCase.want) {
            t.Fatalf(
                "parseLeadingInt(%q) = (%d, %v), want (%d, %v)",
                testCase.input, got, ok, testCase.want, testCase.wantOk,
            )
        }
    }
}

/* @info a parenthesized parameter that lacks '=' (a stray-paren typo such as min(5)/notEmpty(foo)/email(x)) makes the validator's parseValidationTag reject the whole tag with a value-independent "invalid validation tag syntax" error, so the field accepts no value; the mirror's splitRule silently dropped the malformed pair and advertised a satisfiable schema, so this asserts the field is now advertised unsatisfiable */
func TestApplyValidation_InvalidTagSyntaxIsUnsatisfiable(t *testing.T) {
    for _, tag := range []string{"notEmpty(foo)", "min(x)", "min(5)", "email(x)"} {
        schema := &Schema{Type: "string"}
        applyValidation(schema, tag, nil)

        if nil == schema.MinLength || 1 != *schema.MinLength || nil == schema.MaxLength || 0 != *schema.MaxLength {
            t.Fatalf("expected %q to advertise an unsatisfiable string (minLength 1, maxLength 0), got minLength=%v maxLength=%v", tag, schema.MinLength, schema.MaxLength)
        }
        if true == schema.Nullable {
            t.Fatalf("expected %q to clear nullable on the unsatisfiable field", tag)
        }
    }

    /* @important a well-formed parenthesized tag must NOT be swept up by the syntax guard: min(value=3) still advertises its real bound */
    valid := &Schema{Type: "string"}
    applyValidation(valid, "min(value=3)", nil)
    if nil == valid.MinLength || 3 != *valid.MinLength || nil != valid.MaxLength {
        t.Fatalf("expected min(value=3) to stay satisfiable with minLength 3, got minLength=%v maxLength=%v", valid.MinLength, valid.MaxLength)
    }
}

/* @info a malformed numeric/length tag makes the validator fail the field closed, so the spec advertises an unsatisfiable schema rather than a passable default */

type malformedBoundRequest struct {
    Name string `json:"name" validate:"max=abc"`
    Code string `json:"code" validate:"min=xyz"`
}

func TestBuildSchema_MalformedMinMaxBoundIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(malformedBoundRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["malformedBoundRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important the validator rejects every value of a field whose numeric tag cannot be parsed, so the string schema must reject every value too (an impossible length window) rather than advertise a passable default a client would trust */
    for _, fieldName := range []string{"name", "code"} {
        property := schema.Properties[fieldName]
        if nil == property || nil == property.MinLength || 1 != *property.MinLength || nil == property.MaxLength || 0 != *property.MaxLength {
            t.Fatalf("expected a malformed numeric tag to advertise an unsatisfiable string schema (minLength 1, maxLength 0), field %q got: %+v", fieldName, property)
        }
    }
}

type malformedNumericBoundRequest struct {
    Floor   int `json:"floor" validate:"greaterThan=abc"`
    Ceiling int `json:"ceiling" validate:"lessThan=xyz"`
}

func TestBuildSchema_MalformedGreaterLessThanBoundIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(malformedNumericBoundRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["malformedNumericBoundRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important the validator rejects every value of a field whose greaterThan/lessThan tag cannot be parsed, so the number schema must be unsatisfiable: an empty exclusive range that is both > 0 and < 0 */
    for _, fieldName := range []string{"floor", "ceiling"} {
        property := schema.Properties[fieldName]
        if nil == property ||
            nil == property.Minimum || 0 != *property.Minimum || nil == property.ExclusiveMinimum || false == *property.ExclusiveMinimum ||
            nil == property.Maximum || 0 != *property.Maximum || nil == property.ExclusiveMaximum || false == *property.ExclusiveMaximum {
            t.Fatalf("expected a malformed numeric bound to advertise an unsatisfiable number schema (>0 and <0), field %q got: %+v", fieldName, property)
        }
    }
}

type byteEmailRequest struct {
    Blob []byte `json:"blob" validate:"email"`
}

func TestBuildSchema_EmailDoesNotClobberStructuralByteFormat(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(byteEmailRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["byteEmailRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    blob := schema.Properties["blob"]
    if nil == blob || "string" != blob.Type || "byte" != blob.Format {
        t.Fatalf("expected a []byte field to keep format byte even with validate:email, got: %+v", blob)
    }
}

/* @info notBlank/notEmpty reject a null pointer, so the spec must not advertise the field nullable */

func TestApplyValidation_NotBlankNotEmptyPointerFieldNotNullable(t *testing.T) {
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Name", Type: reflect.TypeOf((*string)(nil)), Tag: `json:"name" validate:"notBlank"`},
        {Name: "Title", Type: reflect.TypeOf((*string)(nil)), Tag: `json:"title" validate:"notEmpty"`},
        {Name: "Tags", Type: reflect.TypeOf((*[]string)(nil)), Tag: `json:"tags" validate:"notEmpty"`},
        {Name: "Labels", Type: reflect.TypeOf((*map[string]string)(nil)), Tag: `json:"labels" validate:"notEmpty"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    /* @important the validator rejects a null pointer for notBlank/notEmpty (NotBlank.Validate fails the deref, NotEmpty.Validate rejects a nil pointer), so a pointer field carrying either must not be advertised nullable, otherwise a client trusting the spec sends null and is then rejected — the same fix already applied to greaterThan/lessThan */
    for _, name := range []string{"name", "title", "tags", "labels"} {
        property := schema.Properties[name]
        if nil == property {
            t.Fatalf("expected a %q property schema", name)
        }
        if true == property.Nullable {
            t.Fatalf("expected the pointer field %q with notBlank/notEmpty not to be advertised nullable (the validator rejects null), got nullable=true", name)
        }
    }
}

/* @info a degenerate min=0 must not suppress the notEmpty/notBlank non-empty floor, in either tag order */

func TestBuildSchema_DegenerateMinZeroDoesNotSuppressNotEmptyFloor(t *testing.T) {
    stringType := reflect.TypeOf("")
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "A", Type: stringType, Tag: `json:"a" validate:"min=0,notEmpty"`},
        {Name: "B", Type: stringType, Tag: `json:"b" validate:"notEmpty,min=0"`},
        {Name: "C", Type: stringType, Tag: `json:"c" validate:"min=0,notBlank"`},
        {Name: "D", Type: stringType, Tag: `json:"d" validate:"notBlank,min=0"`},
        {Name: "E", Type: stringType, Tag: `json:"e" validate:"min=5,notEmpty"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    /* @important the validator rejects "" for notEmpty/notBlank, so a degenerate min=0 (whose minLength 0 floor advertises "" as valid) must be raised to 1 regardless of which side of the tag it sits on */
    for _, name := range []string{"a", "b", "c", "d"} {
        property := schema.Properties[name]
        if nil == property || nil == property.MinLength || 1 != *property.MinLength {
            t.Fatalf("expected a degenerate min=0 combined with notEmpty/notBlank to advertise minLength 1, field %q got %+v", name, property)
        }
    }

    /* @important an explicit min >= 1 still wins over the notEmpty floor */
    if e := schema.Properties["e"]; nil == e || nil == e.MinLength || 5 != *e.MinLength {
        t.Fatalf("expected an explicit min=5 to win over the notEmpty floor, got %+v", e)
    }
}

/* @info a constraint whose validator rejects the field's underlying kind outright (notEmpty on a struct, greaterThan/lessThan on a non-numeric) must advertise the field unsatisfiable, mirroring the scalar/time.Time handling */

type nestedNamedStruct struct {
    Value string `json:"value"`
}

type notEmptyOnStructRequest struct {
    Named    nestedNamedStruct  `json:"named" validate:"notEmpty"`
    NamedPtr *nestedNamedStruct `json:"namedPtr" validate:"notEmpty"`
    Inline   struct {
        Field string `json:"field"`
    } `json:"inline" validate:"notEmpty"`
}

/* isImpossibleObject reports whether a member EXCLUDES every value, rather than whether it merely carries a particular pair of keywords.

The distinction is the whole assertion. `minProperties: 1` beside `maxProperties: 0` reads as impossible, and is — for an object. JSON Schema draft-04 §5.4.1 and §5.4.2, the dialect this document declares, apply both to object instances only and ignore them for every other type, so the same pair sitting beside a $ref that resolves to a named collection excludes nothing at all and `["a"]` satisfies the conjunction the mirror published as unsatisfiable. Asserting on the keywords let that through; asserting on the exclusion does not, whatever form the exclusion takes. */
func isImpossibleObject(schema *Schema) bool {
    if nil == schema {
        return false
    }

    /* `not: {}` — the empty schema admits everything, so its negation admits nothing, for every instance type */
    if nil != schema.Not && true == isEmptySchema(schema.Not) {
        return true
    }

    /* the object-level contradiction still counts where the member itself says it is an object, because there the keywords do bind */
    if "object" == schema.Type &&
        nil != schema.MinProperties && 1 == *schema.MinProperties &&
        nil != schema.MaxProperties && 0 == *schema.MaxProperties {
        return true
    }

    /* the collection-level contradiction, likewise, where the member says it is one */
    if "array" == schema.Type &&
        nil != schema.MinItems && 1 == *schema.MinItems &&
        nil != schema.MaxItems && 0 == *schema.MaxItems {
        return true
    }

    return false
}

/* isEmptySchema reports whether a schema constrains nothing, which is what makes its negation exclude everything. */
func isEmptySchema(schema *Schema) bool {
    if nil == schema {
        return false
    }

    return "" == schema.Ref && "" == schema.Type && 0 == len(schema.Properties) && 0 == len(schema.AllOf) &&
        nil == schema.Items && nil == schema.Not && nil == schema.Enum &&
        nil == schema.MinProperties && nil == schema.MaxProperties &&
        nil == schema.MinItems && nil == schema.MaxItems &&
        nil == schema.MinLength && nil == schema.MaxLength &&
        nil == schema.Minimum && nil == schema.Maximum && "" == schema.Pattern
}

func TestBuildSchema_NotEmptyOnStructIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(notEmptyOnStructRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["notEmptyOnStructRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important a named struct field renders as a $ref; notEmpty's validator rejects a struct value outright, so the $ref must be wrapped in a contradictory object constraint under allOf rather than advertised as a satisfiable object */
    named := schema.Properties["named"]
    if nil == named || "" != named.Ref || 2 != len(named.AllOf) {
        t.Fatalf("expected named struct notEmpty to wrap the $ref in a contradictory allOf, got %+v", named)
    }
    if "#/components/schemas/nestedNamedStruct" != named.AllOf[0].Ref || false == isImpossibleObject(named.AllOf[1]) {
        t.Fatalf("expected allOf of [$ref, impossible object], got %+v", named)
    }
    if true == named.Nullable {
        t.Fatalf("expected the unsatisfiable struct field to not be advertised as nullable, got %+v", named)
    }

    /* @important a *struct field is also rejected (notEmpty rejects a null pointer and a struct value), so its nullable allOf-wrapped $ref must gain the same contradiction and lose nullable */
    namedPtr := schema.Properties["namedPtr"]
    if nil == namedPtr || true == namedPtr.Nullable || 0 == len(namedPtr.AllOf) || false == isImpossibleObject(namedPtr.AllOf[len(namedPtr.AllOf)-1]) {
        t.Fatalf("expected pointer-to-struct notEmpty to be an unsatisfiable, non-nullable allOf, got %+v", namedPtr)
    }

    /* @important an inline struct renders as an object with no additionalProperties; the validator rejects the struct value, so advertise an impossible object */
    inline := schema.Properties["inline"]
    if false == isImpossibleObject(inline) {
        t.Fatalf("expected inline struct notEmpty to advertise an impossible object (minProperties 1, maxProperties 0), got %+v", inline)
    }
}

type numericOnNonNumericRequest struct {
    Code  string         `json:"code" validate:"greaterThan=0"`
    Flag  bool           `json:"flag" validate:"lessThan=1"`
    Items []string       `json:"items" validate:"greaterThan=0"`
    Bag   map[string]int `json:"bag" validate:"lessThan=0"`
}

func TestBuildSchema_GreaterLessThanOnNonNumericIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(numericOnNonNumericRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["numericOnNonNumericRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important greaterThan/lessThan's validator rejects a non-numeric value outright ("value must be numeric"), so a string field is unsatisfiable: an impossible length window */
    code := schema.Properties["code"]
    if nil == code || nil == code.MinLength || 1 != *code.MinLength || nil == code.MaxLength || 0 != *code.MaxLength {
        t.Fatalf("expected greaterThan on a string to advertise an unsatisfiable string (minLength 1, maxLength 0), got %+v", code)
    }

    /* @important a boolean carries no length/numeric facet, so unsatisfiability is two contradictory single-value enums under allOf */
    flag := schema.Properties["flag"]
    if nil == flag || 2 != len(flag.AllOf) || nil == flag.AllOf[0].Enum || nil == flag.AllOf[1].Enum {
        t.Fatalf("expected lessThan on a boolean to advertise contradictory enums under allOf, got %+v", flag)
    }

    /* @important an array field is unsatisfiable: minItems 1 with maxItems 0 */
    items := schema.Properties["items"]
    if nil == items || nil == items.MinItems || 1 != *items.MinItems || nil == items.MaxItems || 0 != *items.MaxItems {
        t.Fatalf("expected greaterThan on an array to advertise an impossible array (minItems 1, maxItems 0), got %+v", items)
    }

    /* @important a map renders as an object; it is unsatisfiable: minProperties 1 with maxProperties 0 */
    bag := schema.Properties["bag"]
    if false == isImpossibleObject(bag) {
        t.Fatalf("expected lessThan on a map to advertise an impossible object (minProperties 1, maxProperties 0), got %+v", bag)
    }
}

type notBlankNullableInner struct {
    Value string `json:"value"`
}

func TestBuildSchema_NotBlankClearsNullableOnNonStringPointers(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    intPtr := reflect.TypeOf((*int)(nil))
    boolPtr := reflect.TypeOf((*bool)(nil))
    slicePtr := reflect.TypeOf((*[]int)(nil))
    mapPtr := reflect.TypeOf((*map[string]string)(nil))
    structPtr := reflect.TypeOf((*notBlankNullableInner)(nil))
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Count", Type: intPtr, Tag: `json:"count" validate:"notBlank"`},
        {Name: "Flag", Type: boolPtr, Tag: `json:"flag" validate:"notBlank"`},
        {Name: "Tags", Type: slicePtr, Tag: `json:"tags" validate:"notBlank"`},
        {Name: "Labels", Type: mapPtr, Tag: `json:"labels" validate:"notBlank"`},
        {Name: "Obj", Type: structPtr, Tag: `json:"obj" validate:"notBlank"`},
        {Name: "Plain", Type: intPtr, Tag: `json:"plain"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important notBlank rejects a nil pointer for a field of any kind (dereferenceValue returns ok=false), so a non-string pointer field must not advertise null as valid — but it stays satisfiable because the validator accepts any non-null value (%v is non-blank), so no length/items/properties floor and no unsatisfiable marker is added */
    count := schema.Properties["count"]
    if true == count.Nullable || nil != count.MinLength || nil != count.Minimum || nil != count.Maximum {
        t.Fatalf("expected notBlank on a *int to clear nullable and add no bound, got %+v", count)
    }
    flag := schema.Properties["flag"]
    if true == flag.Nullable || nil != flag.AllOf {
        t.Fatalf("expected notBlank on a *bool to clear nullable and stay satisfiable (no contradictory allOf), got %+v", flag)
    }
    tags := schema.Properties["tags"]
    if true == tags.Nullable || nil != tags.MinItems {
        t.Fatalf("expected notBlank on a *[]int to clear nullable and add no minItems, got %+v", tags)
    }
    labels := schema.Properties["labels"]
    if true == labels.Nullable || nil != labels.MinProperties {
        t.Fatalf("expected notBlank on a *map to clear nullable and add no minProperties, got %+v", labels)
    }

    /* @important a *struct renders as a nullable allOf-wrapped $ref; notBlank rejects null but accepts a non-nil struct, so clear only nullable and keep the $ref satisfiable (no impossible-object contradiction sibling) */
    obj := schema.Properties["obj"]
    if true == obj.Nullable {
        t.Fatalf("expected notBlank on a *struct to clear nullable, got %+v", obj)
    }
    if true == isImpossibleObject(obj) {
        t.Fatalf("expected notBlank on a *struct to stay satisfiable (validator accepts a non-nil struct), got %+v", obj)
    }

    /* @important an untagged pointer keeps nullable, proving the clear comes from notBlank and not the pointer shape itself */
    if plain := schema.Properties["plain"]; false == plain.Nullable {
        t.Fatalf("expected an untagged *int field to stay nullable, got %+v", plain)
    }
}

func TestBuildSchema_MaxOnNonStringScalar(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    intType := reflect.TypeOf(0)
    boolType := reflect.TypeOf(false)
    intPtr := reflect.TypeOf((*int)(nil))
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Zero", Type: intType, Tag: `json:"zero" validate:"max(value=0)"`},
        {Name: "Bad", Type: intType, Tag: `json:"bad" validate:"max(value=abc)"`},
        {Name: "Flag", Type: boolType, Tag: `json:"flag" validate:"max(value=3)"`},
        {Name: "Ok", Type: intType, Tag: `json:"ok" validate:"max(value=5)"`},
        {Name: "NullableZero", Type: intPtr, Tag: `json:"nullableZero" validate:"max(value=0)"`},
        {Name: "Plain", Type: intType, Tag: `json:"plain"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    /* @important MaxLength stringifies any value via %v and rejects len > max; the shortest integer stringification is 1 character, so max=0 admits no non-null value — a non-nullable integer field is fully unsatisfiable (impossible exclusive range and nullable cleared) */
    zero := schema.Properties["zero"]
    if nil == zero.Minimum || 0 != *zero.Minimum || nil == zero.Maximum || 0 != *zero.Maximum ||
        nil == zero.ExclusiveMinimum || nil == zero.ExclusiveMaximum || true == zero.Nullable {
        t.Fatalf("expected max=0 on an int to advertise an unsatisfiable exclusive range (> 0 and < 0), got %+v", zero)
    }

    /* @important a malformed max fails the field closed at the validator (parseIntStrict), so the scalar is unsatisfiable too */
    bad := schema.Properties["bad"]
    if nil == bad.Minimum || nil == bad.Maximum {
        t.Fatalf("expected a malformed max on an int to advertise an unsatisfiable range, got %+v", bad)
    }

    /* @important the shortest boolean stringification is "true" (4 characters), so max=3 admits neither true nor false — unsatisfiable via two contradictory allOf enums */
    flag := schema.Properties["flag"]
    if 2 != len(flag.AllOf) {
        t.Fatalf("expected max=3 on a boolean to advertise two contradictory allOf enums, got %+v", flag)
    }

    /* @important max=5 admits at least one integer ("0".."9" are 1 character); there is no exact OpenAPI facet for a stringified-length ceiling, so the field is left unconstrained rather than mis-advertised as unsatisfiable */
    ok := schema.Properties["ok"]
    if nil != ok.Minimum || nil != ok.Maximum || nil != ok.AllOf {
        t.Fatalf("expected a satisfiable max=5 on an int to add no numeric/allOf constraint, got %+v", ok)
    }

    /* @important a nil pointer passes MaxLength (Validate returns nil for ok=false), so a nullable scalar with max=0 still accepts null: contradict only the non-null value space while keeping nullable advertised */
    nullableZero := schema.Properties["nullableZero"]
    if false == nullableZero.Nullable {
        t.Fatalf("expected max=0 on a *int to keep nullable (the validator accepts null), got %+v", nullableZero)
    }
    if nil == nullableZero.Minimum || nil == nullableZero.Maximum || nil == nullableZero.ExclusiveMinimum || nil == nullableZero.ExclusiveMaximum {
        t.Fatalf("expected max=0 on a *int to advertise an impossible non-null exclusive range, got %+v", nullableZero)
    }

    /* @important an untagged scalar keeps no bound, proving the markers come from max and not the scalar shape itself */
    if plain := schema.Properties["plain"]; nil != plain.Minimum || nil != plain.Maximum {
        t.Fatalf("expected an untagged int field to advertise no numeric bound, got %+v", plain)
    }
}

type paramsOnNonParameterizableRequest struct {
    Email string `json:"email" validate:"email(strict=yes)"`
    Name  string `json:"name" validate:"notBlank(mode=hard)"`
}

func TestBuildSchema_ParamsOnNonParameterizableConstraintIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(paramsOnNonParameterizableRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["paramsOnNonParameterizableRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important the validator fails closed when a tag carries parameters a non-parameterizable constraint cannot consume (createConstraintWithParams rejects the rule), so the spec must advertise the field as unsatisfiable rather than as a satisfiable string a client would trust */
    for _, fieldName := range []string{"email", "name"} {
        property := schema.Properties[fieldName]
        if nil == property || nil == property.MinLength || 1 != *property.MinLength || nil == property.MaxLength || 0 != *property.MaxLength {
            t.Fatalf("expected params on a non-parameterizable constraint to advertise an unsatisfiable string schema, field %q got: %+v", fieldName, property)
        }
    }
}

type unconsumedParameterizedParamsRequest struct {
    Name  string `json:"name" validate:"min(len=8)"`
    Code  string `json:"code" validate:"regex(re=^[0-9]{4}$)"`
    Count int    `json:"count" validate:"greaterThan(min=18)"`
}

func TestBuildSchema_UnconsumedParameterizedParamsIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(unconsumedParameterizedParamsRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["unconsumedParameterizedParamsRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important a parameterizable constraint that receives parameters without its recognized key fails the rule closed in the validator (WithParams returns an error, createConstraintWithParams rejects the rule) before Constraint.Validate runs, so the spec must advertise the field unsatisfiable rather than as the satisfiable default bound (minLength 1, the match-all `.*` pattern, or > 0) a client would trust */
    for _, fieldName := range []string{"name", "code"} {
        property := schema.Properties[fieldName]
        if nil == property || nil == property.MinLength || 1 != *property.MinLength || nil == property.MaxLength || 0 != *property.MaxLength {
            t.Fatalf("expected unconsumed parameterized params to advertise an unsatisfiable string schema, field %q got: %+v", fieldName, property)
        }
    }

    count := schema.Properties["count"]
    if nil == count || true == count.Nullable || nil == count.Minimum || nil == count.Maximum || *count.Minimum != *count.Maximum {
        t.Fatalf("expected unconsumed parameterized params on a numeric field to advertise an empty exclusive range, got: %+v", count)
    }
}

type paramsOnNonParameterizableStructRequest struct {
    Inline *paramsOnNonParameterizableRequest `json:"inline" validate:"notBlank(mode=hard)"`
    Plain  paramsOnNonParameterizableRequest  `json:"plain" validate:"email(strict=yes)"`
}

func TestBuildSchema_ParamsOnNonParameterizableConstraintOnStructIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(paramsOnNonParameterizableStructRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["paramsOnNonParameterizableStructRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important the validator fails the rule closed before Constraint.Validate runs, so a struct field ($ref or nullable allOf-wrapped $ref) carrying params on a non-parameterizable constraint accepts no value either — the early-return branch of applyValidation must advertise it unsatisfiable (contradictory allOf, mirroring notEmpty-on-struct), not as a satisfiable object */
    for _, fieldName := range []string{"inline", "plain"} {
        property := schema.Properties[fieldName]
        if nil == property {
            t.Fatalf("expected the %q property to exist", fieldName)
        }

        if true == property.Nullable || 0 == len(property.AllOf) || false == isImpossibleObject(property.AllOf[len(property.AllOf)-1]) {
            t.Fatalf("expected params on a non-parameterizable constraint over a struct field to advertise an unsatisfiable, non-nullable allOf, field %q got: %+v", fieldName, property)
        }
    }
}

type malformedMinNonStringRequest struct {
    Age   int      `json:"age" validate:"min=abc"`
    Score float64  `json:"score" validate:"min="`
    Ok    bool     `json:"ok" validate:"min=xyz"`
    Tags  []string `json:"tags" validate:"min=nan"`
}

func TestBuildSchema_MalformedMinOnNonStringFieldsIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(malformedMinNonStringRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["malformedMinNonStringRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important a malformed min value fails the rule closed for a field of ANY kind (createConstraintWithParams rejects it before Validate runs), so integer/number/boolean/array fields must be advertised unsatisfiable, not as satisfiable defaults — mirroring the malformed-max handling */
    for _, fieldName := range []string{"age", "score", "ok", "tags"} {
        property := schema.Properties[fieldName]
        if nil == property {
            t.Fatalf("expected the %q property to exist", fieldName)
        }
        if false == isUnsatisfiableSchema(property) {
            t.Fatalf("expected a malformed min on %q to advertise an unsatisfiable field, got %+v", fieldName, property)
        }
    }
}

type uncompilableRegexRequest struct {
    Code string `json:"code" validate:"regex(pattern=a{3,1})"`
}

func TestBuildSchema_UncompilableRegexIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(uncompilableRegexRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["uncompilableRegexRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    property := schema.Properties["code"]
    if nil == property {
        t.Fatalf("expected the code property to exist")
    }

    /* @important the validator accepts the empty string (and null for a pointer) even for an uncompilable pattern, rejecting only non-empty values; the spec must therefore advertise maxLength 0 (only the empty string) — not the uncompilable pattern verbatim, and not a fully unsatisfiable field that would also forbid the empty string */
    if "" != property.Pattern {
        t.Fatalf("expected an uncompilable pattern not to be advertised verbatim, got pattern %q", property.Pattern)
    }
    if nil == property.MaxLength || 0 != *property.MaxLength {
        t.Fatalf("expected an uncompilable regex to advertise maxLength 0, got %+v", property)
    }
    if nil != property.MinLength {
        t.Fatalf("expected no minLength floor (the empty string must stay valid), got %+v", property)
    }
}

type uncompilableRegexNullableRequest struct {
    Code *string `json:"code" validate:"regex(pattern=a{3,1})"`
}

func TestBuildSchema_UncompilableRegexOnPointerKeepsNullable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(uncompilableRegexNullableRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["uncompilableRegexNullableRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    property := schema.Properties["code"]
    if nil == property {
        t.Fatalf("expected the code property to exist")
    }

    /* @important the validator accepts null for a *string with an uncompilable regex (Regex.Validate returns nil for a nil pointer), so the nullable advertisement must be preserved while non-empty strings are excluded via maxLength 0 */
    if false == property.Nullable {
        t.Fatalf("expected a nullable pointer field with an uncompilable regex to stay nullable, got %+v", property)
    }
    if nil == property.MaxLength || 0 != *property.MaxLength {
        t.Fatalf("expected maxLength 0, got %+v", property)
    }
}

type negativeMaxNullableRequest struct {
    Count *int     `json:"count" validate:"max=-1"`
    Ratio *float64 `json:"ratio" validate:"max=-3"`
    Flag  *bool    `json:"flag" validate:"max=-2"`
}

func TestBuildSchema_NegativeMaxOnNullableKeepsNullable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(negativeMaxNullableRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["negativeMaxNullableRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important a negative max parses fine, so MaxLength.Validate runs and accepts a nil pointer (dereferenceValue ok=false): the validator rejects only non-null values, so a nullable field must keep Nullable=true (emptyValueSpace), not be marked fully unsatisfiable */
    for _, fieldName := range []string{"count", "ratio", "flag"} {
        property := schema.Properties[fieldName]
        if nil == property {
            t.Fatalf("expected the %q property to exist", fieldName)
        }
        if false == property.Nullable {
            t.Fatalf("expected nullable field %q with a negative max to keep Nullable=true, got %+v", fieldName, property)
        }
    }
}

func isUnsatisfiableSchema(schema *Schema) bool {
    if "string" == schema.Type {
        return nil != schema.MinLength && nil != schema.MaxLength && 1 == *schema.MinLength && 0 == *schema.MaxLength
    }
    if "integer" == schema.Type || "number" == schema.Type {
        return nil != schema.Minimum && nil != schema.Maximum && 0 == *schema.Minimum && 0 == *schema.Maximum && nil != schema.ExclusiveMinimum && nil != schema.ExclusiveMaximum
    }
    if "array" == schema.Type {
        return nil != schema.MinItems && nil != schema.MaxItems && 1 == *schema.MinItems && 0 == *schema.MaxItems
    }
    if "boolean" == schema.Type {
        return 2 == len(schema.AllOf)
    }
    return false
}

type uncompilableRegexThenMaxRequest struct {
    Code string `json:"code" validate:"regex(pattern=a{3,1}),max=5"`
}

func TestBuildSchema_UncompilableRegexNotLoosenedByLaterMax(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(uncompilableRegexThenMaxRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["uncompilableRegexThenMaxRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    property := schema.Properties["code"]
    if nil == property {
        t.Fatalf("expected the code property to exist")
    }

    /* @important the validator enforces both rules: the uncompilable regex rejects every non-empty value (only "" satisfies), so a later max=5 must not raise the advertised ceiling back to 5 and resurrect 1..5-char values the regex rejects — the tighter maxLength 0 must win */
    if nil == property.MaxLength || 0 != *property.MaxLength {
        t.Fatalf("expected maxLength 0 to survive a later max=5 (tighter bound wins), got %+v", property)
    }
}

type uncompilableRegexThenValuelessMaxRequest struct {
    Code string `json:"code" validate:"regex(pattern=a{3,1}),max"`
}

func TestBuildSchema_UncompilableRegexNotLoosenedByValuelessMax(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(uncompilableRegexThenValuelessMaxRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["uncompilableRegexThenValuelessMaxRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    property := schema.Properties["code"]
    if nil == property {
        t.Fatalf("expected the code property to exist")
    }

    /* @important a value-less max (default maxLength 100) must not raise the maxLength 0 an uncompilable regex set: the validator enforces both, so the tighter ceiling wins */
    if nil == property.MaxLength || 0 != *property.MaxLength {
        t.Fatalf("expected maxLength 0 to survive a later value-less max, got %+v", property)
    }
}

/* @info The generator's bracket-balance helper is documented as an exact mirror of the validator's, so a tag the validator accepts must never be swept up by the syntax guard. RE2 reads a ']' that closes no character class as a literal, and the validator handles this; a generator still rejecting it advertises an unsatisfiable field for values the api happily accepts. */
func TestApplyValidation_LiteralClosingBracketMatchesTheValidator(t *testing.T) {
    schema := &Schema{Type: "string"}
    applyValidation(schema, "regex(pattern=^a]b$)", nil)

    if nil != schema.MinLength && nil != schema.MaxLength && 1 == *schema.MinLength && 0 == *schema.MaxLength {
        t.Fatalf("the generator declared unsatisfiable a field the validator accepts")
    }
    if "^a]b$" != schema.Pattern {
        t.Fatalf("expected the regex to reach the schema pattern, got %q", schema.Pattern)
    }
}

/* @info The runtime validator registers regex only under the name "regex" — there is no "pattern" constraint, and an unknown rule fails the field closed for every value. Advertising `pattern=...` as a satisfiable regex-constrained string published a contract the api rejects outright. */
func TestApplyValidation_PatternIsNotARegexAlias(t *testing.T) {
    schema := &Schema{Type: "string"}
    applyValidation(schema, "pattern=^[0-9]+$", nil)

    if "^[0-9]+$" == schema.Pattern {
        t.Fatalf("the generator invented a regex constraint the validator does not have")
    }
}

/* @info a []byte renders as {string, byte}, but the validator's string facets never see a string there — Regex/alpha/numeric/alphanumeric ignore a []byte and accept every blob, and min/max measure fmt.Sprintf("%v", value) (e.g. "[1 2 3]") not the base64 text — so the mirror must not attach pattern/minLength/maxLength to a byte-format field, exactly as the email branch already declines format byte. Without the guard regex=^[a-z]+$ leaks a pattern, regex=a** advertises maxLength 0 (only the empty payload) and min/max/alpha over-constrain a blob the server actually accepts. */
func TestApplyValidation_StringFacetsSkipByteSliceField(t *testing.T) {
    byteType := reflect.TypeOf([]byte(nil))
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Pattern", Type: byteType, Tag: `json:"pattern" validate:"regex=^[a-z]+$"`},
        {Name: "Broken", Type: byteType, Tag: `json:"broken" validate:"regex=a**"`},
        {Name: "Alpha", Type: byteType, Tag: `json:"alpha" validate:"alpha"`},
        {Name: "Numeric", Type: byteType, Tag: `json:"numeric" validate:"numeric"`},
        {Name: "Alphanumeric", Type: byteType, Tag: `json:"alphanumeric" validate:"alphanumeric"`},
        {Name: "Floor", Type: byteType, Tag: `json:"floor" validate:"min=4"`},
        {Name: "Ceiling", Type: byteType, Tag: `json:"ceiling" validate:"max=8"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    for name := range schema.Properties {
        property := schema.Properties[name]
        if "string" != property.Type || "byte" != property.Format {
            t.Fatalf("expected the []byte field %q to stay {string, byte}, got {%q, %q}", name, property.Type, property.Format)
        }
        if "" != property.Pattern || nil != property.AllOf {
            t.Fatalf("expected no pattern facet on the []byte field %q, got pattern=%q allOf=%v", name, property.Pattern, property.AllOf)
        }
        if nil != property.MinLength || nil != property.MaxLength {
            t.Fatalf("expected no length facet on the []byte field %q, got minLength=%v maxLength=%v", name, property.MinLength, property.MaxLength)
        }
    }
}

/* @info parseLeadingInt must return the platform int, the same width the validator's parseIntStrict uses, so a bound in the 2^31..2^63-1 range that overflows a 32-bit int is rejected identically on both sides instead of parsing into a wider int64 and wrapping to a satisfiable facet the validator refuses. The 32-bit overflow itself is unobservable on a 64-bit test host, so this asserts the concrete return kind, which pins the width divergence on every platform. */
func TestParseLeadingIntReturnsPlatformIntWidth(t *testing.T) {
    parsed, ok := parseLeadingInt("5")
    if false == ok {
        t.Fatalf("expected parseLeadingInt to accept a plain integer")
    }
    if reflect.Int != reflect.TypeOf(parsed).Kind() {
        t.Fatalf("parseLeadingInt must return the platform int (matching the validator's parseIntStrict) so 32-bit overflow behaviour is identical, got %T", parsed)
    }
}

/* @info validateInternal checks every tagged field even when the property is omitted, so a constraint the Go zero value fails (min>=1 on a string, a non-pointer greaterThan bound>=0, a non-pointer lessThan bound<=0) turns an absent property into a 400; the spec must list such a field required — as notBlank/notEmpty already do — while a min of 0, a negative greaterThan bound, or a positive lessThan bound admits the zero value and keeps the field optional. */
func TestBuildSchema_ZeroValueRejectingConstraintsAreRequired(t *testing.T) {
    stringType := reflect.TypeOf("")
    intType := reflect.TypeOf(0)
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Name", Type: stringType, Tag: `json:"name" validate:"min=3"`},
        {Name: "Slug", Type: stringType, Tag: `json:"slug" validate:"min"`},
        {Name: "Count", Type: intType, Tag: `json:"count" validate:"greaterThan=5"`},
        {Name: "Balance", Type: intType, Tag: `json:"balance" validate:"greaterThan"`},
        {Name: "Depth", Type: intType, Tag: `json:"depth" validate:"lessThan"`},
        {Name: "Optional", Type: stringType, Tag: `json:"optional" validate:"min=0"`},
        {Name: "Below", Type: intType, Tag: `json:"below" validate:"greaterThan=-5"`},
        {Name: "Above", Type: intType, Tag: `json:"above" validate:"lessThan=5"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    for _, name := range []string{"name", "slug", "count", "balance", "depth"} {
        if false == containsString(schema.Required, name) {
            t.Fatalf("expected %q to be required (the validator rejects the omitted zero value), required=%v", name, schema.Required)
        }
    }

    for _, name := range []string{"optional", "below", "above"} {
        if true == containsString(schema.Required, name) {
            t.Fatalf("expected %q to stay optional (the zero value satisfies the bound), required=%v", name, schema.Required)
        }
    }
}

/* @info a pointer string field has no zero value the length constraint would measure: an omitted property leaves it nil, the validator's dereference reports absence and accepts the payload, so advertising the field required would force clients to send what the server does not demand. */
func TestBuildSchema_PointerMinFieldStaysOptional(t *testing.T) {
    pointerStringType := reflect.TypeOf((*string)(nil))
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Nick", Type: pointerStringType, Tag: `json:"nick" validate:"min=1"`},
        {Name: "Alias", Type: pointerStringType, Tag: `json:"alias" validate:"min"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    for _, name := range []string{"nick", "alias"} {
        if true == containsString(schema.Required, name) {
            t.Fatalf("expected the pointer field %q to stay optional (the validator accepts its absence), required=%v", name, schema.Required)
        }
    }
}

/* @info a POSIX named class ([:alpha:]) nested in a bracket expression has its own closing ']' that the character-class scanner must not read as the end of the whole class; otherwise regex=[[:alpha:],] is split at the in-class comma into an uncompilable pattern plus a stray ']' rule. The validator rejects such a tag for every value including "", so the mirror must keep the class intact and advertise the compilable pattern rather than a maxLength-0 (empty-string-satisfiable) field. This keeps the local scanner in lockstep with validation/validation_rule.go. */
func TestApplyValidation_PosixClassKeepsTheClassIntact(t *testing.T) {
    schema := &Schema{Type: "string"}
    applyValidation(schema, `regex=[[:alpha:],]`, nil)

    if "[[:alpha:],]" != schema.Pattern {
        t.Fatalf("expected the POSIX-class regex to reach the schema pattern intact, got pattern=%q maxLength=%v allOf=%v", schema.Pattern, schema.MaxLength, schema.AllOf)
    }
}

/* @info RE2 recognises only [: :] as a POSIX named class; a [. .] collating element and a [= =] equivalence element are NOT supported, so RE2 (and regexp.Compile) read the inner '[' as an ordinary class literal. The character-class scanner must do the same: treating [. or [= as a POSIX opener makes it hunt for a ".]"/"=]" delimiter that never arrives, so the class over-extends to the end of the tag, hasBalancedRuleBrackets reports the tag unbalanced, and splitTopLevelRules splits regex=[a,b[.c] at the in-class comma into "regex=[a" (an uncompilable class advertised as maxLength 0, empty-string-only) plus a stray "b[.c]" rule — a contract the validator, which compiles the class whole and rejects every value including "", never honours. The plain and equivalence fields lock the [. and [= literal handling; the posix field pairs a real [:alpha:] class with a following in-class comma and a [. literal so the whole tag survives as one satisfiable pattern rather than closing early at the ':]' — matching validation/validation_rule.go. */
func TestBuildSchema_CollatingEquivalenceOpenersStayLiteralInsideClass(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    stringType := reflect.TypeOf("")
    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Plain", Type: stringType, Tag: `json:"plain" validate:"regex=[a,b[.c]"`},
        {Name: "Equiv", Type: stringType, Tag: `json:"equiv" validate:"regex=[a,b[=c]"`},
        {Name: "Posix", Type: stringType, Tag: `json:"posix" validate:"regex=[[:alpha:],b[.c]"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    cases := []struct {
        jsonName string
        pattern  string
    }{
        {"plain", "[a,b[.c]"},
        {"equiv", "[a,b[=c]"},
        {"posix", "[[:alpha:],b[.c]"},
    }

    for _, testCase := range cases {
        property := schema.Properties[testCase.jsonName]
        if nil == property {
            t.Fatalf("expected the %q property to exist", testCase.jsonName)
        }
        if testCase.pattern != property.Pattern {
            t.Fatalf("expected %q to be mirrored as one regex rule with the full pattern %q (an in-class '[' is an RE2 literal, not a collating/equivalence opener), got pattern=%q allOf=%v maxLength=%v", testCase.jsonName, testCase.pattern, property.Pattern, property.AllOf, property.MaxLength)
        }
        if nil != property.AllOf {
            t.Fatalf("expected %q to stay a single pattern rule, not split into allOf members, got allOf=%v", testCase.jsonName, property.AllOf)
        }
        if nil != property.MaxLength {
            t.Fatalf("expected %q to advertise a satisfiable pattern (no maxLength 0 empty-string-only ceiling), got maxLength=%v", testCase.jsonName, *property.MaxLength)
        }
    }
}

type emailFacetRequest struct {
    Email  string `json:"email" validate:"email,max=64"`
    Domain string `json:"domain" validate:"email,regex=@corp[.]com$"`
    Name   string `json:"name" validate:"min=3,email"`
}

/* @info the email format is an annotation, not a structural format: the validator still enforces every later string facet on the same raw string, so the mirror must not let the annotation swallow a max/regex/min that follows it in tag order */
func TestBuildSchema_EmailAnnotationKeepsLaterStringFacets(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(emailFacetRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["emailFacetRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    email := schema.Properties["email"]
    if "email" != email.Format {
        t.Fatalf("expected the email format annotation, got %q", email.Format)
    }
    if nil == email.MaxLength || 64 != *email.MaxLength {
        t.Fatalf("expected the max after email to be mirrored as maxLength 64, got %v", email.MaxLength)
    }

    domain := schema.Properties["domain"]
    if "@corp[.]com$" != domain.Pattern {
        t.Fatalf("expected the regex after email to be mirrored, got %q", domain.Pattern)
    }

    name := schema.Properties["name"]
    if nil == name.MinLength || 3 != *name.MinLength {
        t.Fatalf("expected the min before email to be mirrored as minLength 3, got %v", name.MinLength)
    }

    nameRequired := false
    for _, requiredName := range schema.Required {
        if "name" == requiredName {
            nameRequired = true
        }
    }
    if false == nameRequired {
        t.Fatalf("expected min=3 to keep the field required despite the email annotation (the absent zero value \"\" fails the floor), got required=%v", schema.Required)
    }
}

type notBlankKindsRequest struct {
    Name  string    `json:"name" validate:"notBlank"`
    Score *int      `json:"score" validate:"notBlank"`
    Data  any       `json:"data" validate:"notBlank"`
    Age   int       `json:"age" validate:"notBlank"`
    Tags  []string  `json:"tags" validate:"notBlank"`
    Blob  []byte    `json:"blob" validate:"notBlank"`
    When  time.Time `json:"when" validate:"notBlank"`
}

/* @info notBlank turns absence into a 400 only for a pointer or interface (nil dereferences to nothing) or a genuine string (the zero "" is blank); the other zero values stringify non-blank ("0", "[]", the zero time) and pass, so listing them required would forbid an omission the validator accepts */
func TestBuildSchema_NotBlankRequiredOnlyWhereAbsenceIsRejected(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(notBlankKindsRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["notBlankKindsRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    expected := map[string]bool{"name": true, "score": true, "data": true}
    if len(expected) != len(schema.Required) {
        t.Fatalf("expected exactly the string, pointer and interface fields required, got %v", schema.Required)
    }
    for _, requiredName := range schema.Required {
        if false == expected[requiredName] {
            t.Fatalf("unexpected required field %q in %v", requiredName, schema.Required)
        }
    }

    blob := schema.Properties["blob"]
    if nil != blob.MinLength {
        t.Fatalf("expected notBlank to stay off the byte-format string (the validator accepts an empty blob), got minLength %v", *blob.MinLength)
    }

    name := schema.Properties["name"]
    if nil == name.MinLength || 1 != *name.MinLength {
        t.Fatalf("expected the genuine string to carry the minLength 1 floor, got %v", name.MinLength)
    }
}

type interfaceRejectAllRequest struct {
    Data any `json:"data" validate:"min=abc"`
}

/* @info a malformed bound rejects every value of every kind, but an interface field builds an empty schema no facet can contradict; not against the empty schema is the one advertisement no value satisfies */
func TestBuildSchema_MalformedBoundOnInterfaceFieldAdvertisedUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(interfaceRejectAllRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["interfaceRejectAllRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    data := schema.Properties["data"]
    if nil == data.Not {
        t.Fatalf("expected the interface field with a reject-all tag to be contradicted with not, got %+v", data)
    }
}

type EmbedAudit struct {
    By string `json:"by"`
}

type embedRejectAllRequest struct {
    EmbedAudit `validate:"notEmpty"`
    Title      string `json:"title"`
}

type embedNotBlankRequest struct {
    *EmbedAudit `validate:"notBlank"`
    Title       string `json:"title"`
}

/* @info the validator runs a promoted embed's own tag against the embed value, so notEmpty on an embed rejects every payload of the enclosing object; the parent schema must advertise that, while notBlank on a pointer embed stays satisfiable and must not be contradicted */
func TestBuildSchema_EmbedRejectAllTagContradictsTheParentSchema(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(embedRejectAllRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["embedRejectAllRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    if nil == schema.MinProperties || 1 != *schema.MinProperties || nil == schema.MaxProperties || 0 != *schema.MaxProperties {
        t.Fatalf("expected the parent object contradicted (minProperties 1, maxProperties 0), got %+v", schema)
    }

    if nil == schema.Properties["title"] || nil == schema.Properties["by"] {
        t.Fatalf("expected the documented properties to be preserved alongside the contradiction, got %v", schema.Properties)
    }

    satisfiableComponents := map[string]*Schema{}
    buildSchema(reflect.TypeOf(embedNotBlankRequest{}), satisfiableComponents, map[reflect.Type]string{}, map[reflect.Type]bool{})

    satisfiable := satisfiableComponents["embedNotBlankRequest"]
    if nil != satisfiable.MinProperties || nil != satisfiable.MaxProperties {
        t.Fatalf("expected notBlank on a pointer embed to leave the parent satisfiable, got %+v", satisfiable)
    }
}

type quotedScalarRequest struct {
    Count   int     `json:"count,string"`
    Ratio   float64 `json:"ratio,string"`
    Active  bool    `json:"active,string"`
    Level   *int    `json:"level,string"`
    Doubled **int   `json:"doubled,string"`
    Name    string  `json:"name,string"`
    Plain   int     `json:"plain"`
    Window  int     `json:"window,string" validate:"greaterThan=5,lessThan=6"`
}

/* @info encoding/json's ",string" option makes the decoder demand the quoted textual form and refuse the bare scalar, so the spec must advertise the string the payload actually spells */
func TestBuildSchema_JsonStringOptionAdvertisedAsQuotedString(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(quotedScalarRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["quotedScalarRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    count := schema.Properties["count"]
    if "string" != count.Type || "^(-?[0-9]+|null)$" != count.Pattern {
        t.Fatalf("expected the quoted integer advertisement (the literal \"null\" included), got %+v", count)
    }

    /* ParseFloat accepts more than the JSON number grammar (hex floats, a trailing dot), so the quoted float advertises no pattern */
    ratio := schema.Properties["ratio"]
    if "string" != ratio.Type || "" != ratio.Pattern {
        t.Fatalf("expected the quoted number advertised without a pattern, got %+v", ratio)
    }

    active := schema.Properties["active"]
    if "string" != active.Type || nil == active.Enum || 3 != len(*active.Enum) || "null" != (*active.Enum)[2] {
        t.Fatalf("expected the quoted boolean advertised as the two literals plus \"null\", got %+v", active)
    }

    level := schema.Properties["level"]
    if "string" != level.Type || false == level.Nullable {
        t.Fatalf("expected the quoted pointer scalar to stay nullable, got %+v", level)
    }

    doubled := schema.Properties["doubled"]
    if "integer" != doubled.Type {
        t.Fatalf("expected the double pointer to keep the option ignored (encoding/json dereferences one unnamed pointer only), got %+v", doubled)
    }

    name := schema.Properties["name"]
    if "string" != name.Type || "" != name.Pattern {
        t.Fatalf("expected the string field left alone, got %+v", name)
    }

    plain := schema.Properties["plain"]
    if "integer" != plain.Type {
        t.Fatalf("expected the option-less integer untouched, got %+v", plain)
    }

    /* the open interval (5, 6) holds no whole number, so the quoted integer must stay unsatisfiable */
    window := schema.Properties["window"]
    if "string" != window.Type || nil == window.MinLength || 1 != *window.MinLength || nil == window.MaxLength || 0 != *window.MaxLength {
        t.Fatalf("expected the unsatisfiable scalar to stay unsatisfiable in its quoted form, got %+v", window)
    }
}

type dynamicValueRequest struct {
    Amount any `json:"amount" validate:"greaterThan=0"`
    Bag    any `json:"bag" validate:"notEmpty"`
}

/* @info the validator judges an interface field's DECODED dynamic value — a positive number passes greaterThan and a non-empty string passes notEmpty — so the kind-classified reject-all arms must not contradict it */
func TestBuildSchema_DynamicRulesOnInterfaceFieldsStaySatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(dynamicValueRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["dynamicValueRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    amount := schema.Properties["amount"]
    if nil != amount.Not || nil != amount.Minimum || nil != amount.MinProperties {
        t.Fatalf("expected greaterThan on an interface field to stay unconstrained, got %+v", amount)
    }

    bag := schema.Properties["bag"]
    if nil != bag.Not || nil != bag.MinProperties {
        t.Fatalf("expected notEmpty on an interface field to stay unconstrained, got %+v", bag)
    }
}

type interfaceMalformedComparisonRequest struct {
    Amount  any `json:"amount" validate:"greaterThan=abc"`
    Ceiling any `json:"ceiling" validate:"lessThan=xyz"`
    Valid   any `json:"valid" validate:"greaterThan=5"`
}

/* @info GreaterThan/LessThan.WithParams run the same parseIntStrict as min/max, so a malformed bound fails constraint creation and rejects every payload before any value is examined; on an interface field the kind-classified branches are exempt, leaving not as the only truthful advertisement — a VALID bound stays satisfiable, judged on the decoded dynamic value */
func TestBuildSchema_MalformedComparisonBoundOnInterfaceFieldAdvertisedUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(interfaceMalformedComparisonRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["interfaceMalformedComparisonRequest"]
    if nil == schema {
        t.Fatalf("expected the request component to be built")
    }

    if nil == schema.Properties["amount"].Not {
        t.Fatalf("expected the malformed greaterThan bound to be contradicted with not, got %+v", schema.Properties["amount"])
    }

    if nil == schema.Properties["ceiling"].Not {
        t.Fatalf("expected the malformed lessThan bound to be contradicted with not, got %+v", schema.Properties["ceiling"])
    }

    if nil != schema.Properties["valid"].Not {
        t.Fatalf("expected the valid bound to stay satisfiable, got %+v", schema.Properties["valid"])
    }
}

type embedValueMaxOneRequest struct {
    EmbedAudit `validate:"max=1"`
    Title      string `json:"title"`
}

type embedValueMaxNegativeRequest struct {
    EmbedAudit `validate:"max=-1"`
    Title      string `json:"title"`
}

type embedValueMaxTwoRequest struct {
    EmbedAudit `validate:"max=2"`
    Title      string `json:"title"`
}

type embedPointerMaxOneRequest struct {
    *EmbedAudit `validate:"max=1"`
    Title       string `json:"title"`
}

type EmbedStringable struct {
    By string `json:"by"`
}

func (instance EmbedStringable) String() string {
    return ""
}

type embedStringerMaxOneRequest struct {
    EmbedStringable `validate:"max=1"`
    Title           string `json:"title"`
}

type EmbedPointerStringable struct {
    By string `json:"by"`
}

func (instance *EmbedPointerStringable) String() string {
    return ""
}

type embedPointerReceiverStringerMaxOneRequest struct {
    EmbedPointerStringable `validate:"max=1"`
    Title                  string `json:"title"`
}

/* @info MaxLength stringifies a value embed with %v and the default struct rendering never drops its braces ("{}" is the floor), so a parseable max below 2 rejects every payload; a pointer embed passes through the nil escape, a value-method Stringer delegates %v and voids the floor, and a pointer-receiver String() stays out of the value's method set so the floor holds */
func TestBuildSchema_ValueEmbedMaxBelowTheBraceFloorContradictsTheParentSchema(t *testing.T) {
    for _, testCase := range []struct {
        requestType   reflect.Type
        componentName string
    }{
        {reflect.TypeOf(embedValueMaxOneRequest{}), "embedValueMaxOneRequest"},
        {reflect.TypeOf(embedValueMaxNegativeRequest{}), "embedValueMaxNegativeRequest"},
        {reflect.TypeOf(embedPointerReceiverStringerMaxOneRequest{}), "embedPointerReceiverStringerMaxOneRequest"},
    } {
        components := map[string]*Schema{}
        buildSchema(testCase.requestType, components, map[reflect.Type]string{}, map[reflect.Type]bool{})

        schema := components[testCase.componentName]
        if nil == schema.MinProperties || 1 != *schema.MinProperties || nil == schema.MaxProperties || 0 != *schema.MaxProperties {
            t.Fatalf("expected %s contradicted (minProperties 1, maxProperties 0), got %+v", testCase.componentName, schema)
        }
    }

    for _, testCase := range []struct {
        requestType   reflect.Type
        componentName string
    }{
        {reflect.TypeOf(embedValueMaxTwoRequest{}), "embedValueMaxTwoRequest"},
        {reflect.TypeOf(embedPointerMaxOneRequest{}), "embedPointerMaxOneRequest"},
        {reflect.TypeOf(embedStringerMaxOneRequest{}), "embedStringerMaxOneRequest"},
    } {
        components := map[string]*Schema{}
        buildSchema(testCase.requestType, components, map[reflect.Type]string{}, map[reflect.Type]bool{})

        schema := components[testCase.componentName]
        if nil != schema.MinProperties || nil != schema.MaxProperties {
            t.Fatalf("expected %s to stay satisfiable, got %+v", testCase.componentName, schema)
        }
    }
}

type quotedUnsignedRequest struct {
    Slots uint `json:"slots,string"`
}

/* @info literalStore consumes a quoted "null" on every ,string kind and leaves the zero value, so the advertised pattern must admit the spelling the decoder accepts — and nothing else beyond the digits */
func TestBuildSchema_JsonStringOptionAdvertisesTheNullLiteral(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(quotedUnsignedRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    slots := components["quotedUnsignedRequest"].Properties["slots"]
    if "string" != slots.Type || "^([0-9]+|null)$" != slots.Pattern {
        t.Fatalf("expected the quoted unsigned advertisement (the literal \"null\" included, no minus), got %+v", slots)
    }
}

type duplicateBoundsRequest struct {
    Amount int `json:"amount" validate:"greaterThan=5,greaterThan=3,lessThan=10,lessThan=20"`
}

/* @info the validator enforces every duplicate rule, so the effective window is the intersection; the mirror must keep the tightest bound of each side instead of letting the last rule overwrite it */
func TestBuildSchema_DuplicateComparisonBoundsKeepTheTightestWindow(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(duplicateBoundsRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    amount := components["duplicateBoundsRequest"].Properties["amount"]
    if nil == amount.Minimum || 5 != *amount.Minimum {
        t.Fatalf("expected the higher greaterThan bound to win, got %+v", amount.Minimum)
    }

    if nil == amount.Maximum || 10 != *amount.Maximum {
        t.Fatalf("expected the lower lessThan bound to win, got %+v", amount.Maximum)
    }
}

type quotedNullRejectingRequest struct {
    Level  *int     `json:"level,string" validate:"greaterThan=0"`
    Flag   *bool    `json:"flag,string" validate:"notBlank"`
    Floor  int      `json:"floor,string" validate:"greaterThan=0"`
    Loose  int      `json:"loose,string" validate:"greaterThan=-5"`
    Silent *int     `json:"silent,string" validate:"max=0"`
    Ratio  *float64 `json:"ratio,string" validate:"notBlank"`
}

/* @info the quoted "null" decodes to null on a pointer and to the zero value on a bare scalar, so the advertisement must follow the validator: a null-rejecting rule withdraws the spelling (pattern, enum, or not on the pattern-less float), a window admitting zero keeps it, and a nullable empty value space accepts exactly the null literal */
func TestBuildSchema_JsonStringOptionWithdrawsTheRejectedNullSpelling(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(quotedNullRejectingRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["quotedNullRejectingRequest"]

    level := schema.Properties["level"]
    if "^-?[0-9]+$" != level.Pattern || true == level.Nullable {
        t.Fatalf("expected the null-rejecting quoted pointer integer advertised without the null literal, got %+v", level)
    }

    flag := schema.Properties["flag"]
    if nil == flag.Enum || 2 != len(*flag.Enum) {
        t.Fatalf("expected the null-rejecting quoted pointer boolean advertised as the two literals alone, got %+v", flag)
    }

    floor := schema.Properties["floor"]
    if "^-?[0-9]+$" != floor.Pattern {
        t.Fatalf("expected the zero-excluding window to withdraw the null spelling on the bare scalar, got %+v", floor)
    }

    loose := schema.Properties["loose"]
    if "^(-?[0-9]+|null)$" != loose.Pattern {
        t.Fatalf("expected the zero-admitting window to keep the null spelling on the bare scalar, got %+v", loose)
    }

    silent := schema.Properties["silent"]
    if nil == silent.Enum || 1 != len(*silent.Enum) || "null" != (*silent.Enum)[0] || false == silent.Nullable {
        t.Fatalf("expected the nullable empty value space to accept exactly the null spellings, got %+v", silent)
    }

    ratio := schema.Properties["ratio"]
    if nil == ratio.Not || nil == ratio.Not.Enum || "null" != (*ratio.Not.Enum)[0] {
        t.Fatalf("expected the pattern-less quoted float to exclude the withdrawn null through not, got %+v", ratio)
    }
}

type quotedUnsignedEmptyWindowRequest struct {
    Slots uint `json:"slots,string" validate:"lessThan=-1"`
    Turns uint `json:"turns,string" validate:"lessThan"`
}

/* @info an unsigned target under a negative ceiling — or the exclusive zero ceiling of a value-less lessThan — accepts nothing, and the kind-blind window check cannot see it: the quoted form must stay unsatisfiable instead of advertising a digits pattern every payload of which the validator rejects */
func TestBuildSchema_JsonStringOptionKeepsTheEmptyUnsignedWindowUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(quotedUnsignedEmptyWindowRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    for _, propertyName := range []string{"slots", "turns"} {
        property := components["quotedUnsignedEmptyWindowRequest"].Properties[propertyName]
        if "" != property.Pattern || nil == property.MinLength || 1 != *property.MinLength || nil == property.MaxLength || 0 != *property.MaxLength {
            t.Fatalf("expected the empty unsigned window of %q to stay unsatisfiable in its quoted form, got %+v", propertyName, property)
        }
    }
}

/* @info the validator measures a length fixed at zero, so notEmpty rejects every payload; advertising minItems 1 alone told a client a non-empty array would be accepted */
func TestBuildSchema_NotEmptyOnZeroLengthArrayIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    requestType := reflect.StructOf([]reflect.StructField{
        {Name: "Tags", Type: reflect.TypeOf([0]string{}), Tag: `json:"tags" validate:"notEmpty"`},
    })

    schema := buildSchema(requestType, components, names, visited)

    property, exists := schema.Properties["tags"]
    if false == exists {
        t.Fatalf("expected the property to be emitted")
    }

    if nil == property.MinItems || 1 != *property.MinItems {
        t.Fatalf("expected the floor to be kept, got %v", property.MinItems)
    }

    if nil == property.MaxItems || 0 != *property.MaxItems {
        t.Fatalf("expected the empty value space to be advertised, got %v", property.MaxItems)
    }
}

func TestSchemaFromType_TopLevelUnsignedCarriesZeroFloor(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}

    schema := schemaFromType(reflect.TypeOf(uint64(0)), components, names)

    if "integer" != schema.Type {
        t.Fatalf("unexpected schema type: %s", schema.Type)
    }

    if nil == schema.Minimum || 0 != *schema.Minimum {
        t.Fatalf("expected a zero floor on a bare unsigned type, got %v", schema.Minimum)
    }
}

func TestSchemaFromType_NilTypeYieldsEmptySchema(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}

    schema := schemaFromType(nil, components, names)

    if nil == schema {
        t.Fatalf("expected a schema for a nil type")
    }

    if "" != schema.Type {
        t.Fatalf("unexpected schema type: %s", schema.Type)
    }
}

type marshalerEmbedPrice struct {
    Amount   int64  `json:"amount"`
    Currency string `json:"currency"`
}

func (instance marshalerEmbedPrice) MarshalJSON() ([]byte, error) {
    return []byte(`"USD 1.00"`), nil
}

type marshalerEmbedWeight struct {
    Grams int `json:"grams"`
}

func (instance marshalerEmbedWeight) MarshalJSON() ([]byte, error) {
    return []byte(`"1kg"`), nil
}

type marshalerEmbedLine struct {
    marshalerEmbedPrice
    Label string `json:"label"`
}

type marshalerEmbedAmbiguous struct {
    marshalerEmbedPrice
    marshalerEmbedWeight
}

type marshalerEmbedPointerReceiver struct {
    Grams int `json:"grams"`
}

func (instance *marshalerEmbedPointerReceiver) MarshalJSON() ([]byte, error) {
    return []byte(`"1kg"`), nil
}

type marshalerEmbedPointerHost struct {
    marshalerEmbedPointerReceiver
    Label string `json:"label"`
}

type marshalerEmbedPointerIndirectHost struct {
    *marshalerEmbedPointerReceiver
    Label string `json:"label"`
}

type timeEmbedEvent struct {
    time.Time
    Name string `json:"name"`
}

type timeEmbedHolder struct {
    time.Time
}

type timeEmbedIndirectEvent struct {
    timeEmbedHolder
    Name string `json:"name"`
}

type timeEmbedBesideAMarshaler struct {
    time.Time
    marshalerEmbedPrice
}

type marshalerEmbedInterfaceHost struct {
    json.Marshaler
    X int `json:"x"`
}

func TestBuildSchema_EmbeddedTimeFollowsThePromotedCodec(t *testing.T) {
    encoded, err := json.Marshal(
        timeEmbedEvent{
            Time: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
            Name: "launch",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"2026-07-25T12:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted MarshalJSON to own the wire form, got %s", encoded)
    }

    schema := buildSchema(reflect.TypeOf(timeEmbedEvent{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "string" != schema.Type || "date-time" != schema.Format {
        t.Fatalf("expected the embedded time codec to be advertised as a date-time string, got %+v", schema)
    }

    if 0 != len(schema.Properties) {
        t.Fatalf("expected no properties for a value encoded as a bare string, got %+v", schema.Properties)
    }
}

func TestBuildSchema_NestedEmbeddedTimeFollowsThePromotedCodec(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedIndirectEvent{Name: "launch"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the twice-promoted MarshalJSON to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(timeEmbedIndirectEvent{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "string" != schema.Type || "date-time" != schema.Format {
        t.Fatalf("expected a date-time string, got %+v", schema)
    }

    if 0 != len(components) {
        t.Fatalf("expected no component for a value encoded as a bare string, got %v", componentKeyList(components))
    }
}

/* a user marshaler embed writes bytes reflection cannot read, so the enclosing object is still advertised as the spread of its fields — the schema does not match the wire, and narrowing to time.Time keeps that gap where it already was instead of moving it onto the embed's own component */
func TestBuildSchema_UserMarshalerEmbedKeepsItsObjectSchema(t *testing.T) {
    encoded, err := json.Marshal(marshalerEmbedLine{Label: "seat"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"USD 1.00"` != string(encoded) {
        t.Fatalf("expected the promoted MarshalJSON to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(marshalerEmbedLine{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/marshalerEmbedLine" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its own component, got %+v", schema)
    }

    component, present := components["marshalerEmbedLine"]
    if false == present {
        t.Fatalf("expected a component for the enclosing type, got %v", componentKeyList(components))
    }

    for _, propertyName := range []string{"amount", "currency", "label"} {
        if _, exists := component.Properties[propertyName]; false == exists {
            t.Fatalf("expected the promoted property %q, got %+v", propertyName, component.Properties)
        }
    }
}

/* a pointer-receiver marshaler embed stays out of the enclosing VALUE's method set, so encoding/json spreads the fields for a value and calls the promoted method for a pointer; the object schema matches the value form */
func TestBuildSchema_PointerReceiverMarshalerEmbedMatchesTheValueWireForm(t *testing.T) {
    encoded, err := json.Marshal(marshalerEmbedPointerHost{Label: "seat"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"grams":0,"label":"seat"}` != string(encoded) {
        t.Fatalf("expected the value form to spread the embed's fields, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(marshalerEmbedPointerHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/marshalerEmbedPointerHost" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its own component, got %+v", schema)
    }

    component := components["marshalerEmbedPointerHost"]
    for _, propertyName := range []string{"grams", "label"} {
        if _, exists := component.Properties[propertyName]; false == exists {
            t.Fatalf("expected the promoted property %q, got %+v", propertyName, component.Properties)
        }
    }
}

func TestPromotedMarshalerOrigin_APointerEmbedWithAPointerReceiverCodecIsUnresolved(t *testing.T) {
    hostType := reflect.TypeOf(marshalerEmbedPointerIndirectHost{})

    if false == hostType.Implements(jsonMarshalerInterfaceType) {
        t.Fatalf("expected the pointer embed to promote its codec onto the enclosing value")
    }

    if true == reflect.TypeOf(marshalerEmbedPointerReceiver{}).Implements(jsonMarshalerInterfaceType) {
        t.Fatalf("expected the dereferenced embed to keep its codec out of the value method set")
    }

    origin, depth, resolved := promotedMarshalerOrigin(hostType, map[reflect.Type]bool{})

    if true == resolved {
        t.Fatalf("expected an unresolvable promotion, got origin %v at depth %d", origin, depth)
    }

    if nil != origin || 0 != depth {
        t.Fatalf("expected no origin and a zero depth for an unresolved promotion, got %v at depth %d", origin, depth)
    }

    encoded, err := json.Marshal(marshalerEmbedPointerIndirectHost{
        marshalerEmbedPointerReceiver: &marshalerEmbedPointerReceiver{Grams: 1000},
        Label:                         "seat",
    })
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"1kg"` != string(encoded) {
        t.Fatalf("expected the promoted pointer-receiver codec to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(hostType, components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/marshalerEmbedPointerIndirectHost" != schema.Ref {
        t.Fatalf("expected the unresolved promotion to keep the object schema, got %+v", schema)
    }

    component := components["marshalerEmbedPointerIndirectHost"]
    for _, propertyName := range []string{"grams", "label"} {
        if _, exists := component.Properties[propertyName]; false == exists {
            t.Fatalf("expected the promoted property %q, got %+v", propertyName, component.Properties)
        }
    }
}

func TestBuildSchema_AmbiguousMarshalerEmbedsKeepPromotedProperties(t *testing.T) {
    encoded, err := json.Marshal(marshalerEmbedAmbiguous{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"amount":0,"currency":"","grams":0}` != string(encoded) {
        t.Fatalf("expected two equal-depth marshalers to promote nothing, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(marshalerEmbedAmbiguous{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    component, present := components["marshalerEmbedAmbiguous"]
    if false == present {
        t.Fatalf("expected an object component, got %+v", schema)
    }

    for _, propertyName := range []string{"amount", "currency", "grams"} {
        if _, exists := component.Properties[propertyName]; false == exists {
            t.Fatalf("expected the promoted property %q, got %+v", propertyName, component.Properties)
        }
    }
}

/* a time.Time embed beside another marshaler embed promotes no MarshalJSON — but time.Time's MarshalText is promoted unopposed, and encoding/json falls back to it, so the wire is a date-time string while the schema stays an object. Recognising this would mean modelling encoding/json's codec precedence, which is wider than the promoted-time case this file describes. */
func TestBuildSchema_TimeEmbedBesideAMarshalerKeepsItsObjectSchema(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedBesideAMarshaler{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted MarshalText to own the wire form, got %s", encoded)
    }

    if true == reflect.TypeOf(timeEmbedBesideAMarshaler{}).Implements(jsonMarshalerInterfaceType) {
        t.Fatalf("expected two equal-depth marshaler embeds to promote no MarshalJSON")
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(timeEmbedBesideAMarshaler{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/timeEmbedBesideAMarshaler" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its own component, got %+v", schema)
    }
}

/* an embedded interface promotes MarshalJSON too, but its dynamic value decides the wire form, so nothing about the static type describes it; the object schema keeps a property for the interface field itself */
func TestBuildSchema_EmbeddedMarshalerInterfaceKeepsItsObjectSchema(t *testing.T) {
    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(marshalerEmbedInterfaceHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/marshalerEmbedInterfaceHost" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its own component, got %+v", schema)
    }

    component := components["marshalerEmbedInterfaceHost"]
    if _, exists := component.Properties["x"]; false == exists {
        t.Fatalf("expected the sibling property, got %+v", component.Properties)
    }
}

type timeEmbedShadowingObjectCodec struct {
    Cents int `json:"cents"`
}

func (instance timeEmbedShadowingObjectCodec) MarshalJSON() ([]byte, error) {
    return []byte(`{"cents":0}`), nil
}

type timeEmbedShadowedByAnObjectCodec struct {
    timeEmbedShadowingObjectCodec
    timeEmbedHolder
}

type timeEmbedShadowingScalarCodec string

func (instance timeEmbedShadowingScalarCodec) MarshalJSON() ([]byte, error) {
    return []byte(`"1kg"`), nil
}

type timeEmbedShadowedByAScalarCodec struct {
    timeEmbedShadowingScalarCodec
    timeEmbedHolder
}

type timeEmbedAboveADeeperCodec struct {
    time.Time
    marshalerEmbedLine
}

/* the promoted codec is the shallowest one, so a depth-1 marshaler embed beats a time.Time reached at depth 2: the wire form is the object that embed writes, and advertising a date-time string would promise a string where a body is an object */
func TestBuildSchema_ShallowerMarshalerEmbedShadowsADeeperTimeCodec(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedShadowedByAnObjectCodec{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"cents":0}` != string(encoded) {
        t.Fatalf("expected the shallower MarshalJSON to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(timeEmbedShadowedByAnObjectCodec{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/timeEmbedShadowedByAnObjectCodec" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its object schema, got %+v", schema)
    }

    if "date-time" == schema.Format {
        t.Fatalf("expected no date-time advertisement for a body encoded as an object, got %+v", schema)
    }
}

/* a non-struct embed carries a codec of its own too, so it takes part in the promotion depth: skipping it would let a time.Time below it claim a promotion the string codec above already won */
func TestBuildSchema_NonStructMarshalerEmbedShadowsADeeperTimeCodec(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedShadowedByAScalarCodec{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"1kg"` != string(encoded) {
        t.Fatalf("expected the shallower MarshalJSON to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(timeEmbedShadowedByAScalarCodec{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "date-time" == schema.Format {
        t.Fatalf("expected no date-time advertisement for a body no time codec writes, got %+v", schema)
    }

    if "#/components/schemas/timeEmbedShadowedByAScalarCodec" != schema.Ref {
        t.Fatalf("expected the enclosing type to keep its object schema, got %+v", schema)
    }
}

/* the mirror of the two above: a time.Time embedded at depth 1 outranks a marshaler embed whose codec is declared at depth 2, and the date-time string is then the truthful advertisement */
func TestBuildSchema_TimeEmbedOutranksADeeperMarshalerEmbed(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedAboveADeeperCodec{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the shallower time codec to own the wire form, got %s", encoded)
    }

    schema := buildSchema(reflect.TypeOf(timeEmbedAboveADeeperCodec{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "string" != schema.Type || "date-time" != schema.Format {
        t.Fatalf("expected a date-time string, got %+v", schema)
    }
}

type timeEmbedWithADeclaredCodec struct {
    timeEmbedHolder
}

func (instance timeEmbedWithADeclaredCodec) MarshalJSON() ([]byte, error) {
    return []byte(`{"declared":true}`), nil
}

type timeEmbedPointerCycle struct {
    *timeEmbedPointerCycle
    time.Time
}

type timeEmbedNamedByATag struct {
    time.Time `json:"at"`
    Name      string `json:"name"`
}

/* a MarshalJSON declared on the enclosing type shadows the promoted one, but reflect.Method carries no declaring type: the promotion and the declaration are indistinguishable, so this shape keeps the date-time string while the wire form is whatever the declared codec writes */
func TestBuildSchema_ADeclaredCodecOverATimeEmbedIsNotDistinguishable(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedWithADeclaredCodec{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `{"declared":true}` != string(encoded) {
        t.Fatalf("expected the declared codec to own the wire form, got %s", encoded)
    }

    schema := buildSchema(reflect.TypeOf(timeEmbedWithADeclaredCodec{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "date-time" != schema.Format {
        t.Fatalf("expected the undetectable declaration to leave the date-time string, got %+v", schema)
    }
}

/* a codec reached through a cycle cannot be given a depth, so the promotion is left unresolved and the object schema stands even though the wire form is a date-time string: guessing the other way would advertise a string for every cyclic marshaler embed */
func TestBuildSchema_TimeCodecBehindAPointerCycleKeepsItsObjectSchema(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedPointerCycle{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted time codec to own the wire form, got %s", encoded)
    }

    components := map[string]*Schema{}
    schema := buildSchema(reflect.TypeOf(timeEmbedPointerCycle{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "#/components/schemas/timeEmbedPointerCycle" != schema.Ref {
        t.Fatalf("expected the unresolved promotion to keep the object schema, got %+v", schema)
    }
}

/* a json name on the embed does not stop the method promotion, so the body is still an RFC 3339 string and the name is never spelled */
func TestBuildSchema_JsonNamedTimeEmbedStillPromotesItsCodec(t *testing.T) {
    encoded, err := json.Marshal(timeEmbedNamedByATag{Name: "launch"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"0001-01-01T00:00:00Z"` != string(encoded) {
        t.Fatalf("expected the promoted codec to own the wire form, got %s", encoded)
    }

    schema := buildSchema(reflect.TypeOf(timeEmbedNamedByATag{}), map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "string" != schema.Type || "date-time" != schema.Format {
        t.Fatalf("expected a date-time string, got %+v", schema)
    }
}

var componentKeyGrammar = regexp.MustCompile(`^[a-zA-Z0-9.\-_]+$`)

type genericPage[T any] struct {
    Items []T `json:"items"`
    Total int `json:"total"`
}

type genericPageUser struct {
    Name string `json:"name"`
}

func componentKeyList(components map[string]*Schema) []string {
    keys := make([]string, 0, len(components))
    for key := range components {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    return keys
}

func assertResolvableComponentReference(t *testing.T, schema *Schema, components map[string]*Schema) {
    t.Helper()

    tokens := strings.Split(schema.Ref, "/")
    if 4 != len(tokens) || "#" != tokens[0] || "components" != tokens[1] || "schemas" != tokens[2] {
        t.Fatalf("expected a three-token json pointer into the components, got %q", schema.Ref)
    }

    if false == componentKeyGrammar.MatchString(tokens[3]) {
        t.Fatalf("expected a component key matching the openapi key grammar, got %q", tokens[3])
    }

    if _, present := components[tokens[3]]; false == present {
        t.Fatalf("expected %q to resolve in the components, got %v", tokens[3], componentKeyList(components))
    }
}

func TestSchemaComponentName_GenericInstantiatedWithASamePackageType(t *testing.T) {
    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(genericPage[genericPageUser]{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)
}

func TestSchemaComponentName_GenericInstantiatedWithACrossPackageType(t *testing.T) {
    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(genericPage[neturl.URL]{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)
}

func TestSchemaComponentName_DistinctGenericsThatSanitizeAlikeStayDisambiguated(t *testing.T) {
    components := map[string]*Schema{}
    names := map[reflect.Type]string{}
    visited := map[reflect.Type]bool{}

    first := buildSchema(reflect.TypeOf(genericPage[genericPageUser]{}), components, names, visited)
    second := buildSchema(reflect.TypeOf(genericPage[*genericPageUser]{}), components, names, visited)

    assertResolvableComponentReference(t, first, components)
    assertResolvableComponentReference(t, second, components)

    if first.Ref+"2" != second.Ref {
        t.Fatalf("expected the colliding instantiation to be disambiguated as %q, got %q", first.Ref+"2", second.Ref)
    }
}

type cyclicMetadata map[string]cyclicMetadata

type cyclicNodes []cyclicNodes

type cyclicNext *cyclicNext

type cyclicLeft []cyclicRight

type cyclicRight []cyclicLeft

type cyclicInterfaceElement interface {
    Children() cyclicInterfaceList
}

type cyclicInterfaceList []cyclicInterfaceElement

type cyclicStructNode struct {
    Value    string              `json:"value"`
    Children []*cyclicStructNode `json:"children"`
}

type cyclicShapeHost struct {
    Metadata cyclicMetadata      `json:"metadata"`
    Nodes    cyclicNodes         `json:"nodes"`
    Next     cyclicNext          `json:"next"`
    Left     cyclicLeft          `json:"left"`
    Dynamic  cyclicInterfaceList `json:"dynamic"`
    Node     *cyclicStructNode   `json:"node"`
}

const cyclicShapeChildMarker = "MELODY_OPENAPI_CYCLIC_SHAPE_CHILD"

/* A type cycle that never passes through a struct meets neither of the guards a struct cycle meets, and the recursion that follows it ends in a Go stack overflow — a fatal error, not a panic: recover cannot reach it and neither can the http recovery middleware, so the process dies. SpecHandler regenerates the document on every request, which makes the spec route a kill switch the first client trips. The reproduction therefore cannot live in this process: the child re-executes this test binary with the marker set, builds every shape and asserts what it got, and the parent asserts the child came back alive. The child carries its own timeout so the pointer-cycle shape, which live-locks rather than crashing, is reported instead of hanging the run. */
func TestBuildSchema_TypeCyclesTerminateWithoutKillingTheProcess(t *testing.T) {
    if "" != os.Getenv(cyclicShapeChildMarker) {
        assertCyclicShapesAreDescribed(t)

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestBuildSchema_TypeCyclesTerminateWithoutKillingTheProcess$",
        "-test.v",
        "-test.timeout=60s",
    )
    command.Env = append(os.Environ(), cyclicShapeChildMarker+"=1")

    output, runErr := command.CombinedOutput()
    if nil != runErr {
        t.Fatalf("schema generation for a cyclic type did not survive: %v\n%s", runErr, output)
    }
}

func assertCyclicShapesAreDescribed(t *testing.T) {
    t.Helper()

    assertNamedMapCycleIsDescribed(t)
    assertNamedSliceCycleIsDescribed(t)
    assertNamedPointerCycleIsDescribed(t)
    assertMutualCollectionCycleIsDescribed(t)
    assertInterfaceCycleIsDescribed(t)
    assertStructCycleIsDescribed(t)
    assertEveryCyclicShapeMarshalsAsOneDocument(t)
}

func assertNamedMapCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicMetadata{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)

    component := components[componentKeyOfReference(schema.Ref)]
    if "object" != component.Type || nil == component.AdditionalProperties {
        t.Fatalf("expected the named map component to describe an object with additional properties, got %+v", component)
    }

    if schema.Ref != component.AdditionalProperties.Ref {
        t.Fatalf("expected the map value to point back at %q, got %+v", schema.Ref, component.AdditionalProperties)
    }
}

func assertNamedSliceCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicNodes{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)

    component := components[componentKeyOfReference(schema.Ref)]
    if "array" != component.Type || nil == component.Items {
        t.Fatalf("expected the named slice component to describe an array with items, got %+v", component)
    }

    if schema.Ref != component.Items.Ref {
        t.Fatalf("expected the slice element to point back at %q, got %+v", schema.Ref, component.Items)
    }
}

func assertNamedPointerCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicNext(nil)), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if false == strings.Contains(schema.Description, "nesting depth") {
        t.Fatalf("expected a pointer type that dereferences to itself to be described as bounded, got %+v", schema)
    }

    if "" != schema.Type || "" != schema.Ref {
        t.Fatalf("expected the bounded position to carry no type and no reference, got %+v", schema)
    }

    if false == schema.Nullable {
        t.Fatalf("expected a pointer type to stay nullable, got %+v", schema)
    }
}

func assertMutualCollectionCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicLeft{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)

    left := components[componentKeyOfReference(schema.Ref)]
    if "array" != left.Type || nil == left.Items || "" == left.Items.Ref {
        t.Fatalf("expected the first component to reference the second, got %+v", left)
    }

    assertResolvableComponentReference(t, left.Items, components)

    right := components[componentKeyOfReference(left.Items.Ref)]
    if "array" != right.Type || nil == right.Items || schema.Ref != right.Items.Ref {
        t.Fatalf("expected the second component to reference %q, got %+v", schema.Ref, right)
    }
}

func assertInterfaceCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicInterfaceList{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    if "array" != schema.Type || nil == schema.Items {
        t.Fatalf("expected a list of interfaces to stay an inline array, got %+v", schema)
    }

    if "" != schema.Items.Type || "" != schema.Items.Ref {
        t.Fatalf("expected the interface element to stay undescribed, got %+v", schema.Items)
    }

    if 0 != len(components) {
        t.Fatalf("expected a cycle broken by an interface to need no component, got %v", componentKeyList(components))
    }
}

func assertStructCycleIsDescribed(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicStructNode{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)

    component := components[componentKeyOfReference(schema.Ref)]
    children := component.Properties["children"]
    if nil == children || "array" != children.Type || nil == children.Items {
        t.Fatalf("expected the recursive struct field to stay an inline array, got %+v", children)
    }

    if 1 != len(children.Items.AllOf) || schema.Ref != children.Items.AllOf[0].Ref {
        t.Fatalf("expected the child element to point back at %q, got %+v", schema.Ref, children.Items)
    }
}

func assertEveryCyclicShapeMarshalsAsOneDocument(t *testing.T) {
    t.Helper()

    components := map[string]*Schema{}

    schema := buildSchema(reflect.TypeOf(cyclicShapeHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    assertResolvableComponentReference(t, schema, components)

    document := &Document{
        OpenApi:    "3.0.3",
        Info:       Info{Title: "cyclic", Version: "1"},
        Paths:      map[string]PathItem{},
        Components: &Components{Schemas: components},
    }

    if _, marshalErr := json.Marshal(document); nil != marshalErr {
        t.Fatalf("expected a document carrying every cyclic shape to marshal, got: %v", marshalErr)
    }
}

func componentKeyOfReference(reference string) string {
    return strings.TrimPrefix(reference, "#/components/schemas/")
}

/* The bound is the guarantee that holds for a shape neither the component mechanism nor the element walk anticipated: reaching it must leave a well-formed, permissive position carrying a diagnostic, never a fatal stack overflow inside a request handler. Two hundred nested slices is deeper than the bound and shallower than the stack, so the walk must stop on its own well before the innermost element. */
func TestBuildSchema_DepthBoundDescribesThePositionItStopsAt(t *testing.T) {
    deepType := reflect.TypeOf("")
    for level := 0; level < 200; level++ {
        deepType = reflect.SliceOf(deepType)
    }

    schema := buildSchema(deepType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    levels := 0
    current := schema
    for nil != current.Items {
        current = current.Items
        levels++
    }

    if 200 <= levels {
        t.Fatalf("expected the walk to stop before the innermost element, followed %d levels", levels)
    }

    if false == strings.Contains(current.Description, "nesting depth") {
        t.Fatalf("expected the stopping position to carry a diagnostic, got %+v", current)
    }

    if "" != current.Type {
        t.Fatalf("expected the stopping position to accept any value, got %+v", current)
    }
}

type cyclicTaggedHost struct {
    Metadata cyclicMetadata    `json:"metadata" validate:"notEmpty"`
    Nodes    cyclicNodes       `json:"nodes" validate:"notEmpty"`
    Optional *cyclicNodes      `json:"optional" validate:"notEmpty"`
    Ranked   cyclicNodes       `json:"ranked" validate:"greaterThan"`
    Labelled cyclicNodes       `json:"labelled" validate:"notBlank"`
    Counted  cyclicNodes       `json:"counted" validate:"notEmpty(mode=hard)"`
    Node     *cyclicStructNode `json:"node" validate:"notEmpty"`
}

/* A named collection that reaches itself is described by a component and answered with a reference, because nothing else ends its recursion. The validator knows none of that: it is handed the decoded value and measures the length of a map or a slice like any other, so notEmpty on such a field rejects a null and a zero length and accepts everything else. Reading the reference as a struct would advertise a field no value satisfies while the server accepts entries, which is the divergence between the document and the enforcement this mirror exists to prevent. */
func TestBuildSchema_NotEmptyFollowsAReferenceToAPromotedCollection(t *testing.T) {
    components := map[string]*Schema{}

    buildSchema(reflect.TypeOf(cyclicTaggedHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["cyclicTaggedHost"]
    if nil == schema {
        t.Fatalf("expected the host schema to be registered in components, got %v", componentKeyList(components))
    }

    metadata := schema.Properties["metadata"]
    assertReferenceCarriesFloor(t, metadata, "#/components/schemas/cyclicMetadata", "minProperties", components)

    nodes := schema.Properties["nodes"]
    assertReferenceCarriesFloor(t, nodes, "#/components/schemas/cyclicNodes", "minItems", components)

    /* a pointer to a promoted collection already carries its reference inside the nullable wrapper; notEmpty rejects the nil pointer, so the null goes and the floor joins the reference */
    optional := schema.Properties["optional"]
    if nil == optional || true == optional.Nullable {
        t.Fatalf("expected notEmpty to withdraw the null a pointer field advertises, got %+v", optional)
    }
    assertReferenceCarriesFloor(t, optional, "#/components/schemas/cyclicNodes", "minItems", components)

    /* the floor is the whole advertisement: nothing may contradict it, or the field is back to accepting nothing */
    for _, fieldName := range []string{"metadata", "nodes", "optional"} {
        property := schema.Properties[fieldName]
        for _, member := range property.AllOf {
            if true == isImpossibleObject(member) {
                t.Fatalf("expected %q to stay satisfiable behind its reference, got %+v", fieldName, property)
            }
        }
    }

    for _, fieldName := range []string{"metadata", "nodes", "optional"} {
        if false == containsString(schema.Required, fieldName) {
            t.Fatalf("expected notEmpty to keep %q required, got %v", fieldName, schema.Required)
        }
    }
}

/* Following the reference must not soften anything else the validator still rejects: greaterThan rejects every non-numeric value, a parameter a non-parameterizable constraint cannot consume fails the rule closed before any value is examined, and notEmpty on a struct is still the outright rejection it always was. */
func TestBuildSchema_AReferenceStaysUnsatisfiableWhereTheValidatorRejectsIt(t *testing.T) {
    components := map[string]*Schema{}

    buildSchema(reflect.TypeOf(cyclicTaggedHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["cyclicTaggedHost"]
    if nil == schema {
        t.Fatalf("expected the host schema to be registered in components, got %v", componentKeyList(components))
    }

    for _, fieldName := range []string{"ranked", "counted", "node"} {
        property := schema.Properties[fieldName]
        if nil == property {
            t.Fatalf("expected the %q property to exist", fieldName)
        }

        if true == property.Nullable || 0 == len(property.AllOf) || false == isImpossibleObject(property.AllOf[len(property.AllOf)-1]) {
            t.Fatalf("expected %q to stay unsatisfiable behind its reference, got %+v", fieldName, property)
        }
    }

    /* notBlank rejects only the null, and a non-pointer collection advertises none, so the reference is left exactly as it was built */
    labelled := schema.Properties["labelled"]
    if nil == labelled || "#/components/schemas/cyclicNodes" != labelled.Ref || 0 != len(labelled.AllOf) {
        t.Fatalf("expected notBlank on a non-pointer collection to leave the bare reference, got %+v", labelled)
    }
}

/* Resolving a reference is itself a walk over the shape the reference was introduced to survive, so it has to terminate on a components map that points back at itself. Neither a map assembled this way nor a key that names nothing may be followed forever, and neither may answer anything but the stricter struct reading. */
func TestReferencedCollectionKind_ResolutionTerminatesOnACyclicComponentsMap(t *testing.T) {
    components := map[string]*Schema{
        "selfPointing":  {Ref: "#/components/schemas/selfPointing"},
        "leftPointing":  {Ref: "#/components/schemas/rightPointing"},
        "rightPointing": {Ref: "#/components/schemas/leftPointing"},
        "missing":       {Ref: "#/components/schemas/absent"},
        "throughToList": {Ref: "#/components/schemas/list"},
        "list":          {Type: "array", Items: &Schema{Type: "string"}},
        "dictionary":    {Type: "object", AdditionalProperties: &Schema{Type: "string"}},
        "placeholder":   {Type: "object"},
        "record":        {Type: "object", Properties: map[string]*Schema{"name": {Type: "string"}}},
    }

    cases := []struct {
        reference string
        expected  string
    }{
        {"#/components/schemas/selfPointing", ""},
        {"#/components/schemas/leftPointing", ""},
        {"#/components/schemas/missing", ""},
        {"#/components/schemas/absent", ""},
        {"#/components/schemas/throughToList", "array"},
        {"#/components/schemas/list", "array"},
        {"#/components/schemas/dictionary", "object"},
        {"#/components/schemas/placeholder", ""},
        {"#/components/schemas/record", ""},
    }

    for _, testCase := range cases {
        if resolved := referencedCollectionKind(&Schema{Ref: testCase.reference}, components); testCase.expected != resolved {
            t.Fatalf("expected %q to resolve to %q, got %q", testCase.reference, testCase.expected, resolved)
        }

        wrapped := &Schema{AllOf: []*Schema{{Ref: testCase.reference}}, Nullable: true}
        if resolved := referencedCollectionKind(wrapped, components); testCase.expected != resolved {
            t.Fatalf("expected the nullable wrapper of %q to resolve to %q, got %q", testCase.reference, testCase.expected, resolved)
        }
    }

    /* a field carrying no reference at all has nothing to resolve */
    if resolved := referencedCollectionKind(&Schema{Type: "array"}, components); "" != resolved {
        t.Fatalf("expected a schema without a reference to resolve to nothing, got %q", resolved)
    }
}

type cyclicFixedRing [2]*cyclicFixedRing

type cyclicEmptyRing [0]*cyclicEmptyRing

type cyclicFixedArrayHost struct {
    Ring  cyclicFixedRing `json:"ring" validate:"notEmpty"`
    Empty cyclicEmptyRing `json:"empty" validate:"notEmpty"`
}

/* A fixed-length array reaches itself through a pointer element, so it is promoted like any other self-referential collection — but the validator measures a length its type fixes. encoding/json zero-fills a shorter payload, so notEmpty can never fail for a length of two and can never pass for a length of zero, and the floor the reference would otherwise carry has to be dropped in the first case and contradicted in the second, exactly as it is for an inline fixed array. */
func TestBuildSchema_NotEmptyOnAPromotedFixedArrayKeepsTheFixedLengthReading(t *testing.T) {
    components := map[string]*Schema{}

    buildSchema(reflect.TypeOf(cyclicFixedArrayHost{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["cyclicFixedArrayHost"]
    if nil == schema {
        t.Fatalf("expected the host schema to be registered in components, got %v", componentKeyList(components))
    }

    ring := schema.Properties["ring"]
    if nil == ring || "" == ring.Ref || 0 != len(ring.AllOf) || nil != ring.MinItems {
        t.Fatalf("expected notEmpty on a promoted fixed-length array to advertise no floor, got %+v", ring)
    }

    if true == containsString(schema.Required, "ring") {
        t.Fatalf("expected a non-pointer fixed-length array not to be required under notEmpty, got %v", schema.Required)
    }

    empty := schema.Properties["empty"]
    if nil == empty || 0 == len(empty.AllOf) || false == isImpossibleObject(empty.AllOf[len(empty.AllOf)-1]) {
        t.Fatalf("expected notEmpty on a promoted zero-length array to advertise an unsatisfiable field, got %+v", empty)
    }
}

func assertReferenceCarriesFloor(t *testing.T, property *Schema, reference string, facet string, components map[string]*Schema) {
    t.Helper()

    if nil == property {
        t.Fatalf("expected a property carrying %q", reference)
    }

    /* a facet beside a $ref binds nothing, the reference replacing the object it sits in, so the floor has to travel inside an allOf */
    if "" != property.Ref {
        t.Fatalf("expected the reference to move into an allOf so the floor binds with it, got %+v", property)
    }

    if 2 != len(property.AllOf) || reference != property.AllOf[0].Ref {
        t.Fatalf("expected an allOf of [%s, floor], got %+v", reference, property)
    }

    assertResolvableComponentReference(t, property.AllOf[0], components)

    floor := property.AllOf[1]
    if "minItems" == facet {
        if nil == floor.MinItems || 1 != *floor.MinItems {
            t.Fatalf("expected the referenced array to carry minItems 1, got %+v", floor)
        }
        if nil != floor.MinProperties {
            t.Fatalf("expected an array floor to carry no minProperties, got %+v", floor)
        }

        return
    }

    if nil == floor.MinProperties || 1 != *floor.MinProperties {
        t.Fatalf("expected the referenced map to carry minProperties 1, got %+v", floor)
    }
    if nil != floor.MinItems {
        t.Fatalf("expected a map floor to carry no minItems, got %+v", floor)
    }
    if nil != floor.MaxProperties {
        t.Fatalf("expected the map floor to stay satisfiable, got %+v", floor)
    }
}
