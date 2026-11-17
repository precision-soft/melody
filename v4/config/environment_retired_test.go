package config

import (
    "os"
    "path/filepath"
    "slices"
    "testing"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
)

func retiringTextKey(retired ...string) configcontract.EnvironmentKey {
    key := textKey("fallback")
    key.Retired = retired

    return key
}

func TestRetiredEnvironmentKeys_FrameworkKeySet_AnswersKeyAndSuccessor(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_PUBLIC_DIR": "web"})

    retiredKeys := configuration.RetiredEnvironmentKeys()
    expected := []configcontract.RetiredEnvironmentKey{{Key: "MELODY_PUBLIC_DIR", Successor: PublicDirectoryKey, Owner: "the framework"}}
    if false == slices.Equal(expected, retiredKeys) {
        t.Fatalf("expected %v, got %v", expected, retiredKeys)
    }
}

func TestRetiredEnvironmentKeys_EmptyValue_StillAnswered(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_PUBLIC_DIR": ""})

    if retiredKeys := configuration.RetiredEnvironmentKeys(); 1 != len(retiredKeys) || "MELODY_PUBLIC_DIR" != retiredKeys[0].Key {
        t.Fatalf("expected the empty line answered, got %v", retiredKeys)
    }
}

func TestRetiredEnvironmentKeys_SuccessorAlone_AnswersNone(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{PublicDirectoryKey: "web", "MELODY_X_TEXT": "v"})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    if retiredKeys := configuration.RetiredEnvironmentKeys(); 0 != len(retiredKeys) {
        t.Fatalf("expected no retired key, got %v", retiredKeys)
    }
}

func TestRetiredEnvironmentKeys_SuccessorBesideTheRetiredKey_StillAnswered(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{PublicDirectoryKey: "web", "MELODY_PUBLIC_DIR": "web"})

    if retiredKeys := configuration.RetiredEnvironmentKeys(); 1 != len(retiredKeys) || "MELODY_PUBLIC_DIR" != retiredKeys[0].Key {
        t.Fatalf("expected the retired key answered beside its successor, got %v", retiredKeys)
    }
}

func TestRetiredEnvironmentKeys_DeclaredRetiredKeySet_AnswersOwner(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TXT": "v"})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    retiredKeys := configuration.RetiredEnvironmentKeys()
    expected := []configcontract.RetiredEnvironmentKey{{Key: "MELODY_X_TXT", Successor: "MELODY_X_TEXT", Owner: "x"}}
    if false == slices.Equal(expected, retiredKeys) {
        t.Fatalf("expected %v, got %v", expected, retiredKeys)
    }
}

func TestRetiredEnvironmentKeys_SortedByKey(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"MELODY_X_TXT": "", "MELODY_PUBLIC_DIR": "", "MELODY_A_TEXT": ""})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT", "MELODY_A_TEXT"))

    keys := make([]string, 0)
    for _, retiredKey := range configuration.RetiredEnvironmentKeys() {
        keys = append(keys, retiredKey.Key)
    }

    if false == slices.Equal([]string{"MELODY_A_TEXT", "MELODY_PUBLIC_DIR", "MELODY_X_TXT"}, keys) {
        t.Fatalf("expected the keys sorted, got %v", keys)
    }
}

func TestRetiredEnvironmentKeyMap_EverySuccessorIsAFrameworkKey_NoRetiredSpellingIs(t *testing.T) {
    if 0 == len(retiredEnvironmentKeyMap) {
        t.Fatalf("expected the framework's retired keys")
    }

    for retiredKey, successor := range retiredEnvironmentKeyMap {
        if _, isFrameworkKey := environmentKeyAliasMap[successor]; false == isFrameworkKey {
            t.Fatalf("expected the successor %s of %s a framework key", successor, retiredKey)
        }

        if _, isFrameworkKey := environmentKeyAliasMap[retiredKey]; true == isFrameworkKey {
            t.Fatalf("expected the retired spelling %s no framework key", retiredKey)
        }

        if false == moduleEnvironmentKeyPattern.MatchString(retiredKey) {
            t.Fatalf("expected the retired spelling %s in the MELODY_ namespace", retiredKey)
        }
    }
}

func TestDeclareEnvironmentKey_RetiredOutsideNamespace_Refused(t *testing.T) {
    for _, retiredKey := range []string{"APP_X_TEXT", "MELODY_x_text", "", "MELODY_X TEXT"} {
        configuration := mustConfigurationFromValues(t, map[string]string{})

        refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey(retiredKey)), "a module's retired environment key is not MELODY_ followed by upper-case letters, digits and underscores")
        if retiredKey != refusal.Context()["retiredEnvironmentKey"] || "MELODY_X_TEXT" != refusal.Context()["environmentKey"] {
            t.Fatalf("expected the spelling and the key named, got %v", refusal.Context())
        }
    }
}

func TestDeclareEnvironmentKey_RetiredEqualsKey_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey("MELODY_X_TEXT")), "a module's environment key retires its own spelling")
}

func TestDeclareEnvironmentKey_RetiredTwiceInOneDeclaration_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey("MELODY_X_TXT", "MELODY_X_TXT")), "a module's environment key retires one spelling twice")
}

