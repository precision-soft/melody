package cli

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "errors"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func newInternalSignTestSigner(ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        Application: "wms-service",
        Ttl:         ttl,
        Secrets: melodysecurity.NewStaticHmacSecretProvider(melodysecurity.HmacKeyRing{
            CurrentKeyId: "key-current",
            Keys: map[string]melodysecurity.HmacKey{
                "key-current": {Application: "wms-service", Secret: []byte("internal-sign-command-test-secret")},
            },
        }),
    })
}

func TestInternalSignCommandPrintsTheHeaderNameAndValueOnTheCommandWriter(t *testing.T) {
    signer := newInternalSignTestSigner(bound.Duration{})
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(nil, captured))
    if nil != runErr {
        t.Fatalf("expected the header to be minted, got %v", runErr)
    }

    lines := strings.Split(captured.String(), "\n")
    if 3 != len(lines) || "" != lines[2] {
        t.Fatalf("expected exactly two lines on the command writer, got %q", captured.String())
    }

    if signer.HeaderName() != lines[0] {
        t.Fatalf("expected the first line to be the header name %q, got %q", signer.HeaderName(), lines[0])
    }

    if "" == lines[1] {
        t.Fatalf("expected the second line to carry the signed header, got %q", captured.String())
    }
}

func TestInternalSignCommandAnswersARefusedWrite(t *testing.T) {
    refusal := errors.New("broken pipe")

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(nil, &refusingWriter{refusal: refusal}))
    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the refused write to fail the command, got %v", runErr)
    }
}

func TestInternalSignCommandHandsTheSignerTheTtlAsked(t *testing.T) {
    cases := map[string]bound.Duration{
        "":    {},
        "1h":  bound.Of(time.Hour),
        "45s": bound.Of(45 * time.Second),
    }

    for rawTtl, expected := range cases {
        handed := bound.Of(time.Duration(-1))
        newSigner := func(ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
            handed = ttl

            return newInternalSignTestSigner(ttl)
        }

        runErr := NewInternalSignCommand(newSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil != runErr || expected != handed {
            t.Fatalf("expected --ttl %q to hand the signer %s, got %s and %v", rawTtl, expected, handed, runErr)
        }
    }
}

func TestInternalSignCommandRefusesATtlThatIsNotADurationOfAtLeastASecond(t *testing.T) {
    for _, rawTtl := range []string{"soon", "-1s", "0s", "500ms"} {
        runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"ttl": rawTtl}, &bytes.Buffer{}))
        if nil == runErr || false == strings.Contains(runErr.Error(), "is not a go duration of at least 1s") {
            t.Fatalf("expected --ttl %q refused, got %v", rawTtl, runErr)
        }
    }
}

func signedActorOf(t *testing.T, output string) map[string]any {
    t.Helper()

    lines := strings.Split(output, "\n")
    if 2 > len(lines) {
        t.Fatalf("expected the header name and value, got %q", output)
    }

    parts := strings.Split(lines[1], ".")
    if 3 != len(parts) {
        t.Fatalf("expected a three-part envelope, got %q", lines[1])
    }

    payload, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != decodeErr {
        t.Fatalf("decode the envelope: %v", decodeErr)
    }

    envelope := map[string]any{}
    if unmarshalErr := json.Unmarshal(payload, &envelope); nil != unmarshalErr {
        t.Fatalf("unmarshal the envelope: %v", unmarshalErr)
    }

    actor, isMap := envelope["actor"].(map[string]any)
    if false == isMap {
        t.Fatalf("expected the envelope to carry an actor, got %v", envelope)
    }

    return actor
}

func TestInternalSignCommand_SignsTheActorWithTheRolesAndAttributesAsked(t *testing.T) {
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"actor": "user-7", "actor-roles": "ROLE_SUPPLIER,ROLE_AUDITOR", "actor-attributes": "tenant=globex,region=eu"}, captured))
    if nil != runErr {
        t.Fatalf("expected the header minted, got %v", runErr)
    }

    actor := signedActorOf(t, captured.String())
    if false == reflect.DeepEqual([]any{"ROLE_SUPPLIER", "ROLE_AUDITOR"}, actor["Roles"]) {
        t.Fatalf("expected the roles asked, got %v", actor["Roles"])
    }

    if false == reflect.DeepEqual(map[string]any{"tenant": "globex", "region": "eu"}, actor["Attributes"]) {
        t.Fatalf("expected the attributes asked, got %v", actor["Attributes"])
    }
}

