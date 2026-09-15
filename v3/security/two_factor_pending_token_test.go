package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestNewTwoFactorPendingToken_RefusesANilPrincipal(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewTwoFactorPendingToken(nil)
    }, "can not build a two-factor pending token from nil")

    var typedNilToken securitycontract.Token = (*AuthenticatedToken)(nil)
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewTwoFactorPendingToken(typedNilToken)
    }, "can not build a two-factor pending token from nil")
}

func TestTwoFactorPendingToken_ShowsNoPrincipalToAuthorization(t *testing.T) {
    pending := NewTwoFactorPendingToken(NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}))

    if true == pending.IsAuthenticated() {
        t.Fatal("expected a pending second factor to read as unauthenticated")
    }

    if "" != pending.UserIdentifier() {
        t.Fatalf("expected no identifier while the second factor is owed, got %q", pending.UserIdentifier())
    }

    if 0 != len(pending.Roles()) {
        t.Fatalf("expected no roles while the second factor is owed, got %v", pending.Roles())
    }

    if nil == pending.Roles() || nil == pending.Scope() || nil == pending.Attributes() {
        t.Fatal("expected empty collections rather than nil, so a caller can range over them")
    }

    if 0 != len(pending.Scope()) || 0 != len(pending.Attributes()) {
        t.Fatalf("expected an empty scope and attributes, got %v and %v", pending.Scope(), pending.Attributes())
    }
}

func TestTwoFactorPendingToken_KeepsThePendingPrincipalReadable(t *testing.T) {
    pending := NewTwoFactorPendingToken(NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}))

    if "u1" != pending.PendingUserIdentifier() {
        t.Fatalf("expected the pending principal to stay readable, got %q", pending.PendingUserIdentifier())
    }

    identifier, isPending := PendingUserFromToken(pending)
    if false == isPending || "u1" != identifier {
        t.Fatalf("expected the reader to answer the pending principal, got %q pending=%v", identifier, isPending)
    }
}

func TestPendingUserFromToken_AnswersFalseForAnythingElse(t *testing.T) {
    var typedNilToken securitycontract.Token = (*AuthenticatedToken)(nil)

    for _, probe := range []securitycontract.Token{nil, typedNilToken, NewAuthenticatedToken("u1", nil), NewAnonymousToken()} {
        identifier, isPending := PendingUserFromToken(probe)
        if true == isPending || "" != identifier {
            t.Fatalf("expected %T to carry no pending principal, got %q pending=%v", probe, identifier, isPending)
        }
    }
}
