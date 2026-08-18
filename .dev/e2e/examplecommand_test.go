package main

import (
    "encoding/base64"
    "encoding/json"
    "strings"
    "testing"
    "time"

    security "github.com/precision-soft/melody/v3/security"
)

/* mintTestOutput reproduces the shape a real mint prints: the boot log as json objects on stdout, then the command
banner wrapped in ANSI sequences, then the credential, then the closing banner. Everything the extraction has to
step over is present. */
const mintTestOutput = "{\"message\":\"configuration defaults applied\",\"level\":\"info\"}\n" +
    "{\"message\":\"configuration validated\",\"level\":\"info\"}\n" +
    "\x1b[42m\x1b[2K\x1b[0m\n" +
    "\x1b[42m\x1b[2K\x1b[97m====== [auth:token] [started] ======\x1b[0m\n" +
    "\n" +
    "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.c2lnbmF0dXJl\n" +
    "\n" +
    "\x1b[42m\x1b[2K\x1b[97m====== [auth:token] [finished] [success] ======\x1b[0m\n"

/* The extraction decides which string is handed to the firewall. Picking a boot-log line, or a fragment of a
banner, produces a 401 that reads exactly like a security regression — so the stripping and the anchoring are worth
pinning. */
func TestExampleStripAnsi_RemovesTheBannerSequences(t *testing.T) {
    plain := exampleStripAnsi(mintTestOutput)

    if true == strings.Contains(plain, "\x1b[") {
        t.Fatal("an escape sequence survived the strip")
    }
    if false == strings.Contains(plain, "====== [auth:token] [started] ======") {
        t.Fatal("the strip removed the text along with the escapes")
    }
}

func TestExampleMintedToken_TakesTheCredentialAndNotABootLogLine(t *testing.T) {
    token := exampleMintedToken("test", exampleStripAnsi(mintTestOutput))

    if "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.c2lnbmF0dXJl" != token {
        t.Fatalf("the extracted credential is %q", token)
    }
}

/* the LAST match is taken on purpose: the boot log is printed above the command body, so a future log line that
happened to look like a token would otherwise be handed to the firewall in place of the credential. */
func TestExampleLastLineMatching_PrefersTheLastMatch(t *testing.T) {
    output := "aaa.bbb.ccc\nsome prose\nddd.eee.fff\n"

    if found := exampleLastLineMatching(output, exampleCompactTokenPattern); "ddd.eee.fff" != found {
        t.Fatalf("the extraction returned %q, wanted the last match", found)
    }
}

/* the pattern is anchored, so a token-shaped fragment inside a longer line must NOT be extracted: a json log line
carrying a dotted identifier is the realistic case. */
func TestExampleCompactTokenPattern_IsAnchoredToWholeLines(t *testing.T) {
    for _, line := range []string{
        `{"message":"resolved a.b.c","level":"info"}`,
        "the token is aaa.bbb.ccc",
        "aaa.bbb.ccc trailing",
        "aaa.bbb",
    } {
        if true == exampleCompactTokenPattern.MatchString(line) {
            t.Fatalf("%q must not be taken for a credential", line)
        }
    }

    if false == exampleCompactTokenPattern.MatchString("aaa.bbb.ccc") {
        t.Fatal("a bare three-segment token must match")
    }
}

func TestExampleMintedTotpCode_MatchesOnlySixDigitLines(t *testing.T) {
    output := "{\"message\":\"boot\",\"level\":\"info\"}\n====== [totp:code] [started] ======\n\n710994\n"

    if code := exampleMintedTotpCode("test", output); "710994" != code {
        t.Fatalf("the extracted code is %q", code)
    }

    for _, line := range []string{"7109940", "71099", "710 994", "code 710994"} {
        if true == exampleTotpCodePattern.MatchString(line) {
            t.Fatalf("%q must not be taken for a totp code", line)
        }
    }
}

