package contract

import (
    "errors"
)

/* ErrKeyIsDirectory is the cause of a refusal of a key that names a directory, a prefix of other keys and not an object. */
var ErrKeyIsDirectory = errors.New("storage key names a directory")
