package validation

import (
    "encoding/json"
    "fmt"
    "reflect"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    validationcontract "github.com/precision-soft/melody/validation/contract"
)

/* maxNestedValidationDepth bounds the recursive descent into nested struct/slice/map/embedded values so a self-referential or deeply cyclic payload cannot overflow the stack; the visited-pointer set below short-circuits genuine reference cycles, and this depth cap is the belt-and-suspenders backstop for value cycles the pointer set cannot observe. Reaching the cap yields a validation error, never a silent pass: the tags below it were never enforced, so reporting the payload valid would let a caller nest past the cap to bypass validation outright. */
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

    /* constructedConstraints memoizes the constraint a parameterized rule resolves to, keyed by rule name and parameters: a rule is otherwise rebuilt for every value it reaches, so a regex tag recompiled its pattern once per element of an array. A registered name never changes constraint (a second registration panics), so an entry can never go stale. */
    constructedConstraints sync.Map
}

type constructedConstraint struct {
    constraint validationcontract.Constraint
    ok         bool
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

    if false == value.IsValid() {
        return errors
    }

    /* the cut runs ahead of the kind switch below because value.Type() is unreadable on an invalid value, which puts it ahead of the switch's nil guards too: a member the payload sent as null or empty — a nil pointer or interface, a slice or map holding no element — holds nothing the cut could have truncated, the switch returning no errors for it at any depth, so reporting one would reject a payload over a member that was never walked */
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

func holdsNoValidationMember(value reflect.Value) bool {
    switch value.Kind() {
    case reflect.Ptr, reflect.Interface:
        return value.IsNil()
    case reflect.Slice, reflect.Map:
        /* the length, not the nil header: an empty slice or map truncates exactly as little as a null one, and the walkers below iterate neither */
        return 0 == value.Len()
    default:
        return false
    }
}

/* nestedTagBearingTypeCache memoizes typeCanCarryValidationTag: the answer is a property of the type alone, and the set of types a program validates is fixed when it is compiled. */
var nestedTagBearingTypeCache sync.Map

/* typeCanCarryValidationTag reports whether a validate tag is declared anywhere at or below targetType, which is what lets the depth cut distinguish a truncated subtree whose constraints went unenforced from one that never had any. An interface member counts as tag-bearing, its dynamic type being unknowable from the type alone: the walk descends into whatever such a member holds and enforces the tags it finds there, so answering "no tag" for it would let a caller nest past the cap to bypass those tags outright — the nesting depth is plain client-controlled json whoever fills the member. The cut therefore fails closed, at the price of reporting a tag-free subtree reached through an interface past the cap, free-form json among it; the other direction is a silent pass, which is what the cap exists to prevent. Filling such a member with a tagged struct takes a custom UnmarshalJSON dispatching on a discriminator, or server-side construction, so the payloads the closed direction costs anything are the tag-free ones nested past 64 levels. */
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

/* validateStruct validates the fields of one json object: the struct's own fields plus everything its embeds promote, resolved with encoding/json's dominance rules — the shallowest field wins a name, an explicit json tag beats an untagged field at equal depth, and an ambiguity drops the name entirely. Only the winners are validated, because only the winners are populated from a payload: validating a shadowed promoted field would run its tag against a permanent zero value and reject every request. */
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
                    /* the embed's own tag runs against the embed value: its promoted fields are payload-populated, so a constraint declared on the embed is satisfiable and must not vanish with the flattening. An unexported embed's value cannot pass through Interface — only its promoted exported fields can — so its tag stays out of reach. */
                    if true == field.IsExported() {
                        errors = append(errors, instance.applyFieldRules(field, fieldValue, embeddedFieldPath(field, path))...)
                    }

                    embeddedType := dereferencedValidationStructType(field.Type)
                    if true == embeddedSeen[embeddedType] {
                        continue
                    }

                    /* a type reached by several equal-depth paths has its fields duplicated per path so a diamond annihilates in the dominance pick, as in encoding/json; two copies decide every tie, so the count is capped there. The count is how many times this level reaches the type, one increment per embed occurrence, exactly as encoding/json's typeFields counts it — adding the parent's own count instead would cascade an ancestor diamond onto every type below it and annihilate a name that a payload does populate. */
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

/* isPromotedValidationEmbed matches encoding/json's flattening: an anonymous struct (or pointer to one) without an explicit json name flattens onto its parent object, and a json-named embed is an ordinary field holding a nested object. An embedded time.Time flattens like any other embed and contributes no name of its own, all of its fields being unexported; the shape where it owns the whole body instead is the promoted codec, which promotesValidationTimeCodec settles before this walk begins. */
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

/* promotesValidationTimeCodec reports a struct that a payload can only ever spell as an RFC 3339 string: such a body is handed whole to the embedded time and populates nothing else in the struct, so no constraint declared there can be satisfied by any payload and enforcing one would reject every body the type is able to decode, which is the reason a `json:"-"` field is left alone as well. Both halves are asked, because they are different questions: what the wire form looks like is decided by the promoted marshaler, while what a body populates is decided by the unmarshaler, and a struct can promote the one while declaring the other. */
func promotesValidationTimeCodec(structType reflect.Type) bool {
    if cached, exists := validationTimeCodecCache.Load(structType); true == exists {
        return cached.(bool)
    }

    promotes := promotesValidationTimeEncoding(structType) && refusesValidationObjectBody(structType)

    validationTimeCodecCache.Store(structType, promotes)

    return promotes
}

/* promotesValidationTimeEncoding reports a struct whose promoted json codec is time.Time's, which is the encoding half alone. */
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

/* refusesValidationObjectBody puts the decode half to encoding/json itself, reflect being unable to answer it: a promoted UnmarshalJSON and one the struct declares are the same entry of the same method set — same name, same signature, same func pointer — so a struct whose body must be an RFC 3339 string cannot be told apart from one whose own decoder accepts an object and fills the very fields the constraints sit on. An empty object is offered to a throwaway value of the type instead: a decoder that refuses it leaves no sibling that any body could populate, while one that accepts it can populate a sibling, so the constraints stay enforced. A panicking decoder counts as accepting, the direction that keeps them enforced. A decoder that refuses the empty object and accepts a populated one is skipped like the time codec it looks like. */
func refusesValidationObjectBody(structType reflect.Type) (refuses bool) {
    defer func() {
        if recovered := recover(); nil != recovered {
            refuses = false
        }
    }()

    return nil != json.Unmarshal([]byte("{}"), reflect.New(structType).Interface())
}

/* promotedValidationMarshalerOrigin reports which type declares the MarshalJSON in structType's value method set, and at what embedding depth, by the selector rule the method set is built by: the candidate at the shallowest depth wins, two candidates tied at that depth promote nothing, and a non-struct embed is a candidate like any other. Unresolved (false) leaves the caller walking the struct, which is the direction that keeps constraints enforced: an embed whose codec has a pointer receiver, and a cycle reached through a pointer embed. A MarshalJSON declared on structType itself while an embed also carries one is out of reach, reflect.Method carrying no declaring type, and resolves to the embed. */
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

            /* the promoted codec has a pointer receiver, so which type declares it cannot be followed through the embed. Outcome-neutral under the origin rule as it stands — a type that does not implement the interface can neither be time.Time nor promote its codec at a single shallowest depth, so falling through would reach the same walk — and kept as the guard for a rule that stops being true. TestPromotedValidationMarshalerOrigin_APointerEmbedWithAPointerReceiverCodecIsUnresolved pins it at the only level it is observable, this function's own return. */
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

/* dereferencedValidationStructValue unwraps a (possibly pointer) embed value; a nil pointer stands for "nothing was supplied", so it yields the zero embed and its promoted fields are validated against their zero values exactly as a value embed's are — anything else would skip every constraint the embed promotes whenever a payload names none of its fields. */
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
                "params": copyValidationRuleParams(rule.params),
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

func (instance *Validator) createConstraintWithParams(name string, params map[string]string) (validationcontract.Constraint, bool) {
    instance.mutex.RLock()
    constraint := instance.constraints[name]
    instance.mutex.RUnlock()

    if 0 == len(params) {
        return constraint, true
    }

    cacheKey := constraintCacheKey(name, params)

    if cached, exists := instance.constructedConstraints.Load(cacheKey); true == exists {
        constructed := cached.(constructedConstraint)

        return constructed.constraint, constructed.ok
    }

    configured, configuredOk := buildConstraintWithParams(constraint, params)

    /* LoadOrStore rather than Store so a concurrent first touch settles on ONE constraint: the contract lets the winner be shared for the process lifetime, and a losing caller validating against its own copy would be an unadvertised second instance. A rejected parameter set is cached alongside the accepted ones — WithParams is a pure function of its parameters by contract, so the outcome cannot change, and leaving failures out would re-attempt the construction for every value a permanently invalid tag reaches while freezing the successes. */
    stored, _ := instance.constructedConstraints.LoadOrStore(cacheKey, constructedConstraint{constraint: configured, ok: configuredOk})
    constructed := stored.(constructedConstraint)

    return constructed.constraint, constructed.ok
}

func buildConstraintWithParams(constraint validationcontract.Constraint, params map[string]string) (validationcontract.Constraint, bool) {
    parameterized, ok := constraint.(validationcontract.ParameterizedConstraint)
    if false == ok {
        /* @important fail closed when the tag carries parameters the registered constraint cannot consume: validating with the unparameterized singleton would silently enforce a different configuration than the tag declares (a custom `between(min=1,max=5)` would validate with whatever the singleton was built with) */
        return nil, false
    }

    /* the parameter map belongs to the memoized parse of the tag, so a constraint that kept or mutated it would poison every later lookup */
    /* @info the copy is the barrier between the process-wide memo and userland: the cached rules share this map's identity, so a constraint that retained or mutated what WithParams received would poison every later validation */
    configured, withParamsErr := parameterized.WithParams(copyValidationRuleParams(params))
    if nil != withParamsErr {
        return nil, false
    }

    if true == internal.IsNilInterface(configured) {
        return nil, false
    }

    return configured, true
}
