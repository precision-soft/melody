package http

import (
    "testing"
)

func TestNormalizeResultToResponse_TypedNilResponseBecomesNilInterface(t *testing.T) {
    var typedNil *Response

    response, err := NormalizeResultToResponse(nil, nil, typedNil)
    if nil != err {
        t.Fatalf("NormalizeResultToResponse returned an error: %v", err)
    }

    if nil != response {
        t.Fatalf("a typed-nil *Response must normalize to a nil httpcontract.Response interface so the kernel takes the no-content path; a non-nil interface wrapping a nil *Response panics on Headers()")
    }
}
