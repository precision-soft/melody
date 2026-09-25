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

/* maxNestedValidationDepth bounds the recursive descent so a deeply nested payload cannot overflow the stack; reaching it is a validation error, never a pass, since the tags below were not enforced. */
const maxNestedValidationDepth = 64

/* cyclicReference identifies a pointer on the current descent path, so a reference cycle is validated once and then short-circuited. The set is path-scoped, since only an ancestor on the path closes a cycle; a shared non-cyclic pointer is validated under every path, which the memo of validationWalk keeps affordable. */
type cyclicReference struct {
    pointer uintptr
    typ     reflect.Type
}

/* validationMemoKey identifies one walk of a pointer: the pointer, its type and its depth, since the depth cap answers differently at different depths. */
type validationMemoKey struct {
    pointer uintptr
    typ     reflect.Type
    depth   int
}

/* memoizedValidationError is one error of a memoized walk. An error under the walked path is kept relative to it and re-prefixed under any later path; an error a constraint answered under a field of its own is kept verbatim. An error of a constraint's own type is not memoized (see remember). */
type memoizedValidationError struct {
    verbatim      validationcontract.ValidationError
    relativeField string
    message       string
    code          string
    context       map[string]any
}

/* validationWalk is the state of one Validate call: the path-scoped set that closes reference cycles, and the whole-call memo that answers a pointer already walked at the same depth with its first walk's errors, re-spelled under the new path. A walk cut short by an ancestor memoizes only what it saw; the ancestor's errors are reported under its own path. */
type validationWalk struct {
    onPath map[cyclicReference]bool
    memo   map[validationMemoKey][]memoizedValidationError
}

func newValidationWalk() *validationWalk {
    return &validationWalk{
        onPath: make(map[cyclicReference]bool),
        memo:   make(map[validationMemoKey][]memoizedValidationError),
    }
}

/* remember files the errors of a finished walk under the key, each relative to the walked path. A walk that produced an error of a constraint's own type is not filed, since re-spelling would change that type, so such a node is walked again per path. */
func (instance *validationWalk) remember(key validationMemoKey, path string, errors ValidationErrors) {
    memoized := make([]memoizedValidationError, 0, len(errors))
    for _, validationError := range errors {
        if _, ownType := validationError.(*ValidationError); false == ownType {
            return
        }

        if false == fieldLiesUnderPath(validationError.Field(), path) {
            memoized = append(memoized, memoizedValidationError{verbatim: validationError})

            continue
        }

        memoized = append(memoized, memoizedValidationError{
            relativeField: strings.TrimPrefix(validationError.Field(), path),
            message:       validationError.Message(),
            code:          validationError.Code(),
            context:       validationError.Context(),
        })
    }

    instance.memo[key] = memoized
}

/* fieldLiesUnderPath asks whether a field is the walked path or a member (`.`) or element (`[`) of it, not merely a string that begins with the path's text. */
func fieldLiesUnderPath(field string, path string) bool {
    if field == path {
        return true
    }

    return strings.HasPrefix(field, path+".") || strings.HasPrefix(field, path+"[")
}

/* recall answers a memoized walk under a new path, or reports that the key was never walked. */
func (instance *validationWalk) recall(key validationMemoKey, path string) (ValidationErrors, bool) {
    memoized, exists := instance.memo[key]
    if false == exists {
        return nil, false
    }

    var errors ValidationErrors
    for _, memoizedError := range memoized {
        if nil != memoizedError.verbatim {
            errors = append(errors, memoizedError.verbatim)

            continue
        }

        errors = append(errors, NewValidationError(
            path+memoizedError.relativeField,
            memoizedError.message,
            memoizedError.code,
            memoizedError.context,
        ))
    }

    return errors, true
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

    /* the constraint a parameterized rule resolves to, keyed by rule name and parameters, so a regex is not recompiled per value; a registered name never changes constraint, so an entry never goes stale */
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

    return instance.validateReflected(reflect.ValueOf(data), "", 0, newValidationWalk())
}

