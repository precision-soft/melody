package openapi

/* lockstep_test.go is the executable half of the openapi/validation lockstep, and the ONLY place in the tree where the openapi package reaches the validation package — a test-only import, so the generator keeps its dependency-free production surface while the guarantee stops resting on comments.

The oracle is semantic, over VALUES, not over the mirror's predicates: each row builds a struct type carrying a real validate tag, reads the mirror's verdict on a value from the generated schema facets (schemaAccepts), reads the validator's verdict from validation.NewValidator().Validate on the same value, and requires that the mirror never advertises a value the validator refuses — absence included, through the required list. An oracle written on predicates would pin exactly the branches a repair just changed and go blind at the next branch that falls out of step, which is the mistake this file replaces.

The mirror is allowed to refuse MORE than the validator (fail-closed over-approximation); the declared divergences in the other direction are enumerated in declaredDivergence with their reasons, and TestLockstepMirrorStillAdvertisesSatisfiableValues keeps schemaAccepts from going vacuous — a verdict function that answered false for everything would turn the main invariant green and empty. */

import (
    "encoding/base64"
    "reflect"
    "regexp"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/validation"
)

type lockstepPayload struct {
    Name string `json:"name"`
}

/* lockstepNodes is a named self-referential collection: it is promoted into components (collectionSchemaReference), so it exercises the $ref reading of a tag against a collection rather than a struct. */
type lockstepNodes []lockstepNodes

var lockstepFieldTypes = []reflect.Type{
    reflect.TypeOf(""),
    reflect.TypeOf((*string)(nil)),
    reflect.TypeOf(0),
    reflect.TypeOf((*int)(nil)),
    reflect.TypeOf(uint(0)),
    reflect.TypeOf(float64(0)),
    reflect.TypeOf((*float64)(nil)),
    reflect.TypeOf(false),
    reflect.TypeOf([]byte(nil)),
    reflect.TypeOf((*[]byte)(nil)),
    reflect.TypeOf(time.Time{}),
    reflect.TypeOf((*time.Time)(nil)),
    reflect.TypeOf([]string(nil)),
    reflect.TypeOf(map[string]string(nil)),
    reflect.TypeOf(lockstepPayload{}),
    reflect.TypeOf((*lockstepPayload)(nil)),
    reflect.TypeOf(lockstepNodes(nil)),
}

/* every tag class the mirror models: bare parameterized constraints, malformed and negative numeric bounds, the string constraints on every shape, the empty and the uncompilable pattern, syntax errors, and the combinations the repairs interact through. Interface-typed fields are deliberately not in lockstepFieldTypes: the validator judges the DECODED dynamic value there while the schema carries no type to constrain, an exemption declared at the greaterThan/lessThan branches of applyValidation. */
var lockstepTags = []string{
    "min",
    "max",
    "greaterThan",
    "lessThan",
    "regex",
    "min=5abc",
    "max=1e3",
    "greaterThan=abc",
    "lessThan=-0.5",
    "min=",
    "min=+5",
    "max=007",
    "min=-5",
    "max=-1",
    "greaterThan=-1",
    "lessThan=-5",
    "greaterThan=0",
    "lessThan=0",
    "greaterThan=5",
    "min=0",
    "min=1",
    "min=2",
    "max=0",
    "max=5",
    "min=2,max=5",
    "min=2,max=1",
    "notBlank",
    "notEmpty",
    "email",
    "alpha",
    "numeric",
    "alphanumeric",
    "regex=^a+$",
    "regex=*",
    "regex=",
    "alpha,numeric",
    "notBlank,min=2,max=40",
    ",",
    "min(5)",
    "notEmpty(foo)",
}

func lockstepStructType(fieldType reflect.Type, tag string) reflect.Type {
    return reflect.StructOf([]reflect.StructField{{
        Name: "Value",
        Type: fieldType,
        Tag:  reflect.StructTag(`json:"value" validate:"` + tag + `"`),
    }})
}