func TestInternalSignCommand_DefaultsTheActorsRolesAndTenant(t *testing.T) {
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"actor": "user-7"}, captured))
    if nil != runErr {
        t.Fatalf("expected the header minted, got %v", runErr)
    }

    actor := signedActorOf(t, captured.String())
    if false == reflect.DeepEqual([]any{"ROLE_BUYER"}, actor["Roles"]) || false == reflect.DeepEqual(map[string]any{"tenant": "acme"}, actor["Attributes"]) {
        t.Fatalf("expected the default role and tenant, got %v", actor)
    }
}

func TestInternalSignCommand_RefusesAnAttributeWithoutAValue(t *testing.T) {
    refusals := []map[string]string{
        {"actor": "user-7", "actor-attributes": "tenant"},
        {"actor": "user-7", "actor-attributes": "tenant=acme,"},
        {"actor": "user-7", "actor-attributes": "tenant="},
        {"actor": "user-7", "actor-roles": "ROLE_BUYER,"},
        {"actor": "user-7", "actor-roles": "ROLE BUYER"},
        {"actor": "user-7", "actor-attributes": "tenant= "},
        {"actor": "user-7", "actor-attributes": "tenant=acme,tenant=globex"},
        {"actor": "user-7", "actor-attributes": "tenant=acme, tenant =globex"},
        {"actor": "user-7", "actor-attributes": "tenant=a\x00b"},
        {"actor": "user-7", "actor-attributes": "ten\x00ant=acme"},
        {"actor": "user-7", "actor-roles": "ROLE\x00BUYER"},
        {"actor": "user-7", "actor-roles": "ROLE_BUYER,ROLE_BUYER"},
    }

    for _, values := range refusals {
        captured := &bytes.Buffer{}

        if runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(values, captured)); nil == runErr {
            t.Fatalf("expected %v refused, got a header %q", values, captured.String())
        }
    }
}

func TestInternalSignCommand_TrimsTheAttributeValue(t *testing.T) {
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"actor": "user-7", "actor-attributes": " tenant = globex "}, captured))
    if nil != runErr {
        t.Fatalf("expected the header minted, got %v", runErr)
    }

    if actor := signedActorOf(t, captured.String()); false == reflect.DeepEqual(map[string]any{"tenant": "globex"}, actor["Attributes"]) {
        t.Fatalf("expected the key and the value trimmed, got %v", actor["Attributes"])
    }
}

func TestInternalSignCommand_SignsAnActorWithoutRolesOrAttributesWhenEachIsEmpty(t *testing.T) {
    captured := &bytes.Buffer{}

    runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{"actor": "user-7", "actor-roles": "", "actor-attributes": ""}, captured))
    if nil != runErr {
        t.Fatalf("expected the header minted, got %v", runErr)
    }

    actor := signedActorOf(t, captured.String())
    if roleList, isList := actor["Roles"].([]any); nil != actor["Roles"] && (false == isList || 0 != len(roleList)) {
        t.Fatalf("expected no role, got %v", actor["Roles"])
    }

    if attributes, isMap := actor["Attributes"].(map[string]any); nil != actor["Attributes"] && (false == isMap || 0 != len(attributes)) {
        t.Fatalf("expected no attribute, got %v", actor["Attributes"])
    }
}

func TestInternalSignCommand_RefusesAnActorFlagWithoutTheActor(t *testing.T) {
    for _, flagName := range []string{"actor-roles", "actor-attributes"} {
        captured := &bytes.Buffer{}

        runErr := NewInternalSignCommand(newInternalSignTestSigner).Run(nil, newStringFlagContext(map[string]string{flagName: "x=y"}, captured))
        if nil == runErr || false == strings.Contains(runErr.Error(), "--"+flagName) || 0 != captured.Len() {
            t.Fatalf("expected --%s without --actor refused naming it, got %v and %q", flagName, runErr, captured.String())
        }
    }
}
