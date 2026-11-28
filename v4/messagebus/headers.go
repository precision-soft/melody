package messagebus

import (
    "errors"
    "sort"
    "unicode"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    DefaultHeaderMaxNameLength = 64
    DefaultHeaderMaxCount      = 32
    DefaultHeaderMaxBytes      = 8 * 1024

    /* HeaderNameMaximumLength is the longest name MaxNameLength admits, an amqp field-table key being a short string; a transport that writes the name behind a prefix refuses above its own lower ceiling. A received name is read up to it, since a writer may have been configured that far. */
    HeaderNameMaximumLength = 255
)

/* ErrHeadersRefused is the cause of every refusal ValidateHeaders answers, so a caller tells a refused message from an unreachable transport. */
var ErrHeadersRefused = errors.New("messagebus: the envelope's headers are refused")

/* HeaderLimitsConfig bounds the headers a transport or a store writes; they are never bounded where they are read. */
type HeaderLimitsConfig struct {
    /* MaxCount is how many headers one message may carry: 32 by default, at least 1, unlimited refused. */
    MaxCount bound.Count
    /* MaxBytes bounds the names and values together: 8 KiB by default, at least 64, unlimited refused, since the bound is what keeps a message inside a broker frame and a row inside a page. */
    MaxBytes bound.Count
    /* MaxNameLength is the longest header name: 64 characters by default, 1 to HeaderNameMaximumLength, unlimited refused. */
    MaxNameLength bound.Count
}

/* HeaderLimits is a HeaderLimitsConfig resolved under its owner's field; the zero value is the defaults. */
type HeaderLimits struct {
    maxCount      int
    maxBytes      int
    maxNameLength int
    field         string
}

/* ResolveHeaderLimits resolves the three limits under the owner's field name, "TransportConfig.Headers" for example, so a refusal names the field the application wrote. */
func ResolveHeaderLimits(config HeaderLimitsConfig, field string) (HeaderLimits, error) {
    maxCount, maxCountErr := bound.Resolve(
        config.MaxCount,
        bound.Rule[int]{Name: field + ".MaxCount", Default: DefaultHeaderMaxCount, Minimum: 1},
    )
    if nil != maxCountErr {
        return HeaderLimits{}, maxCountErr
    }

    maxBytes, maxBytesErr := bound.Resolve(
        config.MaxBytes,
        bound.Rule[int]{Name: field + ".MaxBytes", Default: DefaultHeaderMaxBytes, Minimum: 64},
    )
    if nil != maxBytesErr {
        return HeaderLimits{}, maxBytesErr
    }

    maxNameLength, maxNameLengthErr := bound.Resolve(
        config.MaxNameLength,
        bound.Rule[int]{Name: field + ".MaxNameLength", Default: DefaultHeaderMaxNameLength, Minimum: 1, Maximum: HeaderNameMaximumLength},
    )
    if nil != maxNameLengthErr {
        return HeaderLimits{}, maxNameLengthErr
    }

    return HeaderLimits{maxCount: maxCount.Value, maxBytes: maxBytes.Value, maxNameLength: maxNameLength.Value, field: field}, nil
}

/* ValidateHeaders is the rule every transport and store applies before it writes: a name is 1 to MaxNameLength characters of letters, digits, ".", "_" and "-" beginning with a letter or a digit, a value is valid utf-8 with no control character, and the count and the bytes stay within the limits; a refusal never carries a value. */
func ValidateHeaders(headers map[string]string, limits HeaderLimits) error {
    if 0 == limits.maxCount {
        limits.maxCount = DefaultHeaderMaxCount
        limits.maxBytes = DefaultHeaderMaxBytes
        limits.maxNameLength = DefaultHeaderMaxNameLength
    }

    if limits.maxCount < len(headers) {
        return exception.NewError(
            "the message carries more headers than its limit",
            exceptioncontract.Context{"count": len(headers), "limit": limits.maxCount, "field": limits.field + ".MaxCount"},
            ErrHeadersRefused,
        )
    }

    size, ruleErr := validateHeaderRule(headers, limits.field, limits.maxNameLength)
    if nil != ruleErr {
        return ruleErr
    }

    if limits.maxBytes < size {
        return exception.NewError(
            "the message's headers are larger than their limit",
            exceptioncontract.Context{"bytes": size, "limit": limits.maxBytes, "field": limits.field + ".MaxBytes"},
            ErrHeadersRefused,
        )
    }

    return nil
}

/* IsWritableHeader answers whether one header meets ValidateHeaders' name and value rule, a name up to HeaderNameMaximumLength, so a transport skips on receive what no limits could write back. */
func IsWritableHeader(name string, value string) bool {
    return true == validHeaderName(name, HeaderNameMaximumLength) && true == validHeaderValue(value)
}

func validateHeaderRule(headers map[string]string, field string, maxNameLength int) (int, error) {
    names := make([]string, 0, len(headers))
    for name := range headers {
        names = append(names, name)
    }
    sort.Strings(names)

    size := 0
    for _, name := range names {
        if maxNameLength < len(name) {
            return 0, exception.NewError(
                "a message header name is longer than its limit",
                exceptioncontract.Context{"header": internal.BoundDiagnosticText(name), "length": len(name), "limit": maxNameLength, "field": field + ".MaxNameLength"},
                ErrHeadersRefused,
            )
        }

        if false == validHeaderName(name, maxNameLength) {
            return 0, exception.NewError(
                "a message header name must be letters, digits, \".\", \"_\" and \"-\", beginning with a letter or a digit",
                exceptioncontract.Context{"header": internal.BoundDiagnosticText(name), "field": field},
                ErrHeadersRefused,
            )
        }

        value := headers[name]
        if false == validHeaderValue(value) {
            return 0, exception.NewError(
                "a message header value must be valid utf-8 with no control character",
                exceptioncontract.Context{"header": name, "field": field},
                ErrHeadersRefused,
            )
        }

        size += len(name) + len(value)
    }

    return size, nil
}

func validHeaderName(name string, maxNameLength int) bool {
    if 0 == len(name) || maxNameLength < len(name) {
        return false
    }

    for index := 0; index < len(name); index++ {
        character := name[index]

        alphanumeric := ('a' <= character && 'z' >= character) || ('A' <= character && 'Z' >= character) || ('0' <= character && '9' >= character)
        if true == alphanumeric {
            continue
        }

        if 0 == index || ('.' != character && '_' != character && '-' != character) {
            return false
        }
    }

    return true
}

/* a control character, carriage return and line feed included, would split the log line or the outbound header the value reaches */
func validHeaderValue(value string) bool {
    if false == utf8.ValidString(value) {
        return false
    }

    for _, character := range value {
        if true == unicode.IsControl(character) {
            return false
        }
    }

    return true
}