/* candidateValues answers the value set a field of this type is probed with: the zero value, an empty and a populated collection, boundary strings for every constraint class, and — behind a pointer — the typed nil that decodes from a JSON null, plus each of the element's own candidates. */
func candidateValues(fieldType reflect.Type) []reflect.Value {
    if fieldType == reflect.TypeOf(time.Time{}) {
        return []reflect.Value{
            reflect.ValueOf(time.Time{}),
            reflect.ValueOf(time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)),
        }
    }

    switch fieldType.Kind() {
    case reflect.Ptr:
        candidates := []reflect.Value{reflect.Zero(fieldType)}
        for _, element := range candidateValues(fieldType.Elem()) {
            pointer := reflect.New(fieldType.Elem())
            pointer.Elem().Set(element)
            candidates = append(candidates, pointer)
        }

        return candidates
    case reflect.String:
        var candidates []reflect.Value
        for _, text := range []string{"", "  ", "a", "abc", "abc123", "12345", "ABCdef", "user@example.com", "not-an-email", "aaa", "ăăă", strings.Repeat("a", 120)} {
            candidates = append(candidates, reflect.ValueOf(text).Convert(fieldType))
        }

        return candidates
    case reflect.Int, reflect.Int64:
        var candidates []reflect.Value
        for _, number := range []int{0, 1, 5, -1, -5, 100} {
            candidates = append(candidates, reflect.ValueOf(number).Convert(fieldType))
        }

        return candidates
    case reflect.Uint, reflect.Uint64:
        var candidates []reflect.Value
        for _, number := range []uint{0, 1, 5, 100} {
            candidates = append(candidates, reflect.ValueOf(number).Convert(fieldType))
        }

        return candidates
    case reflect.Float32, reflect.Float64:
        var candidates []reflect.Value
        for _, number := range []float64{0, 0.5, -0.5, 5, -5} {
            candidates = append(candidates, reflect.ValueOf(number).Convert(fieldType))
        }

        return candidates
    case reflect.Bool:
        return []reflect.Value{reflect.ValueOf(true), reflect.ValueOf(false)}
    case reflect.Slice:
        if reflect.Uint8 == fieldType.Elem().Kind() {
            return []reflect.Value{
                reflect.Zero(fieldType),
                reflect.MakeSlice(fieldType, 0, 0),
                reflect.ValueOf([]byte("ab")).Convert(fieldType),
            }
        }

        empty := reflect.MakeSlice(fieldType, 0, 0)
        one := reflect.MakeSlice(fieldType, 1, 1)
        two := reflect.MakeSlice(fieldType, 2, 2)
        if reflect.String == fieldType.Elem().Kind() {
            one.Index(0).SetString("a")
            two.Index(0).SetString("a")
            two.Index(1).SetString("b")
        }

        return []reflect.Value{reflect.Zero(fieldType), empty, one, two}
    case reflect.Map:
        empty := reflect.MakeMap(fieldType)
        one := reflect.MakeMap(fieldType)
        one.SetMapIndex(reflect.ValueOf("a").Convert(fieldType.Key()), reflect.Zero(fieldType.Elem()))

        return []reflect.Value{reflect.Zero(fieldType), empty, one}
    case reflect.Struct:
        zero := reflect.Zero(fieldType)
        populated := reflect.New(fieldType).Elem()
        if 0 < fieldType.NumField() && reflect.String == fieldType.Field(0).Type.Kind() {
            populated.Field(0).SetString("x")
        }

        return []reflect.Value{zero, populated}
    default:
        return []reflect.Value{reflect.Zero(fieldType)}
    }
}

/* the validator's own email pattern (validation/constraint_email.go); the oracle reads the schema's email format annotation as binding with the same acceptance, and the string candidates keep to spellings every email semantics agrees on, so the copy is not load-bearing. */
var lockstepEmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func isNullCandidate(value reflect.Value) bool {
    return reflect.Ptr == value.Kind() && true == value.IsNil()
}

/* jsonTextForm answers the text a value is spelled as in the JSON payload, for the shapes the document renders as strings: the string itself, a []byte as its base64 form, a time.Time as its RFC 3339 form. The length and pattern facets bind that spelling, which is what a client generates against. */
func jsonTextForm(value reflect.Value) (string, bool) {
    if value.Type() == reflect.TypeOf(time.Time{}) {
        return value.Interface().(time.Time).Format(time.RFC3339Nano), true
    }

    if reflect.String == value.Kind() {
        return value.String(), true
    }

    if reflect.Slice == value.Kind() && reflect.Uint8 == value.Type().Elem().Kind() {
        return base64.StdEncoding.EncodeToString(value.Bytes()), true
    }

    return "", false
}

