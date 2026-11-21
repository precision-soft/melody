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

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    defaultSecretBytes   = 20
    defaultRecoveryCount = 10
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

/* Config tunes the TOTP algorithm (RFC 6238); the zero value is the interoperable default authenticator apps assume: a 30s period, 6 digits, one step of skew, the system clock. A field outside its range is refused naming it. */
type Config struct {
    /* Period is the step length: 30s by default, at least 1s and at most 24h, in whole seconds, unlimited refused. */
    Period bound.Duration

    /* Digits is the code length: 6 by default, 6 to 8, unlimited refused. */
    Digits bound.Count

    /* Skew is how many steps on either side of the current one a code is accepted from: 1 by default, 0 for the current step alone, at most 10, unlimited refused. */
    Skew bound.Count

    /* Clock is where Verify reads the current step; nil is the system clock. */
    Clock clockcontract.Clock
}

/* Resolved is a Config decided against its rules: the values the algorithm runs with. */
type Resolved struct {
    Period time.Duration
    Digits int
    Skew   int
    Clock  clockcontract.Clock
}

var periodRule = bound.Rule[time.Duration]{
    Name:    "totp.Config.Period",
    Default: 30 * time.Second,
    Minimum: bound.TtlMinimum,
    Maximum: 24 * time.Hour,
}

var digitsRule = bound.Rule[int]{
    Name:    "totp.Config.Digits",
    Default: 6,
    Minimum: 6,
    Maximum: 8,
}

var skewRule = bound.Rule[int]{
    Name:    "totp.Config.Skew",
    Default: 1,
    Minimum: 0,
    Maximum: 10,
}

/* Resolve answers the config the algorithm runs with, every field decided against its rule; a field outside its range is answered as the error, naming it. A caller that sizes anything on the window Verify accepts, a replay-guard ttl above all, resolves through it. */
func (instance Config) Resolve() (Resolved, error) {
    period, periodErr := bound.Resolve(instance.Period, periodRule)
    if nil != periodErr {
        return Resolved{}, periodErr
    }

    if 0 != period.Value%time.Second {
        return Resolved{}, exception.NewError(
            "the totp period is not a whole number of seconds",
            exceptioncontract.Context{
                "field": periodRule.Name,
                "value": period.Value.String(),
            },
            nil,
        )
    }

    digits, digitsErr := bound.Resolve(instance.Digits, digitsRule)
    if nil != digitsErr {
        return Resolved{}, digitsErr
    }

    skew, skewErr := bound.Resolve(instance.Skew, skewRule)
    if nil != skewErr {
        return Resolved{}, skewErr
    }

    clockInstance := instance.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    return Resolved{
        Period: period.Value,
        Digits: digits.Value,
        Skew:   skew.Value,
        Clock:  clockInstance,
    }, nil
}

/* GenerateSecret returns a new base32 (RFC 4648, unpadded) TOTP secret suitable for an authenticator app and for OtpauthUri. */
func GenerateSecret() (string, error) {
    raw := make([]byte, defaultSecretBytes)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate a totp secret", nil, readErr)
    }

    return base32NoPadding.EncodeToString(raw), nil
}

/* Verify reports whether code is a valid TOTP for secret at the config's clock, accepting codes within the configured skew on either side; a config outside its ranges is answered as the error. Whitespace anywhere in the submitted code is stripped before the constant-time comparison, since authenticator apps display "123 456" and a digit code never contains whitespace. */
func Verify(secret string, code string, config Config) (bool, error) {
    resolved, resolveErr := config.Resolve()
    if nil != resolveErr {
        return false, resolveErr
    }

    key, decodeErr := decodeSecret(secret)
    if nil != decodeErr {
        return false, decodeErr
    }

    code = NormalizeCode(code)

    if len(code) != resolved.Digits {
        return false, nil
    }

    base := resolved.Clock.Now().Unix() / int64(resolved.Period/time.Second)

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

/* NormalizeCode strips whitespace from a submitted code, as Verify does before comparison. A caller that keys anything on an accepted code, a replay guard above all, keys on this form, or a captured code could be replayed by re-spacing it. */
func NormalizeCode(code string) string {
    return strings.Join(strings.Fields(code), "")
}

/* GenerateCodeAt produces the TOTP for secret at the given instant; it renders a code for a client or a test, so it takes the instant rather than the clock. A config outside its ranges is answered as the error. */
func GenerateCodeAt(secret string, at time.Time, config Config) (string, error) {
    resolved, resolveErr := config.Resolve()
    if nil != resolveErr {
        return "", resolveErr
    }

    key, decodeErr := decodeSecret(secret)
    if nil != decodeErr {
        return "", decodeErr
    }

    counter := at.Unix() / int64(resolved.Period/time.Second)
    if 0 > counter {
        return "", exception.NewError("totp time is before the unix epoch", nil, nil)
    }

    return hotpCode(key, uint64(counter), resolved.Digits), nil
}

/* OtpauthUri builds the otpauth:// URI an authenticator app consumes (typically rendered as a QR code) during enrollment; a config outside its ranges is answered as the error. */
func OtpauthUri(issuer string, accountName string, secret string, config Config) (string, error) {
    resolved, resolveErr := config.Resolve()
    if nil != resolveErr {
        return "", resolveErr
    }

    label := url.PathEscape(issuer + ":" + accountName)

    query := url.Values{}
    query.Set("secret", secret)
    query.Set("issuer", issuer)
    query.Set("algorithm", "SHA1")
    query.Set("digits", fmt.Sprintf("%d", resolved.Digits))
    query.Set("period", fmt.Sprintf("%d", int64(resolved.Period/time.Second)))

    return "otpauth://totp/" + label + "?" + query.Encode(), nil
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
    /* six random bytes base32-encode, unpadded, to exactly ten characters, which the split lays out as xxxxx-xxxxx */
    raw := make([]byte, 6)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate a recovery code", nil, readErr)
    }

    encoded := strings.ToLower(base32NoPadding.EncodeToString(raw))

    return encoded[:5] + "-" + encoded[5:], nil
}
