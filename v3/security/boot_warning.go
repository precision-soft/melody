package security

import (
    "sort"

    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* minimumHmacSha256SecretLength is the shortest HS256 secret RFC 7518 section 3.2 admits: one as long as the hash, 256 bits */
const minimumHmacSha256SecretLength = 32

const (
    bootWarningShortJwtSecret  = "security.jwtSecretShort"
    bootWarningShortHmacSecret = "security.hmacSecretShort"
)

func shortJwtSecretBootWarnings(secret []byte) []internal.BootWarning {
    if minimumHmacSha256SecretLength <= len(secret) {
        return nil
    }

    return []internal.BootWarning{
        {
            Name:    bootWarningShortJwtSecret,
            Message: "the HS256 secret of the jwt validator is shorter than the 32 bytes RFC 7518 asks for, so a token signed with it is open to a brute force of the secret; configure a secret of at least 32 random bytes",
            Context: loggingcontract.Context{
                "configurationKey": "JwtConfig.Secret",
                "secretLength":     len(secret),
                "minimumLength":    minimumHmacSha256SecretLength,
            },
        },
    }
}

func shortHmacSecretBootWarnings(secretsByKeyId map[string][]byte) []internal.BootWarning {
    warnings := make([]internal.BootWarning, 0)

    keyIds := make([]string, 0, len(secretsByKeyId))
    for keyId := range secretsByKeyId {
        keyIds = append(keyIds, keyId)
    }
    sort.Strings(keyIds)

    for _, keyId := range keyIds {
        secretLength := len(secretsByKeyId[keyId])
        if minimumHmacSha256SecretLength <= secretLength {
            continue
        }

        warnings = append(warnings, internal.BootWarning{
            Name:    bootWarningShortHmacSecret,
            Message: "an internal-auth HMAC-SHA256 secret is shorter than 32 bytes, so an envelope signed with it is open to a brute force of the secret; configure a secret of at least 32 random bytes for the key id",
            Context: loggingcontract.Context{
                "configurationKey": "HmacKey.Secret",
                "entry":            keyId,
                "secretLength":     secretLength,
                "minimumLength":    minimumHmacSha256SecretLength,
            },
        })
    }

    return warnings
}

/* bootWarningSource is a security object an application constructs itself that holds boot warnings for its first use */
type bootWarningSource interface {
    writeBootWarnings(logger loggingcontract.Logger)
}

/* writeBootWarningsAtFirstUse writes the boot warnings of source into the journal of the runtime that uses it, once; a source that holds none, or a runtime without a logger, writes nothing */
func writeBootWarningsAtFirstUse(runtimeInstance runtimecontract.Runtime, source any) {
    warningSource, holdsWarnings := source.(bootWarningSource)
    if false == holdsWarnings || true == internal.IsNilInterface(warningSource) {
        return
    }

    logger, loggerErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != loggerErr {
        return
    }

    warningSource.writeBootWarnings(logger)
}