/* validateReflected drives the recursive cascade: it unwraps pointers and interfaces, skips nil and on-path references, answers a pointer already walked at this depth from the memo, and dispatches structs, slices, arrays and maps to their walkers. A scalar leaf falls through, its tag belonging to the owning struct. */
func (instance *Validator) validateReflected(value reflect.Value, path string, depth int, walk *validationWalk) ValidationErrors {
    var errors ValidationErrors

    if false == value.IsValid() {
        return errors
    }

    /* the cut runs ahead of the kind switch, since value.Type() is unreadable on an invalid value; a null or empty member holds nothing the cut could truncate, so it is never reported */
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

        return instance.validateReflected(value.Elem(), path, depth+1, walk)
    case reflect.Ptr:
        if true == value.IsNil() {
            return errors
        }

        reference := cyclicReference{pointer: value.Pointer(), typ: value.Type()}
        if true == walk.onPath[reference] {
            return errors
        }

        /* the memo is read before the walk and written after it; a pointer on the path never reaches it, so the set above still closes a cycle */
        memoKey := validationMemoKey{pointer: reference.pointer, typ: reference.typ, depth: depth}
        if recalled, walked := walk.recall(memoKey, path); true == walked {
            return append(errors, recalled...)
        }

        walk.onPath[reference] = true

        walked := instance.validateReflected(value.Elem(), path, depth+1, walk)

        delete(walk.onPath, reference)

        walk.remember(memoKey, path, walked)

        return append(errors, walked...)
    case reflect.Struct:
        return instance.validateStruct(value, path, depth, walk)
    case reflect.Slice, reflect.Array:
        return instance.validateSequence(value, path, depth, walk)
    case reflect.Map:
        return instance.validateMap(value, path, depth, walk)
    default:
        return errors
    }
}

func holdsNoValidationMember(value reflect.Value) bool {
    switch value.Kind() {
    case reflect.Ptr, reflect.Interface:
        return value.IsNil()
    case reflect.Slice, reflect.Map:
        /* the length, not the nil header: an empty slice or map truncates as little as a null one */
        return 0 == value.Len()
    default:
        return false
    }
}

/* nestedTagBearingTypeCache memoizes typeCanCarryValidationTag: the answer is a property of the type alone, and the set of types a program validates is fixed when it is compiled. */
var nestedTagBearingTypeCache sync.Map

/* typeCanCarryValidationTag reports whether a validate tag is declared at or below targetType, so the depth cut tells a truncated subtree with unenforced constraints from one with none. An interface member counts as tag-bearing, since the walk enforces whatever it holds and its depth is client-controlled; the cut fails closed. */
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
        /* a map key is never validated, so only the element type is followed */
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

/* visibleFieldCandidate pairs a field with the value it was reached through, so the dominance pick and the validation of the winner are decided from one record. */
type visibleFieldCandidate struct {
    field reflect.StructField
    value reflect.Value
}

/* validateStruct validates the fields of one json object, its own and those its embeds promote, resolved by encoding/json's dominance rules. Only the winners are validated, since only they are populated from a payload; the openapi mirror (collectStructFields) is kept in lockstep with this walk. */
func (instance *Validator) validateStruct(value reflect.Value, path string, depth int, walk *validationWalk) ValidationErrors {
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
                    /* the embed's own tag runs against the embed value, whose promoted fields a payload populates; an unexported embed's value cannot pass through Interface, so its tag stays out of reach */
                    if true == field.IsExported() {
                        errors = append(errors, instance.applyFieldRules(field, fieldValue, embeddedFieldPath(field, path))...)
                    }

                    embeddedType := dereferencedValidationStructType(field.Type)
                    if true == embeddedSeen[embeddedType] {
                        continue
                    }

                    /* a type reached by several equal-depth paths has its fields duplicated per path, so a diamond annihilates in the dominance pick as in encoding/json; two copies decide every tie, and the count is this level's occurrences, as typeFields counts them */
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

            errors = append(errors, instance.validateVisibleField(winner, jsonName, path, depth, walk)...)
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
    walk *validationWalk,
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

    return append(errors, instance.validateReflected(candidate.value, fieldPath, depth+1, walk)...)
}

