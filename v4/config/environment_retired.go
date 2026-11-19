package config

import (
    "sort"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const frameworkEnvironmentKeyOwner = internal.EnvironmentKeyOwnerFramework

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

func refuseRetiredEnvironmentKeyShape(owner string, key configcontract.EnvironmentKey, context exceptioncontract.Context) error {
    if _, isFrameworkRetired := retiredEnvironmentKeyMap[key.Key]; true == isFrameworkRetired {
        context["holder"] = frameworkEnvironmentKeyOwner

        return exception.NewError("an environment key is declared that another owner retired", context, nil)
    }

    seen := make(map[string]bool, len(key.Retired))
    for _, retiredKey := range key.Retired {
        context["retiredEnvironmentKey"] = retiredKey

        /* the application may retire a MELODY_ spelling, so a deployment renames a framework-era key to the application's own */
        if applicationEnvironmentKeyOwner == owner {
            if false == applicationEnvironmentKeyPattern.MatchString(retiredKey) {
                return exception.NewError("a declared environment key's retired spelling is not an upper-case letter followed by upper-case letters, digits and underscores", context, nil)
            }
        } else if false == moduleEnvironmentKeyPattern.MatchString(retiredKey) {
            return exception.NewError("a module's retired environment key is not MELODY_ followed by upper-case letters, digits and underscores", context, nil)
        }

        if retiredKey == key.Key {
            return exception.NewError("a declared environment key retires its own spelling", context, nil)
        }

        if retiredKey == key.Parameter {
            return exception.NewError("a declared environment key feeds a parameter named like a spelling it retires", context, nil)
        }

        if true == seen[retiredKey] {
            return exception.NewError("a declared environment key retires one spelling twice", context, nil)
        }
        seen[retiredKey] = true

        if _, isFrameworkKey := frameworkEnvironmentKeyIndex[retiredKey]; true == isFrameworkKey {
            context["holder"] = frameworkEnvironmentKeyOwner

            return exception.NewError("an environment key is retired that another owner holds", context, nil)
        }

        if _, isFrameworkRetired := retiredEnvironmentKeyMap[retiredKey]; true == isFrameworkRetired {
            context["holder"] = frameworkEnvironmentKeyOwner

            return exception.NewError("an environment key is retired that another owner already retired", context, nil)
        }
    }

    delete(context, "retiredEnvironmentKey")

    return nil
}

func (instance *Configuration) refuseRetiredEnvironmentKeyCollisionLocked(owner string, key configcontract.EnvironmentKey, context exceptioncontract.Context) error {
    if successor, isRetired := instance.declaredRetiredEnvironmentKeys[key.Key]; true == isRetired {
        return retiredEnvironmentKeyCollision(owner, instance.declaredEnvironmentKeys[successor].owner, "an environment key is declared", "retired", context)
    }

    for _, retiredKey := range key.Retired {
        if declared, isDeclared := instance.declaredEnvironmentKeys[retiredKey]; true == isDeclared {
            context["retiredEnvironmentKey"] = retiredKey

            return retiredEnvironmentKeyCollision(owner, declared.owner, "an environment key is retired", "holds", context)
        }

        if successor, isRetired := instance.declaredRetiredEnvironmentKeys[retiredKey]; true == isRetired {
            context["retiredEnvironmentKey"] = retiredKey

            return retiredEnvironmentKeyCollision(owner, instance.declaredEnvironmentKeys[successor].owner, "an environment key is retired", "already retired", context)
        }
    }

    return nil
}

/* a collision with a key of the same owner names no holder: there is no other owner to look in */
func retiredEnvironmentKeyCollision(owner string, holder string, subject string, verb string, context exceptioncontract.Context) error {
    if holder == owner {
        return exception.NewError(subject+" that the same owner "+verb, context, nil)
    }

    context["holder"] = holder

    return exception.NewError(subject+" that another owner "+verb, context, nil)
}
