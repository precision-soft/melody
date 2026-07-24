package validation

import (
    "fmt"
    "reflect"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    validationcontract "github.com/precision-soft/melody/validation/contract"
)

/* @important maxNestedValidationDepth bounds the recursive descent into nested struct/slice/map/embedded values so a self-referential or deeply cyclic payload cannot overflow the stack; the visited-pointer set below short-circuits genuine reference cycles, and this depth cap is the belt-and-suspenders backstop for value cycles the pointer set cannot observe. */
const maxNestedValidationDepth = 64

/* @important cyclicReference identifies an already-visited pointer/map header during the recursive descent so a reference cycle (a self-referential linked node, a slice/map that reaches back to an ancestor) is validated once and then short-circuited instead of recursing forever. */
type cyclicReference struct {
    pointer uintptr
    typ     reflect.Type
}

func NewValidator() *Validator {
    validator := &Validator{
        constraints: make(map[string]validationcontract.Constraint),
    }

    validator.RegisterConstraint(ConstraintNotBlank, &NotBlank{})
    validator.RegisterConstraint(ConstraintEmail, &Email{})
    validator.RegisterConstraint(ConstraintMinLength, NewMinLength(1))
    validator.RegisterConstraint(ConstraintMaxLength, NewMaxLength(100))
    validator.RegisterConstraint(ConstraintRegex, NewRegex(".*"))
    validator.RegisterConstraint(ConstraintNumeric, &Numeric{})
    validator.RegisterConstraint(ConstraintAlpha, &Alpha{})
    validator.RegisterConstraint(ConstraintAlphanumeric, &Alphanumeric{})
    validator.RegisterConstraint(ConstraintGreaterThan, NewGreaterThan(0))
    validator.RegisterConstraint(ConstraintLessThan, NewLessThan(0))
    validator.RegisterConstraint(ConstraintNotEmpty, NewNotEmpty())

    return validator
}

type Validator struct {
    mutex       sync.RWMutex
    constraints map[string]validationcontract.Constraint
}