func TestDeclareEnvironmentKey_RetiredIsAFrameworkKey_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey(PublicDirectoryKey)), "a module retired an environment key another owner holds")
    if "the framework" != refusal.Context()["owner"] {
        t.Fatalf("expected the framework named, got %v", refusal.Context())
    }

    refusal = declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey("MELODY_PUBLIC_DIR")), "a module retired an environment key another owner already retired")
    if "the framework" != refusal.Context()["owner"] {
        t.Fatalf("expected the framework named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_KeyTheFrameworkRetired_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    key := textKey("")
    key.Key = "MELODY_PUBLIC_DIR"

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a module declared an environment key another owner retired")
    if "the framework" != refusal.Context()["owner"] {
        t.Fatalf("expected the framework named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_RetiredHeldByAnotherDeclaration_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "y", countKey())

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey("MELODY_X_COUNT")), "a module retired an environment key another owner holds")
    if "y" != refusal.Context()["owner"] || "MELODY_X_COUNT" != refusal.Context()["retiredEnvironmentKey"] {
        t.Fatalf("expected the holder named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_KeyRetiredByAnotherDeclaration_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_COUNT"))

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("y", countKey()), "a module declared an environment key another owner retired")
    if "x" != refusal.Context()["owner"] {
        t.Fatalf("expected the retiring owner named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_RetiredTwiceByTwoOwners_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    key := countKey()
    key.Retired = []string{"MELODY_X_TXT"}

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("y", key), "a module retired an environment key another owner already retired")
    if "x" != refusal.Context()["owner"] || "MELODY_X_TXT" != refusal.Context()["retiredEnvironmentKey"] {
        t.Fatalf("expected the first owner named, got %v", refusal.Context())
    }

    if retiredKeys := retiredKeysWithLine(t, configuration, "MELODY_X_TXT"); "x" != retiredKeys[0].Owner {
        t.Fatalf("expected the refused declaration recorded nothing, got %v", retiredKeys)
    }
}

func retiredKeysWithLine(t *testing.T, configuration *Configuration, key string) []configcontract.RetiredEnvironmentKey {
    t.Helper()

    configuration.environment.values[key] = ""

    retiredKeys := configuration.RetiredEnvironmentKeys()
    if 1 != len(retiredKeys) {
        t.Fatalf("expected one retired key, got %v", retiredKeys)
    }

    return retiredKeys
}

func TestDeclareEnvironmentKey_RetiredEqualsParameter_Refused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    key := retiringTextKey("MELODY_X_TXT")
    key.Parameter = "MELODY_X_TXT"

    declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a module's environment key feeds a parameter named like a spelling it retires")
}

func TestDeclareEnvironmentKey_RetiredHeldBySameOwner_RefusedNamingNoOtherOwner(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", countKey())

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", retiringTextKey("MELODY_X_COUNT")), "a module retired an environment key it holds itself")
    if _, namesOwner := refusal.Context()["owner"]; true == namesOwner {
        t.Fatalf("expected no other owner named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_KeyRetiredBySameOwner_RefusedNamingNoOtherOwner(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_COUNT"))

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", countKey()), "a module declared an environment key it retired itself")
    if _, namesOwner := refusal.Context()["owner"]; true == namesOwner {
        t.Fatalf("expected no other owner named, got %v", refusal.Context())
    }
}

func TestDeclareEnvironmentKey_RetiredTwiceBySameOwner_RefusedNamingNoOtherOwner(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    key := countKey()
    key.Retired = []string{"MELODY_X_TXT"}

    refusal := declarationRefusal(t, configuration.DeclareEnvironmentKey("x", key), "a module retired an environment key it already retired itself")
    if _, namesOwner := refusal.Context()["owner"]; true == namesOwner || "MELODY_X_TXT" != refusal.Context()["retiredEnvironmentKey"] {
        t.Fatalf("expected the spelling and no other owner named, got %v", refusal.Context())
    }
}

func TestRetiredEnvironmentKeySuccessor_FrameworkAndDeclaredSpellings_AnsweredUnset(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    for retiredKey, expectedSuccessor := range map[string]string{"MELODY_PUBLIC_DIR": PublicDirectoryKey, "MELODY_X_TXT": "MELODY_X_TEXT"} {
        if successor, isRetired := configuration.RetiredEnvironmentKeySuccessor(retiredKey); false == isRetired || expectedSuccessor != successor {
            t.Fatalf("expected %s retired for %s, got %q %v", retiredKey, expectedSuccessor, successor, isRetired)
        }
    }

    for _, liveKey := range []string{PublicDirectoryKey, "MELODY_X_TEXT", "MELODY_X_UNKNOWN"} {
        if _, isRetired := configuration.RetiredEnvironmentKeySuccessor(liveKey); true == isRetired {
            t.Fatalf("expected %s not retired", liveKey)
        }
    }
}

func TestRetiredEnvironmentKeys_EnvironmentFiles_OnlyTheSelectedEnvironmentJudged(t *testing.T) {
    directory := t.TempDir()
    for name, content := range map[string]string{
        ".env":           "MELODY_ENV=dev\n",
        ".env.dev.local": "MELODY_PUBLIC_DIR=\n",
        ".env.prod":      "MELODY_X_TXT=text\n",
    } {
        if writeErr := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); nil != writeErr {
            t.Fatalf("unexpected write error: %v", writeErr)
        }
    }

    environment, environmentErr := NewEnvironment(NewEnvironmentSource(EnvironmentSourceConfig{FileSystem: os.DirFS(directory)}))
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, directory)
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }
    mustDeclare(t, configuration, "x", retiringTextKey("MELODY_X_TXT"))

    expected := []configcontract.RetiredEnvironmentKey{{Key: "MELODY_PUBLIC_DIR", Successor: PublicDirectoryKey, Owner: "the framework"}}
    if retiredKeys := configuration.RetiredEnvironmentKeys(); false == slices.Equal(expected, retiredKeys) {
        t.Fatalf("expected %v, got %v", expected, retiredKeys)
    }
}