func (instance *Validator) applyFieldRules(field reflect.StructField, value reflect.Value, fieldPath string) ValidationErrors {
    var errors ValidationErrors

    if false == value.IsValid() {
        return errors
    }

    validateTag := field.Tag.Get("validate")
    /* the trimmed comparison keeps a padded " - " a skip marker, in lockstep with the openapi mirror's splitRules */
    if trimmedTag := strings.TrimSpace(validateTag); "" == trimmedTag || "-" == trimmedTag {
        return errors
    }

    rules, err := parseValidationTag(validateTag)
    if nil != err {
        context := map[string]any{
            "tag": validateTag,
        }

        /* the parser names the comma-separated segment it refused; without it a long tag reports only that "the tag" is invalid */
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
        if false == internal.IsNilInterface(validationError) {
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

/* dominantVisibleField mirrors encoding/json's dominance pick and the openapi mirror's dominantEmbeddedField: a single candidate wins, exactly one json-named candidate beats the untagged ones, and anything else is an ambiguity nothing populates, so nothing is validated. */
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

/* validationJsonFieldName mirrors the openapi mirror's jsonFieldName: it yields the name a payload spells the field with, and whether encoding/json omits it outright, whose zero value is then never validated. */
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

/* isPromotedValidationEmbed matches encoding/json's flattening and the openapi mirror's isPromotedEmbed: an anonymous struct, or pointer to one, without a json name flattens onto its parent, and a json-named embed is an ordinary field. An embedded time.Time flattens and contributes no name; promotesValidationTimeCodec settles the shape where it owns the whole body. */
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

/* validationTimeCodecCache memoizes promotesValidationTimeCodec: the answer is a property of the type alone, and the decode probe below is then offered to a type once instead of once per value validated. */
var validationTimeCodecCache sync.Map

/* promotesValidationTimeCodec reports a struct a payload can only spell as an RFC 3339 string, which populates nothing else in it, so its constraints are not enforced. It asks both the encoding half, which promotesValidationTimeEncoding keeps identical to the openapi mirror, and the decode half. */
func promotesValidationTimeCodec(structType reflect.Type) bool {
    if cached, exists := validationTimeCodecCache.Load(structType); true == exists {
        return cached.(bool)
    }

    promotes := promotesValidationTimeEncoding(structType) && refusesValidationObjectBody(structType)

    /* LoadOrStore, so a concurrent first touch settles on one verdict: the probe runs application code whose answer is not guaranteed stable */
    stored, _ := validationTimeCodecCache.LoadOrStore(structType, promotes)

    return stored.(bool)
}

/* promotesValidationTimeEncoding reports a struct whose promoted json codec is time.Time's. It must stay identical to the openapi mirror's promotesEmbeddedTimeCodec, or a body the spec describes as a date-time string would be walked as an object. */
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

/* refusesValidationObjectBody asks encoding/json whether a throwaway value of the type decodes an empty object, since reflect cannot tell a promoted UnmarshalJSON from a declared one. A refusal means no body populates a sibling; an acceptance or a panic keeps the constraints enforced. */
func refusesValidationObjectBody(structType reflect.Type) (refuses bool) {
    defer func() {
        if recovered := recover(); nil != recovered {
            refuses = false
        }
    }()

    return nil != json.Unmarshal([]byte("{}"), reflect.New(structType).Interface())
}

/* promotedValidationMarshalerOrigin must stay identical to the openapi mirror's promotedMarshalerOrigin: it reports which type declares the MarshalJSON in the value method set and at what depth, the shallowest winning and a tie promoting nothing. Unresolved (false) keeps the struct walked, the direction that keeps constraints enforced. */
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

            /* a pointer-receiver codec cannot be followed through the embed, so the origin is unresolved */
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

/* dereferencedValidationStructValue unwraps an embed value; a nil pointer yields the zero embed, so its promoted fields are validated against their zero values as a value embed's are. */
func dereferencedValidationStructValue(value reflect.Value) reflect.Value {
    for true == value.IsValid() && reflect.Ptr == value.Kind() {
        if true == value.IsNil() {
            return reflect.New(dereferencedValidationStructType(value.Type())).Elem()
        }

        value = value.Elem()
    }

    return value
}

func (instance *Validator) validateSequence(value reflect.Value, path string, depth int, walk *validationWalk) ValidationErrors {
    var errors ValidationErrors

    if reflect.Slice == value.Kind() {
        if reflect.Uint8 == value.Type().Elem().Kind() {
            /* a byte slice is a scalar payload, never a sequence of validatable elements */
            return errors
        }

        if true == value.IsNil() {
            return errors
        }
    }

    for i := 0; i < value.Len(); i++ {
        elementPath := fmt.Sprintf("%s[%d]", path, i)

        errors = append(errors, instance.validateReflected(value.Index(i), elementPath, depth+1, walk)...)
    }

    return errors
}

func (instance *Validator) validateMap(value reflect.Value, path string, depth int, walk *validationWalk) ValidationErrors {
    var errors ValidationErrors

    if true == value.IsNil() {
        return errors
    }

    iterator := value.MapRange()
    for true == iterator.Next() {
        elementPath := fmt.Sprintf("%s[%v]", path, iterator.Key().Interface())

        errors = append(errors, instance.validateReflected(iterator.Value(), elementPath, depth+1, walk)...)
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
    /* IsNilInterface, since a custom constraint returning a concrete error variable answers a typed nil on success */
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

/* constraintCacheKey encodes a rule name and its parameters into one lookup key. Every component is length-prefixed and the parameter keys are sorted, so the encoding is injective: no two distinct (name, parameters) pairs can produce the same key whatever characters the tag spells them with. */
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

    /* latent: the registry is append-only, so this guard only keeps a future removal from reaching a Validate call on a nil interface */
    if true == internal.IsNilInterface(constraint) {
        return nil, false, "constraint is not registered"
    }

    if 0 == len(params) {
        /* a parameterized rule named without parameters fails closed: the registered instance is a template for WithParams, not a fallback */
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

    /* LoadOrStore, so a concurrent first touch settles on one shared constraint; refusals are cached too, WithParams being a pure function of its parameters */
    stored, _ := instance.constructedConstraints.LoadOrStore(cacheKey, constructedConstraint{constraint: configured, ok: configuredOk, refusalCause: refusalCause})
    constructed := stored.(constructedConstraint)

    return constructed.constraint, constructed.ok, constructed.refusalCause
}

func buildConstraintWithParams(constraint validationcontract.Constraint, params map[string]string) (validationcontract.Constraint, bool, string) {
    parameterized, ok := constraint.(validationcontract.ParameterizedConstraint)
    if false == ok {
        /* parameters the registered constraint cannot consume fail closed */
        return nil, false, "constraint does not accept parameters"
    }

    /* the parameter map belongs to the memoized parse, so the constraint is handed a copy */
    configured, withParamsErr := parameterized.WithParams(copyValidationRuleParams(params))
    if nil != withParamsErr {
        /* the refusal reason travels with the verdict, so a malformed tag, a rejected parameter value and a constraint that takes no parameters are told apart */
        return nil, false, withParamsErr.Error()
    }

    if true == internal.IsNilInterface(configured) {
        return nil, false, "constraint construction returned nil"
    }

    return configured, true, ""
}
