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
