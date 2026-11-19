package config

import (
    "sort"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

const frameworkEnvironmentKeyOwner = "the framework"

/* retiredEnvironmentKeyMap is keyed by the retired spelling and holds the successor of every key the framework renamed */
var retiredEnvironmentKeyMap = map[string]string{
    "MELODY_HTTP_SESSION_PERSISTENCE_TIMEOUT": HttpSessionStorageTimeoutKey,
    "MELODY_PUBLIC_DIR":                       PublicDirectoryKey,
}

/* RetiredEnvironmentKeys answers, sorted by key, every retired spelling the .env artifacts set, the framework's and the declared ones; presence counts, an empty line included. */
func (instance *Configuration) RetiredEnvironmentKeys() []configcontract.RetiredEnvironmentKey {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    retiredKeys := make([]configcontract.RetiredEnvironmentKey, 0)
    if nil == instance.environment {
        return retiredKeys
    }

    for key := range instance.environment.values {
        if successor, isRetired := retiredEnvironmentKeyMap[key]; true == isRetired {
            retiredKeys = append(retiredKeys, configcontract.RetiredEnvironmentKey{Key: key, Successor: successor, Owner: frameworkEnvironmentKeyOwner})

            continue
        }

        if successor, isRetired := instance.declaredRetiredEnvironmentKeys[key]; true == isRetired {
            retiredKeys = append(retiredKeys, configcontract.RetiredEnvironmentKey{Key: key, Successor: successor, Owner: instance.declaredEnvironmentKeys[successor].owner})
        }
    }

    sort.Slice(retiredKeys, func(left int, right int) bool {
        return retiredKeys[left].Key < retiredKeys[right].Key
    })

    return retiredKeys
}

func (instance *Configuration) RetiredEnvironmentKeySuccessor(key string) (string, bool) {
    if successor, isRetired := retiredEnvironmentKeyMap[key]; true == isRetired {
        return successor, true
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    successor, isRetired := instance.declaredRetiredEnvironmentKeys[key]

    return successor, isRetired
}

func refuseRetiredEnvironmentKeyShape(key configcontract.EnvironmentKey, context exceptioncontract.Context) error {
    if _, isFrameworkRetired := retiredEnvironmentKeyMap[key.Key]; true == isFrameworkRetired {
        context["owner"] = frameworkEnvironmentKeyOwner

        return exception.NewError("a module declared an environment key another owner retired", context, nil)
    }

    seen := make(map[string]bool, len(key.Retired))
    for _, retiredKey := range key.Retired {
        context["retiredEnvironmentKey"] = retiredKey

        if false == moduleEnvironmentKeyPattern.MatchString(retiredKey) {
            return exception.NewError("a module's retired environment key is not MELODY_ followed by upper-case letters, digits and underscores", context, nil)
        }

        if retiredKey == key.Key {
            return exception.NewError("a module's environment key retires its own spelling", context, nil)
        }

        if retiredKey == key.Parameter {
            return exception.NewError("a module's environment key feeds a parameter named like a spelling it retires", context, nil)
        }

        if true == seen[retiredKey] {
            return exception.NewError("a module's environment key retires one spelling twice", context, nil)
        }
        seen[retiredKey] = true

        if _, isFrameworkKey := frameworkEnvironmentKeyIndex[retiredKey]; true == isFrameworkKey {
            context["owner"] = frameworkEnvironmentKeyOwner

            return exception.NewError("a module retired an environment key another owner holds", context, nil)
        }

        if _, isFrameworkRetired := retiredEnvironmentKeyMap[retiredKey]; true == isFrameworkRetired {
            context["owner"] = frameworkEnvironmentKeyOwner

            return exception.NewError("a module retired an environment key another owner already retired", context, nil)
        }
    }

    delete(context, "retiredEnvironmentKey")

    return nil
}

func (instance *Configuration) refuseRetiredEnvironmentKeyCollisionLocked(owner string, key configcontract.EnvironmentKey, context exceptioncontract.Context) error {
    if successor, isRetired := instance.declaredRetiredEnvironmentKeys[key.Key]; true == isRetired {
        return retiredEnvironmentKeyCollision(owner, instance.declaredEnvironmentKeys[successor].owner, "a module declared an environment key", "retired", context)
    }

    for _, retiredKey := range key.Retired {
        if declared, isDeclared := instance.declaredEnvironmentKeys[retiredKey]; true == isDeclared {
            context["retiredEnvironmentKey"] = retiredKey

            return retiredEnvironmentKeyCollision(owner, declared.owner, "a module retired an environment key", "holds", context)
        }

        if successor, isRetired := instance.declaredRetiredEnvironmentKeys[retiredKey]; true == isRetired {
            context["retiredEnvironmentKey"] = retiredKey

            return retiredEnvironmentKeyCollision(owner, instance.declaredEnvironmentKeys[successor].owner, "a module retired an environment key", "already retired", context)
        }
    }

    return nil
}

/* a collision with a key of the same module names no owner: there is no other module to look in */
func retiredEnvironmentKeyCollision(owner string, holder string, subject string, verb string, context exceptioncontract.Context) error {
    if holder == owner {
        return exception.NewError(subject+" it "+verb+" itself", context, nil)
    }

    context["owner"] = holder

    return exception.NewError(subject+" another owner "+verb, context, nil)
}
