package main

import (
    "encoding/base64"
    "encoding/json"
    "strings"
    "testing"
    "time"
)

/* Each of these helpers produces a credential the firewall must REFUSE, so a helper that quietly produced a valid
credential — or the very same credential it was asked to corrupt — would turn its negative into an assertion that
can never fail. That is the whole reason they are tested. */

func decodeTokenSegment(t *testing.T, segment string) map[string]any {
    t.Helper()

    raw, decodeErr := base64.RawURLEncoding.DecodeString(segment)
    if nil != decodeErr {
        t.Fatalf("segment %q is not base64url: %v", segment, decodeErr)
    }

    decoded := map[string]any{}
    if unmarshalErr := json.Unmarshal(raw, &decoded); nil != unmarshalErr {
        t.Fatalf("segment %q is not json: %v", segment, unmarshalErr)
    }

    return decoded
}

/* the wrong-secret token must be correct in every respect EXCEPT its signing key: a token that was also expired,
or missing its subject, would be refused for that reason instead and the negative would prove nothing about
signature verification. */
func TestTokenAuthForeignToken_IsWellFormedAndUnexpired(t *testing.T) {
    token := tokenAuthForeignToken("someone", []string{"ROLE_USER"})

    segmentList := strings.Split(token, ".")
    if 3 != len(segmentList) {
        t.Fatalf("the foreign token has %d segments, wanted 3", len(segmentList))
    }

    header := decodeTokenSegment(t, segmentList[0])
    if "HS256" != header["alg"] {
        t.Fatalf("the foreign token declares alg %v, wanted HS256 — it must differ from the real token ONLY in its key", header["alg"])
    }

    claims := decodeTokenSegment(t, segmentList[1])
    if "someone" != claims["sub"] {
        t.Fatalf("the foreign token's subject is %v", claims["sub"])
    }

    expiry, isNumber := claims["exp"].(float64)
    if false == isNumber {
        t.Fatalf("the foreign token carries no numeric exp: %v", claims["exp"])
    }
    if int64(expiry) <= time.Now().Unix() {
        t.Fatal("the foreign token is already expired, so it would be refused for the wrong reason")
    }

    if "" == segmentList[2] {
        t.Fatal("the foreign token carries no signature at all, which is the alg:none case rather than the wrong-key one")
    }
}

/* the harness must never hold the application's secret: a foreign token signed with the demo key would be ACCEPTED
and the negative would report a firewall failure that is really a harness mistake. */
func TestTokenAuthForeignSecret_IsNotTheApplicationSecret(t *testing.T) {
    if "melody-example-demo-secret-change-me" == tokenAuthForeignSecret {
        t.Fatal("the harness's signing key must differ from the example's demo secret")
    }
    if true == strings.Contains(tokenAuthForeignSecret, "melody-example") {
        t.Fatal("the harness's signing key must not be derived from the example's")
    }
}

func TestTokenAuthAlgorithmNoneToken_DeclaresNoneAndCarriesNoSignature(t *testing.T) {
    token := tokenAuthAlgorithmNoneToken("someone", []string{"ROLE_ADMIN"})

    segmentList := strings.Split(token, ".")
    if 3 != len(segmentList) {
        t.Fatalf("the alg:none token has %d segments, wanted 3 (the third one empty)", len(segmentList))
    }
    if "" != segmentList[2] {
        t.Fatalf("the alg:none token carries a signature (%q), which is not the attack under test", segmentList[2])
    }

    header := decodeTokenSegment(t, segmentList[0])
    if "none" != header["alg"] {
        t.Fatalf("the alg:none token declares alg %v", header["alg"])
    }

    /* the claims must still be the ones an attacker would want honoured, or the refusal could be attributed to a
       malformed payload rather than to the algorithm */
    claims := decodeTokenSegment(t, segmentList[1])
    if "someone" != claims["sub"] {
        t.Fatalf("the alg:none token's subject is %v", claims["sub"])
    }
}

/* a tamper that reproduced the original token would make the tampered-signature negative vacuous: the firewall
would accept it, correctly, and the section would report a pass. */
func TestTokenAuthTamperedSignature_AlwaysChangesTheToken(t *testing.T) {
    for _, token := range []string{
        tokenAuthForeignToken("someone", []string{"ROLE_USER"}),
        "header.payload.Asignaturestartingwiththereplacement",
        "header.payload.Bsignaturestartingwiththealternate",
        "header.payload.x",
    } {
        tampered := tokenAuthTamperedSignature(token)

        if tampered == token {
            t.Fatalf("the tamper reproduced the input token %q", token)
        }
        if len(tampered) != len(token) {
            t.Fatalf("the tamper changed the token length (%d -> %d), so it may be refused as malformed rather than as unsigned", len(token), len(tampered))
        }

        lastDot := strings.LastIndex(token, ".")
        if token[:lastDot] != tampered[:lastDot] {
            t.Fatal("the tamper must touch the signature only, leaving the signed input identical")
        }
    }
}

/* a token with nothing to flip is returned unchanged, and the section's own guard is what has to catch that — the
helper must not invent a signature, which would change what is being asserted. */
func TestTokenAuthTamperedSignature_LeavesAnEmptySignatureAlone(t *testing.T) {
    if tampered := tokenAuthTamperedSignature("header.payload."); "header.payload." != tampered {
        t.Fatalf("a token with an empty signature came back as %q", tampered)
    }
    if tampered := tokenAuthTamperedSignature("not-a-token"); "not-a-token" != tampered {
        t.Fatalf("a token with no dot came back as %q", tampered)
    }
}