/* the mint environment must withhold the harness's own backend variables: melody resolves configuration from the
.env beside the executable, and a leaked variable would point the mint at different infrastructure than the SERVING
application uses — a mint signing with another secret produces a 401 that looks like a security regression. */
func TestExampleMintEnvironment_WithholdsTheHarnessBackendVariables(t *testing.T) {
    for _, variable := range []string{
        "REDIS_ADDRESS",
        "POSTGRES_DSN",
        "MYSQL_DSN",
        "AMQP_DSN",
        "MINIO_ENDPOINT",
        "SMTP_ADDRESS",
        "EXAMPLE_BASE_URL",
        "PROMETHEUS_URL",
    } {
        t.Setenv(variable, "leaked-value-that-must-not-reach-the-mint")
    }

    for _, entry := range exampleMintEnvironment() {
        if true == strings.Contains(entry, "leaked-value-that-must-not-reach-the-mint") {
            t.Fatalf("the mint environment carries %q", entry)
        }
    }

    /* it must still carry what the build and the boot need, or the mint fails for an unrelated reason */
    joined := strings.Join(exampleMintEnvironment(), "\n")
    if false == strings.Contains(joined, "PATH=") {
        t.Fatal("the mint environment carries no PATH")
    }
    if false == strings.Contains(joined, "HOME=") {
        t.Fatal("the mint environment carries no HOME")
    }
}

/* the diagnostic must stay EMPTY for a mint that finished well inside the credential's lifetime: a caveat attached
to every 401 would soften a genuine refusal into something a reader dismisses. */
func TestExampleMintDelayDiagnostic_OnlySpeaksWhenTheMintOutranTheCredential(t *testing.T) {
    if diagnostic := exampleMintDelayDiagnostic(time.Second); "" != diagnostic {
        t.Fatalf("a one-second mint produced the diagnostic %q", diagnostic)
    }
    if diagnostic := exampleMintDelayDiagnostic(exampleHmacEnvelopeLifetime - time.Millisecond); "" != diagnostic {
        t.Fatalf("a mint just inside the lifetime produced the diagnostic %q", diagnostic)
    }

    diagnostic := exampleMintDelayDiagnostic(exampleHmacEnvelopeLifetime)
    if "" == diagnostic {
        t.Fatal("a mint that reached the credential's lifetime must produce the diagnostic")
    }
    if false == strings.Contains(diagnostic, "NOT a failure of the code under test") {
        t.Fatalf("the diagnostic does not say the refusal is unattributable: %q", diagnostic)
    }
}

/* the mirrored lifetime has to match defaultHmacSignerTtl in v3/security/hmac_signer.go: a value larger than the
real one would let a stale-credential 401 be reported as a genuine refusal.

The signer's default is read out of an envelope it actually mints rather than compared against a second literal.
That constant is unexported, so a test that names 30s twice only says the harness agrees with itself: changing
defaultHmacSignerTtl to 10s — the exact drift this test exists to catch — left it green. Signing one request with
Ttl unset and subtracting the envelope's own iat from its exp measures what the framework does. */
func TestExampleHmacEnvelopeLifetime_MirrorsTheSignerDefault(t *testing.T) {
    signer := security.NewHmacEnvelopeSigner(security.HmacEnvelopeSignerConfig{
        App: "lifetime-probe",
        Secrets: security.NewStaticHmacSecretProvider(
            "probe-key-1",
            map[string]security.HmacKey{
                "probe-key-1": {App: "lifetime-probe", Secret: []byte("lifetime-probe-secret-000000001")},
            },
        ),
    })

    headerValue, signErr := signer.Sign("GET", "/probe", nil, nil)
    if nil != signErr {
        t.Fatalf("unexpected signing error: %v", signErr)
    }

    parts := strings.Split(headerValue, ".")
    if 3 != len(parts) {
        t.Fatalf("the internal-auth header has %d parts, wanted 3: %q", len(parts), headerValue)
    }

    payloadBytes, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != decodeErr {
        t.Fatalf("the envelope payload is not base64url: %v", decodeErr)
    }

    var envelope struct {
        IssuedAt  int64 `json:"iat"`
        ExpiresAt int64 `json:"exp"`
    }
    if unmarshalErr := json.Unmarshal(payloadBytes, &envelope); nil != unmarshalErr {
        t.Fatalf("the envelope payload is not json: %v", unmarshalErr)
    }

    signerDefault := time.Duration(envelope.ExpiresAt-envelope.IssuedAt) * time.Second
    if signerDefault != exampleHmacEnvelopeLifetime {
        t.Fatalf(
            "the mirrored envelope lifetime is %s and the signer mints %s; update exampleHmacEnvelopeLifetime to match defaultHmacSignerTtl in v3/security/hmac_signer.go",
            exampleHmacEnvelopeLifetime,
            signerDefault,
        )
    }
}
