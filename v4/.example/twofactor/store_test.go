package twofactor

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v4/encrypt"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func renderedEnrollmentUpsert(t *testing.T) string {
    t.Helper()

    store := &Store{database: newRenderingDatabase()}

    return enrollmentUpsert(store.database, &Enrollment{
        UserIdentifier: "user-2",
        Secret:         melodyencrypt.EncryptedStringFor[SecretColumn]("zz-secret"),
        RecoveryCodes:  melodyencrypt.EncryptedStringFor[RecoveryCodesColumn](`["zz-one"]`),
        CreatedAt:      time.Unix(1, 0),
    }).String()
}

func TestPendingEnrollmentUpsertReplacesThePendingOne(t *testing.T) {
    rendered := enrollmentUpsert(newRenderingDatabase(), &PendingEnrollment{UserIdentifier: "user-2", CreatedAt: time.Unix(1, 0)}).String()

    if false == strings.Contains(rendered, "melody_example_v4_two_factor_pending") || false == strings.Contains(rendered, " ON DUPLICATE KEY UPDATE secret = VALUES(secret)") {
        t.Fatalf("expected the pending table replaced in place, got %q", rendered)
    }
}

func TestEnrollmentUpsertReplacesAnEnrollmentThatIsAlreadyThere(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    if false == strings.Contains(rendered, " ON DUPLICATE KEY UPDATE secret = VALUES(secret)") {
        t.Fatalf(
            "expected a second enrollment to replace the first rather than collide with the primary key, in the form mysql accepts; got %q",
            rendered,
        )
    }

    /* the form bun renders for a dialect without the feature — mysql refuses it */
    if true == strings.Contains(rendered, "UPDATE SET") {
        t.Fatalf("expected the mysql upsert form, got the generic one mysql refuses: %q", rendered)
    }
}

func TestEnrollmentUpsertReplacesTheRecoveryCodesWithTheSecret(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    for _, column := range []string{"secret = VALUES(secret)", "recovery_codes = VALUES(recovery_codes)", "created_at = VALUES(created_at)"} {
        if false == strings.Contains(rendered, column) {
            t.Fatalf("expected the replacement to write %s, got %q", column, rendered)
        }
    }
}

func TestEnrollmentDeleteIsKeyedOnTheAccountIdentifierAlone(t *testing.T) {
    rendered := enrollmentDelete(newRenderingDatabase(), "user-4").String()

    if false == strings.HasPrefix(rendered, "DELETE FROM `melody_example_v4_two_factor`") || false == strings.Contains(rendered, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", rendered)
    }
}

func TestStoreFromRuntime_ResolvesTheRegisteredStoreAndRefusesAnAbsentOne(t *testing.T) {
    published := NewStore(nil)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        ServiceStore,
        func(resolver melodycontainercontract.Resolver) (*Store, error) {
            return published, nil
        },
        melodycontainer.RegisterOptions{},
    )

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    store, storeErr := StoreFromRuntime(runtimeInstance)
    if nil != storeErr || published != store {
        t.Fatalf("expected the published store, got %p, %v", store, storeErr)
    }

    emptyContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    emptyRuntime := melodyruntime.NewRequest(context.Background(), emptyContainer.NewScope())
    store, storeErr = StoreFromRuntime(emptyRuntime)
    if nil == storeErr || nil != store {
        t.Fatalf("expected an absent store to be refused, got %p, %v", store, storeErr)
    }
}

func TestRecoveryCodeMatches_FoldsCaseAndDropsTheSeparators(t *testing.T) {
    for _, presented := range []string{"abcde-12345", "ABCDE-12345", "abcde12345", "AbCdE 12345", " abcde-12345 "} {
        if false == recoveryCodeMatches("abcde-12345", presented) {
            t.Fatalf("expected %q to match the minted code", presented)
        }
    }

    for _, presented := range []string{"abcde-12346", "abcde-1234", "", "abcdf-12345"} {
        if true == recoveryCodeMatches("abcde-12345", presented) {
            t.Fatalf("expected %q refused", presented)
        }
    }

    if false == recoveryCodeMatches("ABCDE-12345", "abcde12345") {
        t.Fatalf("expected a hand-edited stored code compared in its canonical form")
    }
}

func TestEnrollment_SecretAndRecoveryCodesAreBoundToTheirColumns(t *testing.T) {
    cipher := melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider(melodyencrypt.KeyRing{
        CurrentKeyId: "two-factor-store-test",
        Keys:         map[string][]byte{"two-factor-store-test": []byte("two-factor-store-test-key-32byte")},
    }))
    melodyencrypt.NewModule(melodyencrypt.ModuleConfig{Cipher: cipher, SealBindings: SealBindings()}).RegisterServices(
        moduleRegistrar{Container: melodycontainer.NewContainer(melodycontainer.ContainerConfig{})},
    )

    sealedSecret, secretErr := melodyencrypt.EncryptedStringFor[SecretColumn]("zz-secret").Value()
    if nil != secretErr {
        t.Fatalf("sealing the secret failed: %v", secretErr)
    }

    sealedCodes, codesErr := melodyencrypt.EncryptedStringFor[RecoveryCodesColumn](`["zz-one"]`).Value()
    if nil != codesErr {
        t.Fatalf("sealing the recovery codes failed: %v", codesErr)
    }

    for name, sealed := range map[string]any{"secret": sealedSecret, "recovery codes": sealedCodes} {
        if false == strings.HasPrefix(string(sealed.([]byte)), "<ENC>\x00gcm2\x00") {
            t.Fatalf("expected the %s sealed bound, got %q", name, sealed)
        }
    }

    var secret melodyencrypt.EncryptedStringFor[SecretColumn]
    if scanErr := secret.Scan(sealedSecret); nil != scanErr || "zz-secret" != string(secret) {
        t.Fatalf("expected the secret to read back through its column, got %q, %v", secret, scanErr)
    }

    var codes melodyencrypt.EncryptedStringFor[RecoveryCodesColumn]
    if scanErr := codes.Scan(sealedSecret); nil == scanErr {
        t.Fatalf("expected the secret's seal refused by the recovery codes column, read %q", codes)
    }

    unbound, _ := cipher.Encrypt("zz-secret", melodyencrypt.EncryptOptions{})
    if scanErr := secret.Scan(unbound); nil == scanErr || false == errors.Is(scanErr, melodyencrypt.ErrUnboundValueRefused) {
        t.Fatalf("expected an unbound seal refused by the secret column, got %v", scanErr)
    }
}
