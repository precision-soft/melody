package encrypt

import (
    "errors"
)

/* ErrSealFormatUnknown is the cause of a refusal of a stored value that carries the encryption marker under a format this build does not read; such a value is never served or migrated as plaintext. */
var ErrSealFormatUnknown = errors.New("encrypted value is sealed under a format this build does not read")
