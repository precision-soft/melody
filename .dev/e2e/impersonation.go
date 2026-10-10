package main

import (
    "net/http"
    "time"
)

const impersonationLabel = "switch-user over http"

/* the example's switch-user demo resolves two targets (config/impersonation.go): "bob" holds ROLE_USER and "carol" holds ROLE_EDITOR + ROLE_USER. Anything else is unknown to the resolver, which is the fail-closed case probed below. */
const (
    impersonationTarget        = "bob"
    impersonationUnknownTarget = "e2e-no-such-user"
    impersonationCaller        = "e2e-switch-admin"
    impersonationPlainCaller   = "e2e-plain-user"
)

/* runImpersonationCheck drives the switch-user decorator over live http. The property that matters is
AUDITABILITY: the request authorizes as the target while the accountable caller stays readable behind it, so an
audit trail can name who acted. The two negatives are what make the section worth running:

  - a caller WITHOUT the switch role that sends the header must continue as ITSELF. A decorator that honoured the
    header for anyone would turn a single request header into a full account-takeover primitive for every
    authenticated user, and the request would still answer 200 — there is no error to notice;
  - an UNKNOWN target must fail closed. The caller passed the switch-role check, so the request was meant to run
    narrowed to somebody else; continuing with the caller's own broader rights executes it with authority the
    operator believed was constrained.
*/
func runImpersonationCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    switchOutput, switchElapsed := runExampleMintCommand(
        impersonationLabel,
        "auth:token",
        "--user", impersonationCaller,
        "--role", "ROLE_USER",
        "--role", "ROLE_ALLOWED_TO_SWITCH",
    )
    switchToken := exampleMintedToken(impersonationLabel, switchOutput)

    plainOutput, plainElapsed := runExampleMintCommand(
        impersonationLabel,
        "auth:token",
        "--user", impersonationPlainCaller,
        "--role", "ROLE_USER",
    )
    plainToken := exampleMintedToken(impersonationLabel, plainOutput)

    assertImpersonationCarriesBothIdentities(client, switchToken, switchElapsed)
    assertImpersonationWithoutSwitchRoleContinuesAsCaller(client, plainToken, plainElapsed)
    assertImpersonationUnknownTargetFailsClosed(client, switchToken, switchElapsed)
}

func assertImpersonationCarriesBothIdentities(client *liveExampleClient, token string, mintElapsed time.Duration) {
    response := client.call(impersonationLabel, liveExampleRequest{
        method: "GET",
        path:   tokenAuthRoute,
        headerList: map[string]string{
            "Authorization": "Bearer " + token,
            "X-Switch-User": impersonationTarget,
        },
    })

    if http.StatusUnauthorized == response.statusCode {
        fail(
            "%s: a switch-privileged token was refused with 401%s",
            impersonationLabel,
            exampleMintDelayDiagnostic(mintElapsed),
        )
    }
    requireLiveExampleStatus(impersonationLabel, tokenAuthRoute+" under a switch", response, http.StatusOK)

    payload := tokenAuthPayload{}
    decodeLiveExamplePayload(impersonationLabel, response, &payload)

    if impersonationTarget != payload.UserIdentifier {
        fail(
            "%s: the request ran as %q, wanted the switch target %q",
            impersonationLabel,
            payload.UserIdentifier,
            impersonationTarget,
        )
    }
    if nil == payload.Impersonator {
        fail(
            "%s: the request ran as %q with NO impersonator recorded — the accountable caller was erased, so nothing downstream can audit who acted",
            impersonationLabel,
            payload.UserIdentifier,
        )
    }
    if impersonationCaller != payload.Impersonator.UserIdentifier {
        fail(
            "%s: the impersonator is %q, wanted the calling admin %q",
            impersonationLabel,
            payload.Impersonator.UserIdentifier,
            impersonationCaller,
        )
    }

    /* the roles have to be the TARGET's, not the caller's: the switch narrows authority, and a token that kept the caller's switch role while wearing the target's name would let an impersonated session switch again */
    if true == tokenAuthHasRole(payload.Roles, "ROLE_ALLOWED_TO_SWITCH") {
        fail(
            "%s: the impersonating request authorizes with roles %v, which still carry the caller's switch role — the switch widened rather than narrowed authority",
            impersonationLabel,
            payload.Roles,
        )
    }

    pass(
        "the switch produced both identities: acting as %q (roles %v) with %q recorded as the impersonator",
        payload.UserIdentifier,
        payload.Roles,
        payload.Impersonator.UserIdentifier,
    )
}

