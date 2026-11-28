package contract

import (
    "errors"
)

/* ErrKeyIsDirectory is the cause of a refusal of a key that names a directory, a prefix of other keys and not an object. */
var ErrKeyIsDirectory = errors.New("storage key names a directory")

/* ErrObjectNotFound is the cause of a Get or a Stat of a key that names no object. */
var ErrObjectNotFound = errors.New("storage object not found")

/* ErrRangeNotSatisfiable is the cause of a Get whose range starts at or past the end of the object. */
var ErrRangeNotSatisfiable = errors.New("storage range is not satisfiable")

/* ErrPresignUnsupported is the cause of a PresignedUrl the backend cannot sign, or cannot sign for the method asked. */
var ErrPresignUnsupported = errors.New("storage presign is not supported")
