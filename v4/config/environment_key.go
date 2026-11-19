package config

import (
    "errors"
    "fmt"
    "regexp"
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const environmentKeyNamespace = "MELODY_"

/* a module stays in the namespace, so a process that does not register it still warns about and redacts its keys */
var moduleEnvironmentKeyPattern = regexp.MustCompile(`^MELODY_[A-Z0-9_]+$`)

var applicationEnvironmentKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

const applicationEnvironmentKeyOwner = internal.EnvironmentKeyOwnerApplication

/* in slice order, before the first resolve, so a key's default may read a key declared before it and a framework parameter read at construction sees every default */
func (instance *Configuration) declareApplicationEnvironmentKeys(keys []configcontract.EnvironmentKey) error {
    for _, key := range keys {
        if declareErr := instance.DeclareEnvironmentKey(applicationEnvironmentKeyOwner, key); nil != declareErr {
            return declareErr
        }
    }

    return nil
}

type declaredEnvironmentKey struct {
    owner       string
    declaration configcontract.EnvironmentKey
}

/* DeclareEnvironmentKey enters an owner's key into the key table and resolves its parameter at once; only a reference to a parameter registered later waits for the boot pass. Refused once the boot resolve ran, for a module's key outside the MELODY_ namespace, for the application's key inside it or not upper-case letters, digits and underscores, for a key held or retired by another owner, for a retired spelling of the wrong shape or held by another owner, for a parameter already registered or fed, for a secret key with a default, for a literal default its own rule refuses and for a text that cannot be resolved. */
func (instance *Configuration) DeclareEnvironmentKey(owner string, key configcontract.EnvironmentKey) error {
    context := exceptioncontract.Context{
        "owner":          owner,
        "environmentKey": key.Key,
        "field":          key.Field,
        "parameterName":  key.Parameter,
    }

    if shapeErr := refuseEnvironmentKeyShape(owner, key, context); nil != shapeErr {
        return shapeErr
    }

    if retiredShapeErr := refuseRetiredEnvironmentKeyShape(owner, key, context); nil != retiredShapeErr {
        return retiredShapeErr
    }

    if _, isFrameworkKey := frameworkEnvironmentKeyIndex[key.Key]; true == isFrameworkKey {
        context["holder"] = frameworkEnvironmentKeyOwner

        return exception.NewError("an environment key is declared that another owner already holds", context, nil)
    }

    if true == key.Secret && "" != key.Default {
        return exception.NewError("a default is declared for a secret environment key", context, nil)
    }

    if false == internal.IsNilInterface(key.Rule) && false == templateCarriesConstruct(key.Default) {
        if ruleErr := key.Rule.Check(key.Default, key.Field); nil != ruleErr {
            return exception.NewError("a declared environment key's default is refused by its own rule", context, ruleErr)
        }
    }

    /* a rule is its owner's code, so it runs outside the lock and may read the configuration */
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.resolved || true == instance.serving.Load() {
        return exception.NewError("an environment key is declared after the configuration resolved", context, nil)
    }

    if declared, isDeclared := instance.declaredEnvironmentKeys[key.Key]; true == isDeclared {
        if owner == declared.owner {
            return exception.NewError("an environment key is declared that the same owner already holds", context, nil)
        }

        context["holder"] = declared.owner

        return exception.NewError("an environment key is declared that another owner already holds", context, nil)
    }

    if retiredCollisionErr := instance.refuseRetiredEnvironmentKeyCollisionLocked(owner, key, context); nil != retiredCollisionErr {
        return retiredCollisionErr
    }

    if feedingKey, isFed := instance.declaredEnvironmentParameters[key.Parameter]; true == isFed {
        context["holder"] = instance.declaredEnvironmentKeys[feedingKey].owner
        context["feedingEnvironmentKey"] = feedingKey

        return exception.NewError("a declared environment key feeds a parameter another environment key already feeds", context, nil)
    }

    if nil != instance.getInternalParameter(key.Parameter) {
        return exception.NewError("the parameter a declared environment key feeds is already registered", context, nil)
    }

    loadedParameter := instance.getInternalParameter(key.Key)
    if nil != loadedParameter && key.Key != loadedParameter.environmentKey {
        return exception.NewError("a parameter under the name of a declared environment key is already registered", context, nil)
    }

    instance.recordEnvironmentKeyDeclarationLocked(owner, key)

    /* under a rule a blank line is unset */
    line, isSet := "", false
    if nil != instance.environment {
        line, isSet = instance.environment.Get(key.Key)
    }
    speaksDefault := false == isSet || (false == internal.IsNilInterface(key.Rule) && "" == strings.TrimSpace(line))

    keyParameter := loadedParameter
    if nil == keyParameter {
        keyParameter = NewParameter(key.Key, environmentPlaceholder(key.Key), nil, speaksDefault)
    } else {
        /* declared, it reads the key, so a blank line under a rule reaches the default */
        keyParameter.template = environmentPlaceholder(key.Key)
        keyParameter.isDefault = speaksDefault
    }

    /* an override held since construction becomes the parameter's template; the key's name keeps reading the key */
    override, isOverridden := instance.pendingParameterOverrides[key.Parameter]

    parameter := keyParameter
    resolutions := []environmentKeyResolution{{parameter: keyParameter, template: environmentPlaceholder(key.Key), name: key.Parameter}}
    if true == isOverridden {
        parameter = newOverriddenParameter(key.Key, key.Parameter, override)
        resolutions[0].name = key.Key
        resolutions = append(resolutions, environmentKeyResolution{parameter: parameter, template: override, name: key.Parameter})
    }

    /* registered before the resolution, so a secret it reads marks it */
    instance.parameters[key.Key] = keyParameter
    instance.parameters[key.Parameter] = parameter

    /* every text is resolved before any value is stored, so a refusal leaves the tables as they were */
    for index := range resolutions {
        resolution := &resolutions[index]
        resolution.value, resolution.err = instance.resolveTemplate(
            resolution.template,
            resolution.name,
            make(map[string]bool),
            make(map[string]bool),
        )
        if nil != resolution.err && false == errors.Is(resolution.err, errUndefinedParameterReference) && false == errors.Is(resolution.err, errUndefinedEnvironmentKey) {
            instance.forgetEnvironmentKeyDeclarationLocked(key)

            delete(instance.parameters, key.Parameter)
            if nil == loadedParameter {
                delete(instance.parameters, key.Key)
            } else {
                loadedParameter.template = line
                loadedParameter.isDefault = false
                instance.parameters[key.Key] = loadedParameter
            }

            if true == resolution.parameter.overridden {
                context["overridden"] = true
            }

            return exception.NewError("a declared environment key could not be resolved at its declaration", context, resolution.err)
        }
    }

    for _, resolution := range resolutions {
        if nil == resolution.err {
            resolution.parameter.storeValue(resolution.value)
            resolution.parameter.deferred.Store(false)
        } else {
            /* a parameter registered later settles it at the boot pass */
            resolution.parameter.deferred.Store(true)
        }
    }

    if true == isOverridden {
        delete(instance.pendingParameterOverrides, key.Parameter)
    }

    if true == key.Secret {
        if nil == instance.secretEnvironmentKeys {
            instance.secretEnvironmentKeys = make(map[string]bool)
        }

        instance.secretEnvironmentKeys[key.Key] = true
        keyParameter.isSecret.Store(true)
        instance.propagateSecretMarkLocked(key.Key)

        /* the mark says what the parameter carries, so an override from another source stays redacted */
        if true == isOverridden {
            parameter.isSecret.Store(true)
            instance.propagateSecretMarkLocked(key.Parameter)
        }
    }

    return nil
}

type environmentKeyResolution struct {
    parameter *Parameter
    template  string
    name      string
    value     string
    err       error
}

func (instance *Configuration) recordEnvironmentKeyDeclarationLocked(owner string, key configcontract.EnvironmentKey) {
    if nil == instance.declaredEnvironmentKeys {
        instance.declaredEnvironmentKeys = make(map[string]declaredEnvironmentKey)
        instance.declaredEnvironmentParameters = make(map[string]string)
        instance.declaredRetiredEnvironmentKeys = make(map[string]string)
        instance.secretEnvironmentKeys = make(map[string]bool)
    }

    instance.declaredEnvironmentKeys[key.Key] = declaredEnvironmentKey{owner: owner, declaration: key}
    instance.declaredEnvironmentParameters[key.Parameter] = key.Key

    for _, retiredKey := range key.Retired {
        instance.declaredRetiredEnvironmentKeys[retiredKey] = key.Key
    }
}

func (instance *Configuration) forgetEnvironmentKeyDeclarationLocked(key configcontract.EnvironmentKey) {
    delete(instance.declaredEnvironmentKeys, key.Key)
    delete(instance.declaredEnvironmentParameters, key.Parameter)

    for _, retiredKey := range key.Retired {
        delete(instance.declaredRetiredEnvironmentKeys, retiredKey)
    }
}

func refuseEnvironmentKeyShape(owner string, key configcontract.EnvironmentKey, context exceptioncontract.Context) error {
    if applicationEnvironmentKeyOwner == owner {
        if true == strings.HasPrefix(key.Key, environmentKeyNamespace) {
            return exception.NewError("an application's environment key is in the framework's MELODY_ namespace", context, nil)
        }

        if false == applicationEnvironmentKeyPattern.MatchString(key.Key) {
            return exception.NewError("a declared environment key is not an upper-case letter followed by upper-case letters, digits and underscores", context, nil)
        }
    } else if false == moduleEnvironmentKeyPattern.MatchString(key.Key) {
        return exception.NewError("a module's environment key is not MELODY_ followed by upper-case letters, digits and underscores", context, nil)
    }

    if "" == strings.TrimSpace(key.Parameter) || key.Parameter != strings.TrimSpace(key.Parameter) {
        return exception.NewError("a declared environment key names an empty or padded parameter", context, nil)
    }

    if true == strings.HasPrefix(key.Parameter, "kernel.") {
        return exception.NewError("a declared environment key feeds a parameter in the reserved kernel. namespace", context, nil)
    }

    if key.Parameter == key.Key {
        return exception.NewError("a declared environment key feeds a parameter of its own name", context, nil)
    }

    if "" == strings.TrimSpace(key.Field) || key.Field != strings.TrimSpace(key.Field) {
        return exception.NewError("a declared environment key names an empty or padded field", context, nil)
    }

    return nil
}

/* CheckEnvironmentKeys judges every declared key's resolved value against its rule, in key order, and answers the first refusal as the framework refuses its own keys: "invalid environment value" naming the key, its owner, the field and the parameter, the rule's error in the chain, withheld for a secret key since it quotes the value. */
func (instance *Configuration) CheckEnvironmentKeys() error {
    instance.mutex.RLock()

    keys := make([]string, 0, len(instance.declaredEnvironmentKeys))
    for key := range instance.declaredEnvironmentKeys {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    declarations := make([]declaredEnvironmentKey, 0, len(keys))
    parameters := make([]*Parameter, 0, len(keys))
    for _, key := range keys {
        declared := instance.declaredEnvironmentKeys[key]
        declarations = append(declarations, declared)
        parameters = append(parameters, instance.getInternalParameter(declared.declaration.Parameter))
    }

    instance.mutex.RUnlock()

    /* the rules run outside the lock, since a rule is its owner's code and may read the configuration */
    for index, key := range keys {
        declared := declarations[index]
        if true == internal.IsNilInterface(declared.declaration.Rule) {
            continue
        }

        parameter := parameters[index]

        text := ""
        if value := parameter.loadValue(); nil != value {
            if stringValue, isString := value.(string); true == isString {
                text = stringValue
            } else {
                text = fmt.Sprint(value)
            }
        }

        ruleErr := declared.declaration.Rule.Check(text, declared.declaration.Field)
        if nil == ruleErr {
            continue
        }

        if true == parameter.isSecret.Load() {
            ruleErr = nil
        }

        refusalContext := exceptioncontract.Context{
            "environmentKey": key,
            "owner":          declared.owner,
            "field":          declared.declaration.Field,
            "parameterName":  declared.declaration.Parameter,
        }

        if true == parameter.overridden {
            refusalContext["overridden"] = true
        }

        return exception.NewError("invalid environment value", refusalContext, ruleErr)
    }

    return nil
}

/* UndeclaredEnvironmentKeys answers, sorted, every key the .env artifacts loaded in the MELODY_ namespace that is neither the framework's nor declared */
func (instance *Configuration) UndeclaredEnvironmentKeys() []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    keys := make([]string, 0)
    if nil == instance.environment {
        return keys
    }

    for key := range instance.environment.values {
        if false == strings.HasPrefix(key, environmentKeyNamespace) {
            continue
        }

        if _, isFrameworkKey := frameworkEnvironmentKeyIndex[key]; true == isFrameworkKey {
            continue
        }

        if _, isDeclared := instance.declaredEnvironmentKeys[key]; true == isDeclared {
            continue
        }

        keys = append(keys, key)
    }

    sort.Strings(keys)

    return keys
}

var _ configcontract.EnvironmentKeyDeclarer = (*Configuration)(nil)
