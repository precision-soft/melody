package openapi

import (
    "reflect"
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

/* @info character-class and non-empty constraints must reach the spec (CR #66) */

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
    applyValidation(minSchema, "min")
    if nil == minSchema.MinLength || 1 != *minSchema.MinLength {
        t.Fatalf("expected bare min to advertise minLength 1, got %v", minSchema.MinLength)
    }

    maxSchema := &Schema{Type: "string"}
    applyValidation(maxSchema, "max")
    if nil == maxSchema.MaxLength || 100 != *maxSchema.MaxLength {
        t.Fatalf("expected bare max to advertise maxLength 100, got %v", maxSchema.MaxLength)
    }
}

func TestApplyValidation_BareNumericConstraintsMirrorValidatorDefaults(t *testing.T) {
    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan")
    if nil == greaterThanSchema.Minimum || 0 != *greaterThanSchema.Minimum {
        t.Fatalf("expected bare greaterThan to advertise minimum 0, got %v", greaterThanSchema.Minimum)
    }
    if nil == greaterThanSchema.ExclusiveMinimum || false == *greaterThanSchema.ExclusiveMinimum {
        t.Fatalf("expected bare greaterThan to advertise an exclusive minimum")
    }

    lessThanSchema := &Schema{Type: "number"}
    applyValidation(lessThanSchema, "lessThan")
    if nil == lessThanSchema.Maximum || 0 != *lessThanSchema.Maximum {
        t.Fatalf("expected bare lessThan to advertise maximum 0, got %v", lessThanSchema.Maximum)
    }
    if nil == lessThanSchema.ExclusiveMaximum || false == *lessThanSchema.ExclusiveMaximum {
        t.Fatalf("expected bare lessThan to advertise an exclusive maximum")
    }
}

func TestApplyValidation_ValuedConstraintsStillHonourTheirValue(t *testing.T) {
    minSchema := &Schema{Type: "string"}
    applyValidation(minSchema, "min(value=3)")
    if nil == minSchema.MinLength || 3 != *minSchema.MinLength {
        t.Fatalf("expected min(value=3) to advertise minLength 3, got %v", minSchema.MinLength)
    }

    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan(value=5)")
    if nil == greaterThanSchema.Minimum || 5 != *greaterThanSchema.Minimum {
        t.Fatalf("expected greaterThan(value=5) to advertise minimum 5, got %v", greaterThanSchema.Minimum)
    }
}
/* @info a malformed numeric/length tag makes the validator fail the field closed (post-CR70), so the spec advertises an unsatisfiable schema rather than a passable default (CR #71 supersedes CR #64/#65) */

type malformedBoundRequestCR64 struct {
    Name string `json:"name" validate:"max=abc"`
    Code string `json:"code" validate:"min=xyz"`
}

func TestBuildSchema_MalformedMinMaxBoundIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(malformedBoundRequestCR64{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["malformedBoundRequestCR64"]
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

type malformedNumericBoundRequestCR65 struct {
    Floor   int `json:"floor" validate:"greaterThan=abc"`
    Ceiling int `json:"ceiling" validate:"lessThan=xyz"`
}

func TestBuildSchema_MalformedGreaterLessThanBoundIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(malformedNumericBoundRequestCR65{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["malformedNumericBoundRequestCR65"]
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

type byteEmailRequestCR65 struct {
    Blob []byte `json:"blob" validate:"email"`
}

func TestBuildSchema_EmailDoesNotClobberStructuralByteFormat(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(byteEmailRequestCR65{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["byteEmailRequestCR65"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    blob := schema.Properties["blob"]
    if nil == blob || "string" != blob.Type || "byte" != blob.Format {
        t.Fatalf("expected a []byte field to keep format byte even with validate:email, got: %+v", blob)
    }
}

/* @info notBlank/notEmpty reject a null pointer, so the spec must not advertise the field nullable (CR #71) */

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

/* @info a degenerate min=0 must not suppress the notEmpty/notBlank non-empty floor, in either tag order (CR #71) */

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

/* @info CR #74 — a constraint whose validator rejects the field's underlying kind outright (notEmpty on a struct, greaterThan/lessThan on a non-numeric) must advertise the field unsatisfiable, mirroring the scalar/time.Time handling closed in CR #72/#73 */

type cr74InnerStruct struct {
    Value string `json:"value"`
}

type cr74NotEmptyOnStructRequest struct {
    Named    cr74InnerStruct  `json:"named" validate:"notEmpty"`
    NamedPtr *cr74InnerStruct `json:"namedPtr" validate:"notEmpty"`
    Inline   struct {
        Field string `json:"field"`
    } `json:"inline" validate:"notEmpty"`
}

func isImpossibleObject(schema *Schema) bool {
    return nil != schema && nil != schema.MinProperties && 1 == *schema.MinProperties &&
        nil != schema.MaxProperties && 0 == *schema.MaxProperties
}

func TestBuildSchema_NotEmptyOnStructIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(cr74NotEmptyOnStructRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["cr74NotEmptyOnStructRequest"]
    if nil == schema {
        t.Fatalf("expected the request schema to be registered in components")
    }

    /* @important a named struct field renders as a $ref; notEmpty's validator rejects a struct value outright, so the $ref must be wrapped in a contradictory object constraint under allOf rather than advertised as a satisfiable object */
    named := schema.Properties["named"]
    if nil == named || "" != named.Ref || 2 != len(named.AllOf) {
        t.Fatalf("expected named struct notEmpty to wrap the $ref in a contradictory allOf, got %+v", named)
    }
    if "#/components/schemas/cr74InnerStruct" != named.AllOf[0].Ref || false == isImpossibleObject(named.AllOf[1]) {
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

type cr74NumericOnNonNumericRequest struct {
    Code  string            `json:"code" validate:"greaterThan=0"`
    Flag  bool              `json:"flag" validate:"lessThan=1"`
    Items []string          `json:"items" validate:"greaterThan=0"`
    Bag   map[string]int    `json:"bag" validate:"lessThan=0"`
}

func TestBuildSchema_GreaterLessThanOnNonNumericIsUnsatisfiable(t *testing.T) {
    components := map[string]*Schema{}
    buildSchema(reflect.TypeOf(cr74NumericOnNonNumericRequest{}), components, map[reflect.Type]string{}, map[reflect.Type]bool{})

    schema := components["cr74NumericOnNonNumericRequest"]
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