/* assertImpersonationWithoutSwitchRoleContinuesAsCaller is the most valuable assertion in this batch: the response is a 200 either way, so the only thing separating correct behaviour from an account-takeover primitive is WHICH identity the body names. */
func assertImpersonationWithoutSwitchRoleContinuesAsCaller(client *liveExampleClient, token string, mintElapsed time.Duration) {
    response := client.call(impersonationLabel, liveExampleRequest{
        method: "GET",
        path:   tokenAuthRoute,
        headerList: map[string]string{
            "Authorization": "Bearer " + token,
            "X-Switch-User": impersonationTarget,
        },
    })

    if http.StatusUnauthorized == response.statusCode {
        fail(
            "%s: the token of a caller without the switch role was refused with 401, so the switch-role assertion below never ran%s",
            impersonationLabel,
            exampleMintDelayDiagnostic(mintElapsed),
        )
    }
    requireLiveExampleStatus(impersonationLabel, tokenAuthRoute+" with the switch header but no switch role", response, http.StatusOK)

    payload := tokenAuthPayload{}
    decodeLiveExamplePayload(impersonationLabel, response, &payload)

    if impersonationTarget == payload.UserIdentifier {
        fail(
            "%s: a caller WITHOUT the switch role became %q by sending one header — every authenticated user can act as any other, and the response is a 200 that reveals nothing",
            impersonationLabel,
            payload.UserIdentifier,
        )
    }
    if impersonationPlainCaller != payload.UserIdentifier {
        fail(
            "%s: the unprivileged switch attempt ran as %q, wanted the caller's own %q",
            impersonationLabel,
            payload.UserIdentifier,
            impersonationPlainCaller,
        )
    }
    if nil != payload.Impersonator {
        fail(
            "%s: the unprivileged switch attempt recorded %q as an impersonator, so a switch was performed after all",
            impersonationLabel,
            payload.Impersonator.UserIdentifier,
        )
    }

    pass("a caller without the switch role kept its own identity %q despite sending X-Switch-User", payload.UserIdentifier)
}

/* assertImpersonationUnknownTargetFailsClosed asserts BOTH that the request is refused and that it was not silently served under the caller's own identity. A 200 naming the caller is precisely the fail-to-narrow the framework guards against, so a status-only assertion would pass on it. */
func assertImpersonationUnknownTargetFailsClosed(client *liveExampleClient, token string, mintElapsed time.Duration) {
    response := client.call(impersonationLabel, liveExampleRequest{
        method: "GET",
        path:   tokenAuthRoute,
        headerList: map[string]string{
            "Authorization": "Bearer " + token,
            "X-Switch-User": impersonationUnknownTarget,
        },
    })

    if http.StatusOK == response.statusCode {
        payload := tokenAuthPayload{}
        decodeLiveExamplePayload(impersonationLabel, response, &payload)

        fail(
            "%s: a switch to the unknown target %q was served with 200 as %q — the request continued with the caller's own broader rights instead of failing closed",
            impersonationLabel,
            impersonationUnknownTarget,
            payload.UserIdentifier,
        )
    }
    if http.StatusUnauthorized != response.statusCode && http.StatusForbidden != response.statusCode {
        fail(
            "%s: a switch to an unknown target answered %d, wanted 401 or 403%s: %s",
            impersonationLabel,
            response.statusCode,
            exampleMintDelayDiagnostic(mintElapsed),
            exampleTruncate(response.bodyText()),
        )
    }

    pass("a switch to an unknown target failed closed with %d (the caller's rights were not reused)", response.statusCode)
}