func jsonNumberForm(value reflect.Value) (float64, bool) {
    switch value.Kind() {
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        return float64(value.Int()), true
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return float64(value.Uint()), true
    case reflect.Float32, reflect.Float64:
        return value.Float(), true
    }

    return 0, false
}

/* schemaAccepts is the document's verdict on a value: whether a client generating a payload against these facets would consider the value valid for the field. It resolves $ref through the components map, conjoins allOf, negates not, answers null from the nullable flag, and binds the length, pattern, numeric window, items, properties and enum facets against the value's JSON form. It deliberately reads only what the document SAYS — it knows nothing of the tag that produced the facets — so a mirror branch that stops matching the validator is caught by the verdicts disagreeing, not by a predicate happening to be re-derived here. TestLockstepMirrorStillAdvertisesSatisfiableValues is the guard against this function answering false for everything, which would silence the whole oracle. */
func schemaAccepts(schema *Schema, components map[string]*Schema, value reflect.Value) bool {
    if nil == schema {
        return true
    }

    if "" != schema.Ref {
        key := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
        component, exists := components[key]
        if false == exists || nil == component {
            return true
        }

        return schemaAccepts(component, components, value)
    }

    if true == isNullCandidate(value) {
        return schema.Nullable
    }

    resolved := value
    for reflect.Ptr == resolved.Kind() {
        resolved = resolved.Elem()
    }

    if nil != schema.Not && true == schemaAccepts(schema.Not, components, resolved) {
        return false
    }

    for _, member := range schema.AllOf {
        if false == schemaAccepts(member, components, resolved) {
            return false
        }
    }

    if nil != schema.Enum {
        found := false
        for _, allowed := range *schema.Enum {
            if true == reflect.DeepEqual(allowed, resolved.Interface()) {
                found = true
            }
        }
        if false == found {
            return false
        }
    }

    if text, isText := jsonTextForm(resolved); true == isText {
        length := utf8.RuneCountInString(text)
        if nil != schema.MinLength && length < *schema.MinLength {
            return false
        }
        if nil != schema.MaxLength && length > *schema.MaxLength {
            return false
        }
        if "" != schema.Pattern {
            compiled, compileErr := regexp.Compile(schema.Pattern)
            if nil == compileErr && false == compiled.MatchString(text) {
                return false
            }
        }
        if "email" == schema.Format && false == lockstepEmailPattern.MatchString(text) {
            return false
        }
    }

    if number, isNumber := jsonNumberForm(resolved); true == isNumber {
        if nil != schema.Minimum {
            if number < *schema.Minimum {
                return false
            }
            if number == *schema.Minimum && nil != schema.ExclusiveMinimum && true == *schema.ExclusiveMinimum {
                return false
            }
        }
        if nil != schema.Maximum {
            if number > *schema.Maximum {
                return false
            }
            if number == *schema.Maximum && nil != schema.ExclusiveMaximum && true == *schema.ExclusiveMaximum {
                return false
            }
        }
    }

    if (reflect.Slice == resolved.Kind() && reflect.Uint8 != resolved.Type().Elem().Kind()) || reflect.Array == resolved.Kind() {
        length := resolved.Len()
        if nil != schema.MinItems && length < *schema.MinItems {
            return false
        }
        if nil != schema.MaxItems && length > *schema.MaxItems {
            return false
        }
    }

    if reflect.Map == resolved.Kind() {
        length := resolved.Len()
        if nil != schema.MinProperties && length < *schema.MinProperties {
            return false
        }
        if nil != schema.MaxProperties && length > *schema.MaxProperties {
            return false
        }
    }

    if reflect.Struct == resolved.Kind() && resolved.Type() != reflect.TypeOf(time.Time{}) {
        properties := resolved.NumField()
        if nil != schema.MinProperties && properties < *schema.MinProperties {
            return false
        }
        if nil != schema.MaxProperties && properties > *schema.MaxProperties {
            return false
        }
    }

    return true
}

