package cli

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "strings"
    "testing"
)

func TestAuthTokenCommandPrintsTheTokenOnTheCommandWriter(t *testing.T) {
    secret := []byte("auth-token-command-test-secret")
    captured := &bytes.Buffer{}

    runErr := NewAuthTokenCommand(secret).Run(nil, &flagContext{stringByName: map[string]string{"user": "alice"}, writer: captured})
    if nil != runErr {
        t.Fatalf("expected the token to be minted, got %v", runErr)
    }

    output := captured.String()
    if false == strings.HasSuffix(output, "\n") || 1 != strings.Count(output, "\n") {
        t.Fatalf("expected exactly one line on the command writer, got %q", output)
    }

    parts := strings.Split(strings.TrimSuffix(output, "\n"), ".")
    if 3 != len(parts) {
        t.Fatalf("expected the line to be a token and nothing else, got %q", output)
    }

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(parts[0] + "." + parts[1]))
    if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
        t.Fatalf("expected the printed token to carry the command's signature, got %q", output)
    }

    payloadJson, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != decodeErr {
        t.Fatalf("decode the payload: %v", decodeErr)
    }

    claims := map[string]any{}
    if unmarshalErr := json.Unmarshal(payloadJson, &claims); nil != unmarshalErr {
        t.Fatalf("read the claims: %v", unmarshalErr)
    }

    if "alice" != claims["sub"] {
        t.Fatalf("expected the subject alice, got %v", claims["sub"])
    }
}

func TestAuthTokenCommandAnswersARefusedWrite(t *testing.T) {
    refusal := errors.New("broken pipe")

    runErr := NewAuthTokenCommand([]byte("secret")).Run(nil, &flagContext{writer: &refusingWriter{refusal: refusal}})
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}
