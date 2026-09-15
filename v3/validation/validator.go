package validation

import (
    "encoding/json"
    goerrors "errors"
    "fmt"
    "reflect"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

const maxNestedValidationDepth = 64

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

/* Validator owns a synchronized constraint registry and rule cache. Validation data stays local to each call; registered constraints must support concurrent use. */
type Validator struct {
    mutex       sync.RWMutex
    constraints map[string]validationcontract.Constraint

    constructedConstraints sync.Map
}

type constructedConstraint struct {
    constraint   validationcontract.Constraint
    ok           bool
    refusalCause string
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

func (instance *Validator) validateReflected(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if false == value.IsValid() {
        return errors
    }

    if true == holdsNoValidationMember(value) {
        return errors
    }

    if maxNestedValidationDepth < depth {
        if false == typeCanCarryValidationTag(value.Type()) {
            return errors
        }

        return append(errors, NewValidationError(
            path,
            "this field is nested deeper than validation allows",
            ErrorNestingDepthExceeded,
            map[string]any{
                "maxDepth": maxNestedValidationDepth,
            },
        ))
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

        errors = append(errors, instance.validateReflected(value.Elem(), path, depth+1, visited)...)

        delete(visited, reference)

        return errors
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

func holdsNoValidationMember(value reflect.Value) bool {
    switch value.Kind() {
    case reflect.Ptr, reflect.Interface:
        return value.IsNil()
    case reflect.Slice, reflect.Map:

        return 0 == value.Len()
    default:
        return false
    }
}

var nestedTagBearingTypeCache sync.Map

func typeCanCarryValidationTag(targetType reflect.Type) bool {
    if cached, exists := nestedTagBearingTypeCache.Load(targetType); true == exists {
        return cached.(bool)
    }

    carries := typeCarriesValidationTag(targetType, make(map[reflect.Type]bool))

    nestedTagBearingTypeCache.Store(targetType, carries)

    return carries
}

func typeCarriesValidationTag(targetType reflect.Type, seen map[reflect.Type]bool) bool {
    if nil == targetType || true == seen[targetType] {
        return false
    }
    seen[targetType] = true

    switch targetType.Kind() {
    case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Map:

        return typeCarriesValidationTag(targetType.Elem(), seen)
    case reflect.Interface:
        return true
    case reflect.Struct:
        for index := 0; index < targetType.NumField(); index++ {
            field := targetType.Field(index)

            if "" != field.Tag.Get("validate") {
                return true
            }

            if true == typeCarriesValidationTag(field.Type, seen) {
                return true
            }
        }

        return false
    default:
        return false
    }
}

type visibleFieldCandidate struct {
    field reflect.StructField
    value reflect.Value
}

func (instance *Validator) validateStruct(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if true == promotesValidationTimeCodec(value.Type()) {
        return errors
    }

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

                    if true == field.IsExported() {
                        errors = append(errors, instance.applyFieldRules(field, fieldValue, embeddedFieldPath(field, path))...)
                    }

                    embeddedType := dereferencedValidationStructType(field.Type)
                    if true == embeddedSeen[embeddedType] {
                        continue
                    }

                    nextItem, exists := nextByType[embeddedType]
                    if false == exists {
                        nextItem = &embeddedLevelItem{
                            itemType: embeddedType,
                            value:    dereferencedValidationStructValue(fieldValue),
                        }
                        nextByType[embeddedType] = nextItem
                        nextLevel = append(nextLevel, nextItem)
                    }

                    nextItem.count = nextItem.count + 1
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

    if trimmedTag := strings.TrimSpace(validateTag); "" == trimmedTag || "-" == trimmedTag {
        return errors
    }

    rules, err := parseValidationTag(validateTag)
    if nil != err {
        context := map[string]any{
            "tag": validateTag,
        }

        var parseError *exception.Error
        if true == goerrors.As(err, &parseError) {
            if part, exists := parseError.Context()["part"]; true == exists {
                context["part"] = part
            }
        }

        return append(errors, NewValidationError(
            fieldPath,
            "invalid validation tag syntax",
            ErrorInvalidRuleSyntax,
            context,
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

func embeddedFieldPath(field reflect.StructField, path string) string {
    if "" == path {
        return field.Name
    }

    return path + "." + field.Name
}

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

    return reflect.Struct == embedded.Kind()
}

var validationTimeType = reflect.TypeOf(time.Time{})
var validationJsonMarshalerInterfaceType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

var validationTimeCodecCache sync.Map

func promotesValidationTimeCodec(structType reflect.Type) bool {
    if cached, exists := validationTimeCodecCache.Load(structType); true == exists {
        return cached.(bool)
    }

    promotes := promotesValidationTimeEncoding(structType) && refusesValidationObjectBody(structType)

    stored, _ := validationTimeCodecCache.LoadOrStore(structType, promotes)

    return stored.(bool)
}

func promotesValidationTimeEncoding(structType reflect.Type) bool {
    if false == structType.Implements(validationJsonMarshalerInterfaceType) {
        return false
    }

    origin, depth, resolved := promotedValidationMarshalerOrigin(structType, make(map[reflect.Type]bool))
    if false == resolved {
        return false
    }

    return 0 < depth && validationTimeType == origin
}

func refusesValidationObjectBody(structType reflect.Type) (refuses bool) {
    defer func() {
        if recovered := recover(); nil != recovered {
            refuses = false
        }
    }()

    return nil != json.Unmarshal([]byte("{}"), reflect.New(structType).Interface())
}

func promotedValidationMarshalerOrigin(targetType reflect.Type, path map[reflect.Type]bool) (reflect.Type, int, bool) {
    if validationTimeType == targetType {
        return validationTimeType, 0, true
    }

    if reflect.Struct != targetType.Kind() {
        return targetType, 0, true
    }

    if true == path[targetType] {
        return nil, 0, false
    }
    path[targetType] = true
    defer delete(path, targetType)

    var winner reflect.Type
    winnerDepth := 0
    winnerCount := 0

    for index := 0; index < targetType.NumField(); index++ {
        field := targetType.Field(index)
        if false == field.Anonymous {
            continue
        }

        if false == field.Type.Implements(validationJsonMarshalerInterfaceType) {
            continue
        }

        embedded := field.Type
        if reflect.Ptr == embedded.Kind() {
            embedded = dereferencedValidationStructType(embedded)

            if false == embedded.Implements(validationJsonMarshalerInterfaceType) {
                return nil, 0, false
            }
        }

        origin, originDepth, resolved := promotedValidationMarshalerOrigin(embedded, path)
        if false == resolved {
            return nil, 0, false
        }

        originDepth++

        if 0 == winnerCount || originDepth < winnerDepth {
            winner = origin
            winnerDepth = originDepth
            winnerCount = 1

            continue
        }

        if originDepth == winnerDepth {
            winnerCount++
        }
    }

    if 1 != winnerCount {
        return targetType, 0, true
    }

    return winner, winnerDepth, true
}

func dereferencedValidationStructType(targetType reflect.Type) reflect.Type {
    for reflect.Ptr == targetType.Kind() {
        targetType = targetType.Elem()
    }

    return targetType
}

func dereferencedValidationStructValue(value reflect.Value) reflect.Value {
    for true == value.IsValid() && reflect.Ptr == value.Kind() {
        if true == value.IsNil() {
            return reflect.New(dereferencedValidationStructType(value.Type())).Elem()
        }

        value = value.Elem()
    }

    return value
}

func (instance *Validator) validateSequence(value reflect.Value, path string, depth int, visited map[cyclicReference]bool) ValidationErrors {
    var errors ValidationErrors

    if reflect.Slice == value.Kind() {
        if reflect.Uint8 == value.Type().Elem().Kind() {

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

    constraint, paramsOk, refusalCause := instance.createConstraintWithParams(rule.name, rule.params)
    if false == paramsOk {
        context := map[string]any{
            "rule":   rule.name,
            "params": copyValidationRuleParams(rule.params),
        }

        if "" != refusalCause {
            context["cause"] = refusalCause
        }

        return NewValidationError(
            fieldName,
            "invalid validation rule parameter",
            ErrorInvalidRuleSyntax,
            context,
        )
    }

    err := constraint.Validate(value, fieldName)

    if true == internal.IsNilInterface(err) {
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

func constraintCacheKey(name string, params map[string]string) string {
    keys := make([]string, 0, len(params))
    for key := range params {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    builder := strings.Builder{}
    builder.WriteString(strconv.Itoa(len(name)))
    builder.WriteString(":")
    builder.WriteString(name)

    for _, key := range keys {
        value := params[key]

        builder.WriteString(strconv.Itoa(len(key)))
        builder.WriteString(":")
        builder.WriteString(key)
        builder.WriteString(strconv.Itoa(len(value)))
        builder.WriteString(":")
        builder.WriteString(value)
    }

    return builder.String()
}

func (instance *Validator) createConstraintWithParams(name string, params map[string]string) (validationcontract.Constraint, bool, string) {
    instance.mutex.RLock()
    constraint := instance.constraints[name]
    instance.mutex.RUnlock()

    if true == internal.IsNilInterface(constraint) {
        return nil, false, "constraint is not registered"
    }

    if 0 == len(params) {

        if _, parameterized := constraint.(validationcontract.ParameterizedConstraint); true == parameterized {
            return nil, false, "constraint requires parameters"
        }

        return constraint, true, ""
    }

    cacheKey := constraintCacheKey(name, params)

    if cached, exists := instance.constructedConstraints.Load(cacheKey); true == exists {
        constructed := cached.(constructedConstraint)

        return constructed.constraint, constructed.ok, constructed.refusalCause
    }

    configured, configuredOk, refusalCause := buildConstraintWithParams(constraint, params)

    stored, _ := instance.constructedConstraints.LoadOrStore(cacheKey, constructedConstraint{constraint: configured, ok: configuredOk, refusalCause: refusalCause})
    constructed := stored.(constructedConstraint)

    return constructed.constraint, constructed.ok, constructed.refusalCause
}

func buildConstraintWithParams(constraint validationcontract.Constraint, params map[string]string) (validationcontract.Constraint, bool, string) {
    parameterized, ok := constraint.(validationcontract.ParameterizedConstraint)
    if false == ok {

        return nil, false, "constraint does not accept parameters"
    }

    configured, withParamsErr := parameterized.WithParams(copyValidationRuleParams(params))
    if nil != withParamsErr {

        return nil, false, withParamsErr.Error()
    }

    if true == internal.IsNilInterface(configured) {
        return nil, false, "constraint construction returned nil"
    }

    return configured, true, ""
}
