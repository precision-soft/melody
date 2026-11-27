package contract

import (
    "io"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type PutOptions struct {
    ContentType string
}

/* GetOptions is what a Get may ask beyond the key; the zero value reads the whole object. An implementation answers an option it does not know by refusing nothing, so a caller that depends on an option added after this major's tag checks the backend's version. */
type GetOptions struct {
    Range ByteRange
}

/* ByteRange names a part of an object, Length bytes from Offset, judged by ResolveByteRange. The zero value is the whole object, and a zero Length reads from Offset to the end. A negative Offset or Length is refused by RefuseNegativeByteRange before the backend is asked. An Offset at or past the end of an object answers an error whose chain carries ErrRangeNotSatisfiable, the offset 0 of an empty object excepted; a Length that runs past the end is clipped to it. Info.Size stays the whole object's size. */
type ByteRange struct {
    Offset int64
    Length int64
}

/* ObjectInfo is what a storage knows about an object without reading it. Size is the whole object's size in bytes, whatever range was read. ContentType is what the Put that stored it declared; when it declared none, LocalStorage answers it empty and the S3 backend the store's default, application/octet-stream. ModifiedAt is when the object was last stored. EntityTag is the backend's tag of the stored bytes without quotes: the bucket's ETag on S3, and on the local backend a tag derived from the size and the modification time as the static file server derives its own, so weak-equivalent: it moves with every store, but two stores of equal size in the same instant share it. Key is the key the caller asked, as given. */
type ObjectInfo struct {
    Size        int64
    ContentType string
    ModifiedAt  time.Time
    EntityTag   string
    Key         string
}

/* Object is a stored object opened for reading. The caller closes Body, exactly once, also after a read failed; on the S3 backend Body is read under the context of the runtime handed to the Get, and LocalStorage's file reads take no context. Beside an error Object is the zero value and Body is nil: nothing is open. */
type Object struct {
    Body io.ReadCloser
    Info ObjectInfo
}

/* PresignConfig declares a presigned url; the zero value is a download link of the default expiry. */
type PresignConfig struct {
    /* Expiry is how long the url is honoured, read against PresignExpiryRule: 15m when left out, at least 1s, at most 7 days, the longest a SigV4 presigned url is signed for, unlimited refused, since the url is a bearer credential to the object. A value is applied in whole seconds. */
    Expiry bound.Duration

    /* Method is the http method the url is signed for: http.MethodGet, a download, when left out; http.MethodPut signs an upload of the key, which binds neither a content type nor a size. Any other method is refused with ErrPresignUnsupported. */
    Method string
}

/* Storage stores objects under keys. A key is folded as the backends document: a backslash reads as a separator, "." and empty segments and a leading slash fold away, and a ".." segment is refused by name. Every door takes the runtime it runs under first; the S3 backend runs under its context, and LocalStorage reads none, a local file operation taking no context. Get and Stat of a key that names no object answer an error whose chain carries ErrObjectNotFound, and a failure to ask the backend never carries it; Delete of such a key answers nil; whether a key names an object is storage.Exists, derived from Stat. Further verbs, List or Copy, arrive as optional capability interfaces once v4.0.0 is tagged, never as methods of Storage. */
type Storage interface {
    /* Put stores the reader's bytes under the key. A non-negative size is a declaration the backend verifies, refusing a short or long stream; a negative size means the length is unknown, as http.Request.ContentLength uses it, and nothing is verified. */
    Put(runtimeInstance containercontract.Runtime, key string, reader io.Reader, size int64, options PutOptions) error

    /* Get opens the object under the key for reading and answers it with its info. */
    Get(runtimeInstance containercontract.Runtime, key string, options GetOptions) (Object, error)

    /* Stat answers what the storage knows about the object under the key, opening no body. It refuses exactly what Get refuses, by the same causes, and answers the Info a Get would answer at that instant. */
    Stat(runtimeInstance containercontract.Runtime, key string) (ObjectInfo, error)

    Delete(runtimeInstance containercontract.Runtime, key string) error

    /* PresignedUrl answers a url that carries its own authorization, for a client that reads or writes the object straight at the backend. A backend that cannot sign, or cannot sign the method asked, answers an error whose chain carries ErrPresignUnsupported. Whether the object exists is not asked. */
    PresignedUrl(runtimeInstance containercontract.Runtime, key string, config PresignConfig) (string, error)
}
