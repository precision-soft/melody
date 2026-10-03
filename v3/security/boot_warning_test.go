package security

import (
    "strings"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func bootWarningRecords(logger *recordingLogger, name string) []recordedLogRecord {
    named := make([]recordedLogRecord, 0)
    for _, record := range logger.recordsAtLevel(loggingcontract.LevelWarning) {
        if name == record.context["bootWarning"] {
            named = append(named, record)
        }
    }

    return named
}

/* an HS256 secret under the 32 bytes RFC 7518 asks for is named once, at the validator's first token, by its configuration key and its length, never its value */
func TestJwtTokenValidator_AShortSecretIsNamedOnceAtTheFirstTokenAndNeverItsValue(t *testing.T) {
    runtimeInstance, logger := runtimeWithRecordingLogger()

    validator := NewJwtTokenValidator(JwtConfig{Secret: []byte("short-secret")})
    for range 3 {
        _, _ = validator.Validate(runtimeInstance, "not.a.token")
    }

    records := bootWarningRecords(logger, bootWarningShortJwtSecret)
    if 1 != len(records) {
        t.Fatalf("expected the short secret named once, got %v", records)
    }

    if "JwtConfig.Secret" != records[0].context["configurationKey"] || 12 != records[0].context["secretLength"] {
        t.Fatalf("expected the configuration key and the length, got %v", records[0].context)
    }

    for _, value := range records[0].context {
        if rendered, isString := value.(string); true == isString && true == strings.Contains(rendered, "short-secret") {
            t.Fatalf("expected the secret never written, got %v", records[0].context)
        }
    }

    longRuntime, longLogger := runtimeWithRecordingLogger()
    _, _ = NewJwtTokenValidator(JwtConfig{Secret: []byte("a-secret-of-exactly-32-bytes-ok!")}).Validate(longRuntime, "not.a.token")

    if records := bootWarningRecords(longLogger, bootWarningShortJwtSecret); 0 != len(records) {
        t.Fatalf("expected a 32-byte secret to be silent, got %v", records)
    }
}

/* an internal-auth secret under 32 bytes is named by its key id, once, at the first envelope a token source resolves with the provider */
func TestHmacTokenSource_AShortSecretOfItsProviderIsNamedOnceByItsKeyId(t *testing.T) {
    runtimeInstance, logger := runtimeWithRecordingLogger()

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets: NewStaticHmacSecretProvider(
            "key-current",
            map[string]HmacKey{
                "key-current":  {App: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
                "key-previous": {App: "wms-service", Secret: []byte("too-short")},
            },
        ),
        Apps:       hmacTestApps(),
        NonceGuard: NewMemoryNonceGuard(),
    })

    for range 3 {
        _, _ = source.Resolve(runtimeInstance, hmacRequest("GET", "/internal", nil, "", ""))
    }

    records := bootWarningRecords(logger, bootWarningShortHmacSecret)
    if 1 != len(records) || "key-previous" != records[0].context["entry"] || 9 != records[0].context["secretLength"] {
        t.Fatalf("expected the short key id named once with its length, got %v", records)
    }

    silentRuntime, silentLogger := runtimeWithRecordingLogger()
    _, _ = hmacTestSource(NewMemoryNonceGuard()).Resolve(silentRuntime, hmacRequest("GET", "/internal", nil, "", ""))

    if records := bootWarningRecords(silentLogger, bootWarningShortHmacSecret); 0 != len(records) {
        t.Fatalf("expected 32-byte secrets to be silent, got %v", records)
    }
}