/* declaredDivergence enumerates the known places the document advertises a value the validator refuses, each one declared in schema.go with its reason. Exactly one exists: notBlank's whitespace-only rejection on a genuine string cannot be expressed by minLength, so a non-empty blank string is advertised at length >= 1 and refused by the constraint's TrimSpace. Anything else that trips the invariant is a real divergence of the mirror. */
func declaredDivergence(tag string, fieldType reflect.Type, value reflect.Value) bool {
    hasNotBlank := false
    for _, rule := range splitRules(tag) {
        name, _ := splitRule(rule)
        if "notBlank" == name {
            hasNotBlank = true
        }
    }
    if false == hasNotBlank {
        return false
    }

    resolved := value
    for reflect.Ptr == resolved.Kind() && false == resolved.IsNil() {
        resolved = resolved.Elem()
    }
    if reflect.String != resolved.Kind() {
        return false
    }

    text := resolved.String()

    return "" != text && "" == strings.TrimSpace(text)
}

func lockstepRequired(schema *Schema) bool {
    for _, name := range schema.Required {
        if "value" == name {
            return true
        }
    }

    return false
}

/* TestLockstepMirrorNeverAdvertisesWhatTheValidatorRefuses is the lockstep invariant itself, quantified over every tag class crossed with every field shape: a value the document's facets accept must be a value the real validator accepts, and a field the document leaves optional must have its absent zero value accepted. The mirror refusing more than the validator is legal (fail-closed); the reverse is the silent divergence this file exists to make loud. */
func TestLockstepMirrorNeverAdvertisesWhatTheValidatorRefuses(t *testing.T) {
    validatorInstance := validation.NewValidator()

    for _, tag := range lockstepTags {
        for _, fieldType := range lockstepFieldTypes {
            structType := lockstepStructType(fieldType, tag)
            components := map[string]*Schema{}
            names := map[reflect.Type]string{}
            schema := schemaFromType(structType, components, names)

            property, exists := schema.Properties["value"]
            if false == exists || nil == property {
                t.Fatalf("tag %q on %v: the generated schema carries no property for the field", tag, fieldType)
            }

            if false == lockstepRequired(schema) {
                zeroStruct := reflect.New(structType).Elem()
                if validateErr := validatorInstance.Validate(zeroStruct.Interface()); nil != validateErr {
                    t.Errorf(
                        "tag %q on %v: the spec leaves the field optional, the validator refuses the absent zero value: %v",
                        tag, fieldType, validateErr,
                    )
                }
            }

            for _, candidate := range candidateValues(fieldType) {
                if true == declaredDivergence(tag, fieldType, candidate) {
                    continue
                }

                if false == schemaAccepts(property, components, candidate) {
                    continue
                }

                structValue := reflect.New(structType).Elem()
                structValue.Field(0).Set(candidate)
                if validateErr := validatorInstance.Validate(structValue.Interface()); nil != validateErr {
                    t.Errorf(
                        "tag %q on %v: the spec advertises %#v, the validator refuses it: %v",
                        tag, fieldType, candidate.Interface(), validateErr,
                    )
                }
            }
        }
    }
}

