package totp

import (
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha1"
    "crypto/subtle"
    "encoding/base32"
    "encoding/binary"
    "fmt"
    "net/url"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

const (
    defaultPeriod        = 30
    defaultDigits        = 6
    defaultSkew          = 1
    defaultSecretBytes   = 20
    defaultRecoveryCount = 10
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

/* Config tunes the TOTP algorithm (RFC 6238). The zero value is valid and uses the widely interoperable defaults (30-second period, 6 digits, ±1 step skew) that authenticator apps assume. */
type Config struct {
    Period uint
    Digits int
    Skew   uint
}

func (instance Config) withDefaults() Config {
    resolved := instance
    if 0 == resolved.Period {
        resolved.Period = defaultPeriod
    }

    /* RFC 6238 defines 6 to 8 digits; clamp anything outside that range (including the zero value and values large enough to overflow the uint32 modulo in hotpCode) back to the default. */
    if 6 > resolved.Digits || 8 < resolved.Digits {
        resolved.Digits = defaultDigits
    }

    if 0 == resolved.Skew {
        resolved.Skew = defaultSkew
    }

    return resolved
}

/* GenerateSecret returns a new base32 (RFC 4648, unpadded) TOTP secret suitable for an authenticator app and for OtpauthURI. */
func GenerateSecret() (string, error) {
    raw := make([]byte, defaultSecretBytes)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate a totp secret", nil, readErr)
    }

    return base32NoPadding.EncodeToString(raw), nil
}

/* Verify reports whether code is a valid TOTP for secret at the current time, accepting codes within the configured step skew on either side. */
func Verify(secret string, code string, config Config) (bool, error) {
    return VerifyAt(secret, code, time.Now(), config)
}

/* VerifyAt is Verify at an explicit time, exposed for deterministic testing and for callers that drive their own clock. */
func VerifyAt(secret string, code string, at time.Time, config Config) (bool, error) {
    resolved := config.withDefaults()

    key, decodeErr := decodeSecret(secret)
    if nil != decodeErr {
        return false, decodeErr
    }

    if len(code) != resolved.Digits {
        return false, nil
    }

    base := at.Unix() / int64(resolved.Period)

    for delta := -int64(resolved.Skew); delta <= int64(resolved.Skew); delta++ {
        counter := base + delta
        if 0 > counter {
            continue
        }

        candidate := hotpCode(key, uint64(counter), resolved.Digits)
        if 1 == subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) {
            return true, nil
        }
    }

    return false, nil
}

/* GenerateCodeAt produces the TOTP for secret at the given time. It is primarily useful in tests and for clients that need to render a code; servers verify with Verify. */
func GenerateCodeAt(secret string, at time.Time, config Config) (string, error) {
    resolved := config.withDefaults()

    key, decodeErr := decodeSecret(secret)
    if nil != decodeErr {
        return "", decodeErr
    }

    counter := at.Unix() / int64(resolved.Period)
    if 0 > counter {
        return "", exception.NewError("totp time is before the unix epoch", nil, nil)
    }

    return hotpCode(key, uint64(counter), resolved.Digits), nil
}

/* OtpauthURI builds the otpauth:// URI an authenticator app consumes (typically rendered as a QR code) during enrollment. */
func OtpauthURI(issuer string, accountName string, secret string, config Config) string {
    resolved := config.withDefaults()

    label := url.PathEscape(issuer + ":" + accountName)

    query := url.Values{}
    query.Set("secret", secret)
    query.Set("issuer", issuer)
    query.Set("algorithm", "SHA1")
    query.Set("digits", fmt.Sprintf("%d", resolved.Digits))
    query.Set("period", fmt.Sprintf("%d", resolved.Period))

    return "otpauth://totp/" + label + "?" + query.Encode()
}

/* GenerateRecoveryCodes returns count single-use recovery codes (formatted xxxxx-xxxxx). The caller stores them (encrypted) and removes each on use; a count of zero yields the default set size. */
func GenerateRecoveryCodes(count int) ([]string, error) {
    if 0 >= count {
        count = defaultRecoveryCount
    }

    codes := make([]string, 0, count)
    for index := 0; index < count; index++ {
        code, codeErr := newRecoveryCode()
        if nil != codeErr {
            return nil, codeErr
        }

        codes = append(codes, code)
    }

    return codes, nil
}

func hotpCode(key []byte, counter uint64, digits int) string {
    var counterBytes [8]byte
    binary.BigEndian.PutUint64(counterBytes[:], counter)

    mac := hmac.New(sha1.New, key)
    mac.Write(counterBytes[:])
    sum := mac.Sum(nil)

    offset := sum[len(sum)-1] & 0x0f
    truncated := (uint32(sum[offset]&0x7f) << 24) |
        (uint32(sum[offset+1]) << 16) |
        (uint32(sum[offset+2]) << 8) |
        uint32(sum[offset+3])

    modulo := uint32(1)
    for index := 0; index < digits; index++ {
        modulo *= 10
    }

    return fmt.Sprintf("%0*d", digits, truncated%modulo)
}

func decodeSecret(secret string) ([]byte, error) {
    normalized := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(secret))
    if "" == normalized {
        return nil, exception.NewError("totp secret is empty", nil, nil)
    }

    key, decodeErr := base32NoPadding.DecodeString(normalized)
    if nil != decodeErr {
        return nil, exception.NewError("totp secret is not valid base32", nil, decodeErr)
    }

    return key, nil
}

func newRecoveryCode() (string, error) {
    /* six random bytes base32-encode (unpadded) to exactly ten characters, so the [:5]+"-"+[5:] split yields the documented xxxxx-xxxxx layout (50 bits of entropy); five bytes would only produce eight characters (xxxxx-xxx). */
    raw := make([]byte, 6)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate a recovery code", nil, readErr)
    }

    encoded := strings.ToLower(base32NoPadding.EncodeToString(raw))

    return encoded[:5] + "-" + encoded[5:], nil
}
