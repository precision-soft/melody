package user

import (
    "encoding/json"
    "errors"
    "io"
)

/* errTrailingData refuses a body that carries more than one json document */
var errTrailingData = errors.New("the body carries data after its json document")

/* decodeOneDocument decodes the body's one json document and refuses anything after it, as the product and currency doors' binding does: a second document behind the first is not silently dropped */
func decodeOneDocument(body io.Reader, target any) error {
    decoder := json.NewDecoder(body)
    if decodeErr := decoder.Decode(target); nil != decodeErr {
        return decodeErr
    }

    if _, trailingErr := decoder.Token(); false == errors.Is(trailingErr, io.EOF) {
        if nil == trailingErr {
            return errTrailingData
        }

        return trailingErr
    }

    return nil
}
