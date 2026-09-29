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

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
)

func TestAuthTokenCommandPrintsTheTokenOnTheCommandWriter(t *testing.T) {
    secret := []byte("auth-token-command-test-secret")
    captured := &bytes.Buffer{}

    runErr := NewAuthTokenCommand(secret).Run(nil, newStringFlagContext(map[string]string{"user": "alice"}, captured))
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

    runErr := NewAuthTokenCommand([]byte("secret")).Run(nil, newStringFlagContext(nil, &refusingWriter{refusal: refusal}))
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}

func mintedClaims(t *testing.T, commandContext *melodyclicontract.StaticContext) map[string]any {
    t.Helper()

    captured := &bytes.Buffer{}
    commandContext.WriterValue = captured
    if runErr := NewAuthTokenCommand([]byte("auth-token-command-test-secret")).Run(nil, commandContext); nil != runErr {
        t.Fatalf("expected the token to be minted, got %v", runErr)
    }

    parts := strings.Split(strings.TrimSpace(captured.String()), ".")
    payloadJson, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != decodeErr {
        t.Fatalf("decode the payload: %v", decodeErr)
    }

    claims := map[string]any{}
    if unmarshalErr := json.Unmarshal(payloadJson, &claims); nil != unmarshalErr {
        t.Fatalf("read the claims: %v", unmarshalErr)
    }

    return claims
}

func TestAuthTokenCommandCarriesScopeRolesAloneWithAnEmptyRolesClaim(t *testing.T) {
    claims := mintedClaims(t, &melodyclicontract.StaticContext{
        StringSliceValues: map[string][]string{"scope-role": {"ROLE_EDITOR"}},
        SetFlagNames:      []string{"scope-role"},
    })

    roles, isList := claims["roles"].([]any)
    if false == isList || 0 != len(roles) {
        t.Fatalf("expected an empty roles claim beside the scope roles, got %v", claims["roles"])
    }

    scope, isMap := claims["scope"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected a scope claim, got %v", claims["scope"])
    }

    scopeRoles, isScopeList := scope["roles"].([]any)
    if false == isScopeList || 1 != len(scopeRoles) || "ROLE_EDITOR" != scopeRoles[0] {
        t.Fatalf("expected scope.roles [ROLE_EDITOR], got %v", scope["roles"])
    }
}

func TestAuthTokenCommandKeepsTheDefaultRoleAndNoScopeWithoutEitherFlag(t *testing.T) {
    claims := mintedClaims(t, &melodyclicontract.StaticContext{})

    roles, isList := claims["roles"].([]any)
    if false == isList || 1 != len(roles) || "ROLE_USER" != roles[0] {
        t.Fatalf("expected the default roles [ROLE_USER], got %v", claims["roles"])
    }

    if _, hasScope := claims["scope"]; true == hasScope {
        t.Fatalf("expected no scope claim, got %v", claims["scope"])
    }
}

