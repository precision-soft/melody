package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

/* the key-id↔app binding is only sound when each app's secret material is distinct, so the provider refuses the same secret bytes under key ids belonging to different apps — otherwise a holder could sign under a sibling app's key id and defeat the binding. */
func TestStaticHmacSecretProvider_RejectsSecretReusedAcrossApps(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatal("expected a panic when the same secret is registered for two different apps")
        }
    }()

    shared := []byte("shared-secret-value-across-apps-1")
    NewStaticHmacSecretProvider("key-a", map[string]HmacKey{
        "key-a": {App: "app-a", Secret: shared},
        "key-b": {App: "app-b", Secret: shared},
    })
}

/* positive control: the same app may legitimately own several key ids (rotation overlap), even reusing material is fine within one app — only cross-app reuse is rejected. */
func TestStaticHmacSecretProvider_AllowsMultipleKeysForOneApp(t *testing.T) {
    provider := NewStaticHmacSecretProvider("key-current", map[string]HmacKey{
        "key-current":  {App: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
        "key-previous": {App: "wms-service", Secret: []byte("previous-shared-secret-value-002")},
    })

    if app, bound := provider.AppForKeyId("key-previous"); false == bound || "wms-service" != app {
        t.Fatalf("expected key-previous bound to wms-service, got %q bound=%v", app, bound)
    }
}

func TestNewStaticHmacSecretProvider_RefusesAConfigurationThatCanNotSign(t *testing.T) {
    validKeys := map[string]HmacKey{"key-a": {App: "app-a", Secret: []byte("secret-value-of-thirty-two-bytes")}}

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("", validKeys)
    }, "hmac current key id is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("key-a", nil)
    }, "hmac secrets are empty")

    /* the current key is what every outgoing envelope is signed with: naming one that resolves to no secret builds a provider that cannot sign at all, and the failure would otherwise surface at the first request rather than at boot */
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("key-absent", validKeys)
    }, "hmac current key id has no secret")
}

func TestNewStaticHmacSecretProvider_RefusesAKeyThatCanNotIdentifyItsOwner(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("key-a", map[string]HmacKey{"": {App: "app-a", Secret: []byte("secret-value-of-thirty-two-bytes")}})
    }, "hmac key id is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("key-a", map[string]HmacKey{"key-a": {App: "", Secret: []byte("secret-value-of-thirty-two-bytes")}})
    }, "hmac key has no app")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacSecretProvider("key-a", map[string]HmacKey{"key-a": {App: "app-a", Secret: nil}})
    }, "hmac secret is empty")
}

func TestStaticHmacSecretProvider_AnswersFalseForAnUnknownKeyId(t *testing.T) {
    provider := NewStaticHmacSecretProvider("key-a", map[string]HmacKey{"key-a": {App: "app-a", Secret: []byte("secret-value-of-thirty-two-bytes")}})

    if "key-a" != provider.CurrentKeyId() {
        t.Fatalf("expected the current key id to be reported, got %q", provider.CurrentKeyId())
    }

    if secret, exists := provider.Secret("key-absent"); true == exists || nil != secret {
        t.Fatalf("expected an unknown key id to resolve to no secret, got %v", secret)
    }

    if app, exists := provider.AppForKeyId("key-absent"); true == exists || "" != app {
        t.Fatalf("expected an unknown key id to be bound to no app, got %q", app)
    }
}

/* the secret is the whole credential: a caller that kept the slice it handed in, or that mutates the slice handed back, rewrites what every envelope of that app verifies against */
func TestStaticHmacSecretProvider_OwnsItsSecretBytes(t *testing.T) {
    callerSecret := []byte("secret-value-of-thirty-two-bytes")
    provider := NewStaticHmacSecretProvider("key-a", map[string]HmacKey{"key-a": {App: "app-a", Secret: callerSecret}})

    callerSecret[0] = 'X'

    secret, _ := provider.Secret("key-a")
    if 's' != secret[0] {
        t.Fatalf("expected the provider to keep its own copy, got %q", string(secret))
    }

    secret[0] = 'X'

    readAgain, _ := provider.Secret("key-a")
    if 's' != readAgain[0] {
        t.Fatalf("expected the provider to hand out a copy, got %q", string(readAgain))
    }
}