/* TestLockstepMirrorStillAdvertisesSatisfiableValues is the anti-vacuity half: for rows that are satisfiable on both sides it requires the document to ACCEPT the value and the validator to agree. Without it, a schemaAccepts that answered false for everything — or a mirror that marked every field unsatisfiable — would leave the main invariant green while proving nothing. */
func TestLockstepMirrorStillAdvertisesSatisfiableValues(t *testing.T) {
    validatorInstance := validation.NewValidator()

    rows := []struct {
        tag       string
        fieldType reflect.Type
        value     any
    }{
        {"min=2,max=5", reflect.TypeOf(""), "abc"},
        {"min=0", reflect.TypeOf(""), ""},
        {"notBlank", reflect.TypeOf(""), "abc"},
        {"notBlank,min=2,max=40", reflect.TypeOf(""), "melody"},
        {"email", reflect.TypeOf(""), "user@example.com"},
        {"alpha", reflect.TypeOf(""), "abc"},
        {"alpha", reflect.TypeOf(""), ""},
        {"numeric", reflect.TypeOf(""), "12345"},
        {"alphanumeric", reflect.TypeOf(""), "abc123"},
        {"regex=^a+$", reflect.TypeOf(""), "aaa"},
        {"greaterThan=0", reflect.TypeOf(0), 5},
        {"lessThan=0", reflect.TypeOf(0), -5},
        {"greaterThan=-1", reflect.TypeOf(0), 0},
        {"greaterThan=-1", reflect.TypeOf(uint(0)), uint(0)},
        {"notEmpty", reflect.TypeOf([]string(nil)), []string{"a"}},
        {"notEmpty", reflect.TypeOf(map[string]string(nil)), map[string]string{"a": "b"}},
        {"notEmpty", reflect.TypeOf(""), "a"},
    }

    for _, row := range rows {
        structType := lockstepStructType(row.fieldType, row.tag)
        components := map[string]*Schema{}
        names := map[reflect.Type]string{}
        schema := schemaFromType(structType, components, names)

        property := schema.Properties["value"]
        candidate := reflect.ValueOf(row.value).Convert(row.fieldType)

        if false == schemaAccepts(property, components, candidate) {
            t.Errorf("tag %q on %v: the document must advertise %#v as valid and does not", row.tag, row.fieldType, row.value)
        }

        structValue := reflect.New(structType).Elem()
        structValue.Field(0).Set(candidate)
        if validateErr := validatorInstance.Validate(structValue.Interface()); nil != validateErr {
            t.Errorf("tag %q on %v: the row is meant to be satisfiable on both sides, the validator refuses %#v: %v", row.tag, row.fieldType, row.value, validateErr)
        }
    }

    nullRows := []struct {
        tag       string
        fieldType reflect.Type
    }{
        {"max=5", reflect.TypeOf((*string)(nil))},
        {"min=1", reflect.TypeOf((*[]byte)(nil))},
        {"email", reflect.TypeOf((*int)(nil))},
    }

    for _, row := range nullRows {
        structType := lockstepStructType(row.fieldType, row.tag)
        components := map[string]*Schema{}
        names := map[reflect.Type]string{}
        schema := schemaFromType(structType, components, names)

        property := schema.Properties["value"]
        nullCandidate := reflect.Zero(row.fieldType)

        if false == schemaAccepts(property, components, nullCandidate) {
            t.Errorf("tag %q on %v: the nil-skipping constraint leaves null valid and the document must keep advertising it", row.tag, row.fieldType)
        }

        structValue := reflect.New(structType).Elem()
        if validateErr := validatorInstance.Validate(structValue.Interface()); nil != validateErr {
            t.Errorf("tag %q on %v: the validator is meant to skip the nil pointer and does not: %v", row.tag, row.fieldType, validateErr)
        }
    }
}

/* TestLockstepNumericBoundAgreesWithValidator pins parseBoundStrict to the validator's parseIntStrict through the public doors alone: for every bound spelling, the tag goes through the real generator on one side and the real validator on the other, and the two verdicts must be EQUAL on every candidate value — not merely fail-closed — because a length bound on a genuine string is an exact mirror, with no over-approximation to hide behind. The pre-repair test this replaces pinned a hand-written copy of the old acceptance table and never touched the validator, so it stayed green while the two parsers diverged. */
func TestLockstepNumericBoundAgreesWithValidator(t *testing.T) {
    validatorInstance := validation.NewValidator()

    bounds := []string{"5", "0", "007", "+5", "120", "9223372036854775807", "5abc", "1e3", "-0.5", "abc", "", "-5", "-1", "99999999999999999999"}
    stringType := reflect.TypeOf("")

    for _, bound := range bounds {
        for _, constraint := range []string{"min", "max"} {
            tag := constraint + "=" + bound
            structType := lockstepStructType(stringType, tag)
            components := map[string]*Schema{}
            names := map[reflect.Type]string{}
            schema := schemaFromType(structType, components, names)
            property := schema.Properties["value"]

            for _, candidate := range candidateValues(stringType) {
                mirrorAccepts := schemaAccepts(property, components, candidate)

                structValue := reflect.New(structType).Elem()
                structValue.Field(0).Set(candidate)
                validatorAccepts := nil == validatorInstance.Validate(structValue.Interface())

                if mirrorAccepts != validatorAccepts {
                    t.Errorf(
                        "tag %q: the document and the validator disagree on %q — document %v, validator %v",
                        tag, candidate.String(), mirrorAccepts, validatorAccepts,
                    )
                }
            }
        }
    }
}
