package security

import (
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* minimumHmacSha256SecretLength is the shortest HS256 secret RFC 7518 section 3.2 admits, one as long as the hash, 256 bits; a MinimumSecretLength field may raise it, never lower it */
const minimumHmacSha256SecretLength = 32

func resolveMinimumSecretLength(limit bound.Count, field string) int {
    resolved, resolveErr := bound.Resolve(
        limit,
        bound.Rule[int]{
            Name:    field,
            Default: minimumHmacSha256SecretLength,
            Minimum: minimumHmacSha256SecretLength,
        },
    )
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    return resolved.Value
}

func shortSecretContext(field string, secretLength int, minimumLength int) exceptioncontract.Context {
    return exceptioncontract.Context{
        "field":         field,
        "secretLength":  secretLength,
        "minimumLength": minimumLength,
    }
}
