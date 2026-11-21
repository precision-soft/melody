package contract

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* RefuseNegativeByteRange refuses a range whose Offset or Length is negative, naming GetOptions.Range; a backend calls it before it is asked anything, since the refusal needs no size. */
func RefuseNegativeByteRange(byteRange ByteRange) error {
    if 0 > byteRange.Offset || 0 > byteRange.Length {
        return exception.NewError(
            "the storage range is negative",
            exceptioncontract.Context{
                "field":  "GetOptions.Range",
                "offset": byteRange.Offset,
                "length": byteRange.Length,
            },
            nil,
        )
    }

    return nil
}

/* ResolveByteRange answers the offset and the length a Get reads of an object of the given size, the length clipped to the end, or the refusal of the range; every backend reads its range through here. */
func ResolveByteRange(byteRange ByteRange, size int64) (int64, int64, error) {
    if negativeErr := RefuseNegativeByteRange(byteRange); nil != negativeErr {
        return 0, 0, negativeErr
    }

    if size < byteRange.Offset || (size == byteRange.Offset && 0 < size) {
        return 0, 0, exception.NewError(
            "the storage range starts past the end of the object",
            exceptioncontract.Context{
                "field":  "GetOptions.Range",
                "offset": byteRange.Offset,
                "size":   size,
            },
            ErrRangeNotSatisfiable,
        )
    }

    remaining := size - byteRange.Offset
    if 0 == byteRange.Length || remaining < byteRange.Length {
        return byteRange.Offset, remaining, nil
    }

    return byteRange.Offset, byteRange.Length, nil
}
