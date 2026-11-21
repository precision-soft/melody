package encrypt

import (
    "errors"
)

/* ErrSealFormatUnknown is the cause of a refusal of a stored value that carries the encryption marker under a format this build does not read; such a value is never served or migrated as plaintext. */
var ErrSealFormatUnknown = errors.New("encrypted value is sealed under a format this build does not read")

/* ErrSealBound is the cause of a refusal of a seal bound to a column read where no binding is: through Cipher.Decrypt, a plain column type or a column of another marker without one. */
var ErrSealBound = errors.New("encrypted value is bound to a column and cannot be opened without its binding")

/* ErrUnboundValueRefused is the cause of a refusal of a seal without a binding, or a value without the marker, read by a column whose binding refuses unbound values. */
var ErrUnboundValueRefused = errors.New("stored value is not bound to this column, which refuses unbound values")
