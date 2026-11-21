package contract

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveByteRange_ReadsTheRule(t *testing.T) {
    for name, testCase := range map[string]struct {
        byteRange      ByteRange
        size           int64
        expectedOffset int64
        expectedLength int64
    }{
        "the zero value":            {ByteRange{}, 10, 0, 10},
        "a range inside":            {ByteRange{Offset: 3, Length: 4}, 10, 3, 4},
        "a zero length to the end":  {ByteRange{Offset: 3}, 10, 3, 7},
        "a length past the end":     {ByteRange{Offset: 3, Length: 100}, 10, 3, 7},
        "the last byte":             {ByteRange{Offset: 9, Length: 1}, 10, 9, 1},
        "an empty object":           {ByteRange{}, 0, 0, 0},
        "an empty object, a length": {ByteRange{Length: 5}, 0, 0, 0},
    } {
        offset, length, resolveErr := ResolveByteRange(testCase.byteRange, testCase.size)
        if nil != resolveErr || testCase.expectedOffset != offset || testCase.expectedLength != length {
            t.Fatalf("%s: expected (%d, %d), got (%d, %d) %v", name, testCase.expectedOffset, testCase.expectedLength, offset, length, resolveErr)
        }
    }

    for name, testCase := range map[string]struct {
        byteRange ByteRange
        size      int64
    }{
        "at the end":              {ByteRange{Offset: 10}, 10},
        "past the end":            {ByteRange{Offset: 11, Length: 1}, 10},
        "past the end of nothing": {ByteRange{Offset: 1}, 0},
    } {
        if _, _, resolveErr := ResolveByteRange(testCase.byteRange, testCase.size); false == exception.ChainIs(resolveErr, ErrRangeNotSatisfiable) {
            t.Fatalf("%s: expected ErrRangeNotSatisfiable, got %v", name, resolveErr)
        }
    }

    for name, byteRange := range map[string]ByteRange{"a negative offset": {Offset: -1}, "a negative length": {Length: -1}} {
        _, _, resolveErr := ResolveByteRange(byteRange, 10)
        if nil == resolveErr || true == exception.ChainIs(resolveErr, ErrRangeNotSatisfiable) {
            t.Fatalf("%s: expected a refusal that is not ErrRangeNotSatisfiable, got %v", name, resolveErr)
        }

        if "GetOptions.Range" != exception.FromError(resolveErr).Context()["field"] {
            t.Fatalf("%s: expected the refusal to name GetOptions.Range, got %v", name, exception.FromError(resolveErr).Context())
        }
    }
}

func TestRefuseNegativeByteRange_RefusesANegativeOffsetOrLengthOnly(t *testing.T) {
    for name, byteRange := range map[string]ByteRange{"the zero value": {}, "a range": {Offset: 3, Length: 4}} {
        if negativeErr := RefuseNegativeByteRange(byteRange); nil != negativeErr {
            t.Fatalf("%s: expected nil, got %v", name, negativeErr)
        }
    }

    for name, byteRange := range map[string]ByteRange{"a negative offset": {Offset: -1, Length: 1}, "a negative length": {Offset: 1, Length: -1}} {
        if negativeErr := RefuseNegativeByteRange(byteRange); nil == negativeErr || "GetOptions.Range" != exception.FromError(negativeErr).Context()["field"] {
            t.Fatalf("%s: expected the refusal naming GetOptions.Range, got %v", name, negativeErr)
        }
    }
}