func (instance *Validator) RegisterConstraint(name string, constraint validationcontract.Constraint) {
    if "" == name {
        exception.Panic(exception.NewError("constraint name is empty", nil, nil))
    }

    trimmedName := strings.TrimSpace(name)
    if name != trimmedName {
        exception.Panic(
            exception.NewError(
                "constraint name must not contain leading or trailing whitespace",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    if true == internal.IsNilInterface(constraint) {
        exception.Panic(
            exception.NewError(
                "constraint instance is nil",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()

    _, exists := instance.constraints[name]
    if true == exists {
        instance.mutex.Unlock()

        exception.Panic(
            exception.NewError(
                "constraint already registered",
                exceptioncontract.Context{
                    "name": name,
                },
                nil,
            ),
        )
    }

    instance.constraints[name] = constraint

    instance.mutex.Unlock()
}

func (instance *Validator) Validate(data any) error {
    errors := instance.validateInternal(data)

    if 0 == len(errors) {
        return nil
    }

    return errors
}

func (instance *Validator) validateInternal(data any) ValidationErrors {
    if nil == data {
        return nil
    }

    return instance.validateReflected(reflect.ValueOf(data), "", 0, make(map[cyclicReference]bool))
}

/* @important validateReflected drives the recursive cascade: it unwraps pointers/interfaces (skipping nil and already-visited references), then dispatches structs, slices/arrays and maps to their per-kind walkers so that validate tags declared on nested fields are enforced with a path that identifies the offending nested field. Scalar leaves have no tags of their own to enforce here (their owning struct applies the tag) and fall through untouched, so a flat payload with no nested tags produces exactly the same result as before. */
func (instance *Validator) validateReflected(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if maxNestedValidationDepth < depth {
        return errors
    }

    if false == value.IsValid() {
        return errors
    }

    switch value.Kind() {
    case reflect.Interface:
        if true == value.IsNil() {
            return errors
        }

        return instance.validateReflected(value.Elem(), path, depth+1, visited)
    case reflect.Ptr:
        if true == value.IsNil() {
            return errors
        }

        reference := cyclicReference{pointer: value.Pointer(), typ: value.Type()}
        if true == visited[reference] {
            return errors
        }
        visited[reference] = true

        return instance.validateReflected(value.Elem(), path, depth+1, visited)
    case reflect.Struct:
        return instance.validateStruct(value, path, depth, visited)
    case reflect.Slice, reflect.Array:
        return instance.validateSequence(value, path, depth, visited)
    case reflect.Map:
        return instance.validateMap(value, path, depth, visited)
    default:
        return errors
    }
}

/* visibleFieldCandidate pairs a field with the value it was reached through. An invalid value — the field of a nil pointer embed — still takes part in dominance, so a shallower field keeps shadowing what it would shadow were the embed present; it just yields nothing to validate. */
type visibleFieldCandidate struct {
    field reflect.StructField
    value reflect.Value
}

/* validateStruct validates the fields of one json object: the struct's own fields plus everything its embeds promote, resolved with encoding/json's dominance rules — the shallowest field wins a name, an explicit json tag beats an untagged field at equal depth, and an ambiguity drops the name entirely. Only the winners are validated, because only the winners are populated from a payload: validating a shadowed promoted field would run its tag against a permanent zero value and reject every request. */
func (instance *Validator) validateStruct(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    type embeddedLevelItem struct {
        itemType reflect.Type
        value    reflect.Value
        count    int
    }

    resolved := make(map[string]bool)

    embeddedSeen := make(map[reflect.Type]bool)
    embeddedSeen[value.Type()] = true

    level := []*embeddedLevelItem{
        {itemType: value.Type(), value: value, count: 1},
    }

    for 0 < len(level) {
        candidatesByName := make(map[string][]visibleFieldCandidate)
        var order []string

        nextByType := make(map[reflect.Type]*embeddedLevelItem)
        var nextLevel []*embeddedLevelItem

        for _, item := range level {
            for index := 0; index < item.itemType.NumField(); index++ {
                field := item.itemType.Field(index)

                fieldValue := reflect.Value{}
                if true == item.value.IsValid() {
                    fieldValue = item.value.Field(index)
                }

                if true == isPromotedValidationEmbed(field) {
                    /* the embed's own tag runs against the embed value: its promoted fields are payload-populated, so a constraint declared on the embed is satisfiable and must not vanish with the flattening. An unexported embed's value cannot pass through Interface — only its promoted exported fields can — so its tag stays out of reach. */
                    if true == field.IsExported() {
                        errors = append(errors, instance.applyFieldRules(field, fieldValue, embeddedFieldPath(field, path))...)
                    }

                    embeddedType := dereferencedValidationStructType(field.Type)
                    if true == embeddedSeen[embeddedType] {
                        continue
                    }

                    /* a type reached by several equal-depth paths has its fields duplicated per path so a diamond annihilates in the dominance pick, as in encoding/json; two copies decide every tie, so the count is capped there instead of doubling through stacked diamonds */
                    nextItem, exists := nextByType[embeddedType]
                    if false == exists {
                        nextItem = &embeddedLevelItem{
                            itemType: embeddedType,
                            value:    dereferencedValidationStructValue(fieldValue),
                        }
                        nextByType[embeddedType] = nextItem
                        nextLevel = append(nextLevel, nextItem)
                    }

                    nextItem.count = nextItem.count + item.count
                    if 2 < nextItem.count {
                        nextItem.count = 2
                    }

                    continue
                }

                if false == field.IsExported() {
                    continue
                }

                jsonName, omit := validationJsonFieldName(field)
                if true == omit {
                    continue
                }

                if true == resolved[jsonName] {
                    continue
                }

                if _, seen := candidatesByName[jsonName]; false == seen {
                    order = append(order, jsonName)
                }

                duplicateCount := item.count
                if 2 < duplicateCount {
                    duplicateCount = 2
                }

                for copyIndex := 0; copyIndex < duplicateCount; copyIndex++ {
                    candidatesByName[jsonName] = append(candidatesByName[jsonName], visibleFieldCandidate{
                        field: field,
                        value: fieldValue,
                    })
                }
            }
        }

        for _, jsonName := range order {
            resolved[jsonName] = true

            winner, ok := dominantVisibleField(candidatesByName[jsonName])
            if false == ok {
                continue
            }

            errors = append(errors, instance.validateVisibleField(winner, jsonName, path, depth, visited)...)
        }

        for _, nextItem := range nextLevel {
            embeddedSeen[nextItem.itemType] = true
        }

        level = nextLevel
    }

    return errors
}

/* validateVisibleField applies the field's own tag and recurses into its value, under the json name of the field — the promoted fields of an embed keep the parent's path prefix, exactly as the payload spells them. */
func (instance *Validator) validateVisibleField(
    candidate visibleFieldCandidate,
    jsonName string,
    path string,
    depth int,
    visited map[cyclicReference]bool,
) ValidationErrors {
    var errors ValidationErrors

    if false == candidate.value.IsValid() {
        return errors
    }

    fieldPath := jsonName
    if "" != path {
        fieldPath = path + "." + jsonName
    }

    errors = append(errors, instance.applyFieldRules(candidate.field, candidate.value, fieldPath)...)

    return append(errors, instance.validateReflected(candidate.value, fieldPath, depth+1, visited)...)
}

func (instance *Validator) applyFieldRules(field reflect.StructField, value reflect.Value, fieldPath string) ValidationErrors {
    var errors ValidationErrors

    if false == value.IsValid() {
        return errors
    }

    validateTag := field.Tag.Get("validate")
    /* the trimmed comparison keeps a padded " - " a skip marker instead of an unknown rule that rejects every value */
    if trimmedTag := strings.TrimSpace(validateTag); "" == trimmedTag || "-" == trimmedTag {
        return errors
    }

    rules, err := parseValidationTag(validateTag)
    if nil != err {
        return append(errors, NewValidationError(
            fieldPath,
            "invalid validation tag syntax",
            ErrorInvalidRuleSyntax,
            map[string]any{
                "tag": validateTag,
            },
        ))
    }

    for _, rule := range rules {
        validationError := instance.validateRule(value.Interface(), fieldPath, rule)
        if nil != validationError {
            errors = append(errors, validationError)
        }
    }

    return errors
}

/* embeddedFieldPath names the embed the way an error can point at it: by its field name under the parent's path, since the embed itself has no json name of its own. */
func embeddedFieldPath(field reflect.StructField, path string) string {
    if "" == path {
        return field.Name
    }

    return path + "." + field.Name
}

/* dominantVisibleField mirrors encoding/json's dominance pick: a single candidate wins outright, exactly one explicitly json-named candidate beats the untagged ones, and anything else is the ambiguity encoding/json drops — no field is populated, so none is validated. */
func dominantVisibleField(group []visibleFieldCandidate) (visibleFieldCandidate, bool) {
    if 1 == len(group) {
        return group[0], true
    }

    taggedIndex := -1
    taggedCount := 0
    for index, candidate := range group {
        if true == hasExplicitValidationJsonName(candidate.field) {
            taggedCount++
            taggedIndex = index
        }
    }

    if 1 == taggedCount {
        return group[taggedIndex], true
    }

    return visibleFieldCandidate{}, false
}

/* validationJsonFieldName yields the name a payload spells the field with, and whether the field is omitted outright — a field encoding/json never populates cannot be satisfied by any payload, so validating its permanent zero value would reject every request (a field literally named "-" is spelled "-," and stays validated). */
func validationJsonFieldName(field reflect.StructField) (string, bool) {
    tag := field.Tag.Get("json")
    if "-" == tag {
        return "", true
    }

    if "" == tag {
        return field.Name, false
    }

    parts := strings.Split(tag, ",")
    if "" == parts[0] {
        return field.Name, false
    }

    return parts[0], false
}

func hasExplicitValidationJsonName(field reflect.StructField) bool {
    tag := field.Tag.Get("json")
    if "" == tag {
        return false
    }

    parts := strings.Split(tag, ",")

    return "" != parts[0] && "-" != parts[0]
}

/* isPromotedValidationEmbed matches encoding/json's flattening: an anonymous struct (or pointer to one) without an explicit json name flattens onto its parent object; a json-named embed is an ordinary field holding a nested object, and an embedded time.Time is a scalar. */
func isPromotedValidationEmbed(field reflect.StructField) bool {
    if false == field.Anonymous {
        return false
    }

    tag := field.Tag.Get("json")
    if "-" == tag {
        return false
    }

    if "" != tag && "" != strings.Split(tag, ",")[0] {
        return false
    }

    embedded := field.Type
    for reflect.Ptr == embedded.Kind() {
        embedded = embedded.Elem()
    }

    return reflect.Struct == embedded.Kind() && validationTimeType != embedded
}

var validationTimeType = reflect.TypeOf(time.Time{})

func dereferencedValidationStructType(targetType reflect.Type) reflect.Type {
    for reflect.Ptr == targetType.Kind() {
        targetType = targetType.Elem()
    }

    return targetType
}

/* dereferencedValidationStructValue unwraps a (possibly pointer) embed value; a nil pointer yields an invalid value, which keeps its promoted names in the dominance while leaving nothing to validate. */
func dereferencedValidationStructValue(value reflect.Value) reflect.Value {
    for true == value.IsValid() && reflect.Ptr == value.Kind() {
        if true == value.IsNil() {
            return reflect.Value{}
        }

        value = value.Elem()
    }

    return value
}

func (instance *Validator) validateSequence(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if reflect.Slice == value.Kind() {
        if reflect.Uint8 == value.Type().Elem().Kind() {
            /* @important a byte slice is a scalar payload, never a sequence of validatable elements, so it carries no nested tags to enforce. */
            return errors
        }

        if true == value.IsNil() {
            return errors
        }
    }

    for i := 0; i < value.Len(); i++ {
        elementPath := fmt.Sprintf("%s[%d]", path, i)

        errors = append(errors, instance.validateReflected(value.Index(i), elementPath, depth+1, visited)...)
    }

    return errors
}

func (instance *Validator) validateMap(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if true == value.IsNil() {
        return errors
    }

    iterator := value.MapRange()
    for true == iterator.Next() {
        elementPath := fmt.Sprintf("%s[%v]", path, iterator.Key().Interface())

        errors = append(errors, instance.validateReflected(iterator.Value(), elementPath, depth+1, visited)...)
    }

    return errors
}

func (instance *Validator) validateRule(value any, fieldName string, rule validationRule) validationcontract.ValidationError {
    instance.mutex.RLock()
    _, exists := instance.constraints[rule.name]
    instance.mutex.RUnlock()

    if false == exists {
        return NewValidationError(
            fieldName,
            "unknown validation rule",
            ErrorUnknownRule,
            map[string]any{
                "rule": rule.name,
            },
        )
    }

    constraint, paramsOk := instance.createConstraintWithParams(rule.name, rule.params)
    if false == paramsOk {
        return NewValidationError(
            fieldName,
            "invalid validation rule parameter",
            ErrorInvalidRuleSyntax,
            map[string]any{
                "rule":   rule.name,
                "params": rule.params,
            },
        )
    }

    err := constraint.Validate(value, fieldName)
    if nil == err {
        return nil
    }

    if "" != err.Field() {
        return err
    }

    return NewValidationError(
        fieldName,
        err.Message(),
        err.Code(),
        err.Context(),
    )
}

func (instance *Validator) createConstraintWithParams(name string, params map[string]string) (validationcontract.Constraint, bool) {
    instance.mutex.RLock()
    constraint := instance.constraints[name]
    instance.mutex.RUnlock()

    if 0 == len(params) {
        return constraint, true
    }

    parameterized, ok := constraint.(validationcontract.ParameterizedConstraint)
    if false == ok {
        /* @important fail closed when the tag carries parameters the registered constraint cannot consume: validating with the unparameterized singleton would silently enforce a different configuration than the tag declares (a custom `between(min=1,max=5)` would validate with whatever the singleton was built with) */
        return nil, false
    }

    configured, withParamsErr := parameterized.WithParams(params)
    if nil != withParamsErr {
        return nil, false
    }

    if true == internal.IsNilInterface(configured) {
        return nil, false
    }

    return configured, true
}
