package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "io"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/redis/rueidis"
)

const (
    exampleTokenStoreKeyPrefix = "{melody-example-v3:token}:token:"
    exampleTokenStoreEpochKey  = "{melody-example-v3:token}:epoch:"

    exampleRevocationUserField        = "user"
    exampleRevocationDeviceFieldPre   = "device:"
    exampleRevocationEditorIdentifier = "user-2"
)

type exampleIssuedToken struct {
    Token            string    `json:"token"`
    DeviceIdentifier string    `json:"deviceIdentifier"`
    IssuedAt         time.Time `json:"issuedAt"`
}

type exampleRevocation struct {
    UserIdentifier   string    `json:"userIdentifier"`
    DeviceIdentifier string    `json:"deviceIdentifier"`
    RevokedBefore    time.Time `json:"revokedBefore"`
}

type exampleStoredClaims struct {
    UserIdentifier   string    `json:"UserIdentifier"`
    Roles            []string  `json:"Roles"`
    DeviceIdentifier string    `json:"DeviceIdentifier"`
    IssuedAt         time.Time `json:"IssuedAt"`
}

func runOpaqueTokenRevocationCheck(baseUrl string, redisAddress string) {
    redisClient := openRedis(redisAddress)
    defer redisClient.Close()
    bearer := newLiveExampleClient(baseUrl)

    editor := newExampleHttpClient()
    signInExampleHttpEditor(editor, baseUrl, "")

    administrator := newExampleHttpClient()
    signInExampleHttpAdmin(administrator, baseUrl)

    deviceA := liveExampleUnique("device-a")
    deviceB := liveExampleUnique("device-b")
    deviceJwt := liveExampleUnique("device-jwt")
    deviceJwtOther := liveExampleUnique("device-jwt-other")
    anonymous := bearer.call("device identity without a credential", liveExampleRequest{
        method:     "GET",
        path:       "/device/identity/",
        headerList: map[string]string{"Accept": "application/json"},
    })
    if http.StatusUnauthorized != anonymous.statusCode {
        fail(
            "GET /device/identity/ without a credential answered %d, not 401 — a stateless json firewall did not match it, so every 200 below could be earned by something other than a bearer token",
            anonymous.statusCode,
        )
    }
    pass("the device route refuses an unauthenticated call with 401, so it is guarded by the opaque-token firewall")
    tokenA := issueExampleAccessToken(editor, baseUrl, deviceA)
    if deviceA != tokenA.DeviceIdentifier {
        fail("the issued token names device %q, expected %q", tokenA.DeviceIdentifier, deviceA)
    }
    if true == tokenA.IssuedAt.IsZero() {
        fail("the issued token carries no issue instant, so nothing downstream can compare it against a revocation boundary")
    }
    if time.Since(tokenA.IssuedAt) > time.Minute {
        fail("the issued token claims to have been issued at %v, which is not now — the store stamped it from the wrong clock", tokenA.IssuedAt)
    }

    assertExampleDeviceIdentity(bearer, tokenA.Token, http.StatusOK, "the freshly issued token")
    pass("a token issued for %q resolves the device route as %q", deviceA, exampleRevocationEditorIdentifier)
    storedBefore := readExampleStoredClaims(redisClient, tokenA.Token)
    if exampleRevocationEditorIdentifier != storedBefore.UserIdentifier || deviceA != storedBefore.DeviceIdentifier {
        fail(
            "the stored entry says user=%q device=%q, expected %q/%q — the store dropped a field the revocation is decided on",
            storedBefore.UserIdentifier, storedBefore.DeviceIdentifier, exampleRevocationEditorIdentifier, deviceA,
        )
    }
    if false == storedBefore.IssuedAt.Equal(tokenA.IssuedAt) {
        fail("the stored issue instant %v differs from the one the route reported, %v", storedBefore.IssuedAt, tokenA.IssuedAt)
    }

    revokedA := revokeExampleDevice(editor, baseUrl, deviceA)
    if false == revokedA.RevokedBefore.After(tokenA.IssuedAt) {
        fail(
            "the boundary %v is not after the token's issue instant %v, so refusing the token would prove nothing about the boundary",
            revokedA.RevokedBefore, tokenA.IssuedAt,
        )
    }

    assertExampleDeviceIdentity(bearer, tokenA.Token, http.StatusUnauthorized, "the revoked token")
    assertExampleTokenKeyPresent(redisClient, tokenA.Token, "revoked device token")
    storedEpochA := readExampleEpoch(redisClient, exampleRevocationEditorIdentifier, deviceA)
    if storedEpochA != revokedA.RevokedBefore.UnixNano() {
        fail(
            "redis holds the boundary %d for device %q but the route reported %d — the refusal is not tied to the value RevokeBefore wrote",
            storedEpochA, deviceA, revokedA.RevokedBefore.UnixNano(),
        )
    }
    tokenA2 := issueExampleAccessToken(editor, baseUrl, deviceA)
    assertExampleDeviceIdentity(bearer, tokenA2.Token, http.StatusOK, "a token minted after the revocation on the same device")

    pass("a revoked token is refused while its entry is still in redis, the boundary matches what the route reported, and a token minted after it works")
    plantedAt := time.Now()
    if false == plantedAt.After(revokedA.RevokedBefore) {
        fail("the harness reached the planting step before the boundary instant, so it cannot construct the case it exists to check")
    }

    tokenPast := liveExampleUnique("planted-before")
    tokenFuture := liveExampleUnique("planted-after")
    defer deleteExampleTokenKeys(redisClient, tokenPast, tokenFuture)

    plantExampleTokenEntry(redisClient, tokenPast, deviceA, revokedA.RevokedBefore.Add(-time.Second))
    plantExampleTokenEntry(redisClient, tokenFuture, deviceA, revokedA.RevokedBefore.Add(2*time.Second))

    assertExampleTokenKeyPresent(redisClient, tokenPast, "the entry planted with an instant before the boundary")
    assertExampleTokenKeyPresent(redisClient, tokenFuture, "the entry planted with an instant after the boundary")

    assertExampleDeviceIdentity(bearer, tokenPast, http.StatusUnauthorized, "an entry written after the revocation but stamped before it")
    assertExampleTokenKeyPresent(redisClient, tokenPast, "the refused planted entry")
    assertExampleDeviceIdentity(bearer, tokenFuture, http.StatusOK, "an entry identical to the refused one except for its issue instant")

    pass("an entry written into the store AFTER the revocation returned is still refused for predating it, while one differing only in its issue instant is honoured")
    tokenB := issueExampleAccessToken(editor, baseUrl, deviceB)
    revokedB := revokeExampleDevice(editor, baseUrl, deviceB)

    assertExampleDeviceIdentity(bearer, tokenB.Token, http.StatusUnauthorized, "the token of the second revoked device")
    assertExampleDeviceIdentity(bearer, tokenA2.Token, http.StatusOK, "the first device's token after the second device was revoked")

    assertExampleTokenKeyPresent(redisClient, tokenB.Token, "the second device's revoked token")
    assertExampleTokenKeyPresent(redisClient, tokenA2.Token, "the first device's surviving token")

    if unchanged := readExampleEpoch(redisClient, exampleRevocationEditorIdentifier, deviceA); unchanged != storedEpochA {
        fail(
            "revoking device %q moved device %q's boundary from %d to %d, so the boundaries are not per-device at all",
            deviceB, deviceA, storedEpochA, unchanged,
        )
    }

    if readExampleEpoch(redisClient, exampleRevocationEditorIdentifier, deviceB) != revokedB.RevokedBefore.UnixNano() {
        fail("the second device's boundary in redis does not match what the route reported")
    }

    pass("revoking one device refuses only that device's token, both entries still present, and the other device's boundary is untouched")
    runExampleJwtRevocationCheck(baseUrl, bearer, editor, deviceJwt, deviceJwtOther)
    if editorForbidden := postExampleJson(editor, baseUrl, "/access-token/revoke/user/", map[string]any{
        "userIdentifier": exampleRevocationEditorIdentifier,
    }); http.StatusForbidden != editorForbidden.statusCode {
        fail(
            "an editor revoking a named user answered %d, expected 403 — an administrator-only route is reachable by an editor",
            editorForbidden.statusCode,
        )
    }

    tokenBFresh := issueExampleAccessToken(editor, baseUrl, deviceB)

    revokeExampleUser(administrator, baseUrl, exampleRevocationEditorIdentifier)

    assertExampleDeviceIdentity(bearer, tokenA2.Token, http.StatusUnauthorized, "the first device's token after a user-wide revocation")
    assertExampleDeviceIdentity(bearer, tokenBFresh.Token, http.StatusUnauthorized, "the second device's token after a user-wide revocation")

    assertExampleTokenKeyPresent(redisClient, tokenA2.Token, "the first device's token after a user-wide revocation")
    assertExampleTokenKeyPresent(redisClient, tokenBFresh.Token, "the second device's token after a user-wide revocation")

    tokenAfterUserRevocation := issueExampleAccessToken(editor, baseUrl, deviceA)
    assertExampleDeviceIdentity(bearer, tokenAfterUserRevocation.Token, http.StatusOK, "a token minted after the user-wide revocation")

    pass("a user-wide revocation refuses every device's token while every entry is still in redis — nothing enumerated or deleted anything")
}

func runExampleJwtRevocationCheck(
    baseUrl string,
    bearer *liveExampleClient,
    editor *http.Client,
    deviceJwt string,
    otherDevice string,
) {
    revocable, _ := runExampleMintCommand(
        "opaque token revocation",
        "auth:token",
        "--user", exampleRevocationEditorIdentifier,
        "--role", "ROLE_USER",
        "--device", deviceJwt,
    )
    revocableToken := exampleMintedToken("opaque token revocation", revocable)

    survivor, _ := runExampleMintCommand(
        "opaque token revocation",
        "auth:token",
        "--user", exampleRevocationEditorIdentifier,
        "--role", "ROLE_USER",
        "--device", otherDevice,
    )
    survivorToken := exampleMintedToken("opaque token revocation", survivor)

    assertExampleSecureIdentity(bearer, revocableToken, http.StatusOK, "a freshly minted json web token")
    time.Sleep(1100 * time.Millisecond)
    revokeExampleDevice(editor, baseUrl, deviceJwt)

    assertExampleSecureIdentity(bearer, revocableToken, http.StatusUnauthorized, "a json web token issued before its device's boundary")

    expiry := exampleJwtExpiry(revocableToken)
    if false == expiry.After(time.Now()) {
        fail("the refused json web token had already expired at %v, so the 401 says nothing about revocation", expiry)
    }

    assertExampleSecureIdentity(bearer, survivorToken, http.StatusOK, "a json web token minted for another device before the revocation")

    time.Sleep(1100 * time.Millisecond)

    replacement, _ := runExampleMintCommand(
        "opaque token revocation",
        "auth:token",
        "--user", exampleRevocationEditorIdentifier,
        "--role", "ROLE_USER",
        "--device", deviceJwt,
    )

    assertExampleSecureIdentity(bearer, exampleMintedToken("opaque token revocation", replacement), http.StatusOK, "a json web token minted for the same device after the revocation")

    pass("a signed, unexpired json web token is refused once its device's boundary is published, while one minted afterwards for the same device works")
}

func assertExampleDeviceIdentity(bearer *liveExampleClient, tokenString string, expected int, what string) {
    response := bearer.call("device identity", liveExampleRequest{
        method: "GET",
        path:   "/device/identity/",
        headerList: map[string]string{
            "Accept":        "application/json",
            "Authorization": "Bearer " + tokenString,
        },
    })

    if expected != response.statusCode {
        fail("GET /device/identity/ with %s answered %d, expected %d: %s", what, response.statusCode, expected, response.bodyText())
    }

    if http.StatusOK != expected {
        return
    }
    payload := struct {
        UserIdentifier string `json:"userIdentifier"`
    }{}
    decodeLiveExamplePayload("opaque token revocation", response, &payload)

    if exampleRevocationEditorIdentifier != payload.UserIdentifier {
        fail(
            "GET /device/identity/ with %s authenticated as %q, expected %q — a 200 alone would not have said which credential got in",
            what, payload.UserIdentifier, exampleRevocationEditorIdentifier,
        )
    }
}

func assertExampleSecureIdentity(bearer *liveExampleClient, tokenString string, expected int, what string) {
    response := bearer.call("secure identity", liveExampleRequest{
        method: "GET",
        path:   "/secure/me/",
        headerList: map[string]string{
            "Accept":        "application/json",
            "Authorization": "Bearer " + tokenString,
        },
    })

    if expected != response.statusCode {
        fail("GET /secure/me/ with %s answered %d, expected %d: %s", what, response.statusCode, expected, response.bodyText())
    }
}

func issueExampleAccessToken(client *http.Client, baseUrl string, deviceIdentifier string) exampleIssuedToken {
    response := postExampleJson(client, baseUrl, "/access-token/issue/", map[string]any{
        "deviceIdentifier": deviceIdentifier,
    })

    if http.StatusCreated != response.statusCode && http.StatusOK != response.statusCode {
        fail("POST /access-token/issue/ answered %d: %s", response.statusCode, response.body)
    }

    issued := exampleIssuedToken{}
    if decodeErr := json.Unmarshal([]byte(response.body), &issued); nil != decodeErr {
        fail("POST /access-token/issue/ did not answer json: %v (%s)", decodeErr, response.body)
    }

    if "" == issued.Token {
        fail("POST /access-token/issue/ answered without a token: %s", response.body)
    }

    return issued
}

func revokeExampleDevice(client *http.Client, baseUrl string, deviceIdentifier string) exampleRevocation {
    return decodeExampleRevocation(postExampleJson(client, baseUrl, "/access-token/revoke/device/", map[string]any{
        "deviceIdentifier": deviceIdentifier,
    }), "/access-token/revoke/device/")
}

func revokeExampleUser(client *http.Client, baseUrl string, userIdentifier string) exampleRevocation {
    return decodeExampleRevocation(postExampleJson(client, baseUrl, "/access-token/revoke/user/", map[string]any{
        "userIdentifier": userIdentifier,
    }), "/access-token/revoke/user/")
}

func decodeExampleRevocation(response exampleJsonResponse, route string) exampleRevocation {
    if http.StatusOK != response.statusCode {
        fail("POST %s answered %d: %s", route, response.statusCode, response.body)
    }

    revoked := exampleRevocation{}
    if decodeErr := json.Unmarshal([]byte(response.body), &revoked); nil != decodeErr {
        fail("POST %s did not answer json: %v (%s)", route, decodeErr, response.body)
    }

    if true == revoked.RevokedBefore.IsZero() {
        fail("POST %s reported no boundary, so nothing was published: %s", route, response.body)
    }

    return revoked
}

type exampleJsonResponse struct {
    statusCode int
    body       string
}

func postExampleJson(client *http.Client, baseUrl string, path string, payload map[string]any) exampleJsonResponse {
    body, marshalErr := json.Marshal(payload)
    if nil != marshalErr {
        fail("opaque token revocation: encode the request body: %v", marshalErr)
    }

    request, requestErr := http.NewRequest("POST", strings.TrimRight(baseUrl, "/")+path, strings.NewReader(string(body)))
    if nil != requestErr {
        fail("opaque token revocation: build the request for %s: %v", path, requestErr)
    }

    request.Header.Set("Content-Type", "application/json")
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("opaque token revocation: %s: %v", path, responseErr)
    }
    defer response.Body.Close()

    read, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("opaque token revocation: read the response of %s: %v", path, readErr)
    }

    return exampleJsonResponse{statusCode: response.StatusCode, body: string(read)}
}

func assertExampleTokenKeyPresent(redisClient rueidis.Client, tokenString string, what string) {
    exists, existsErr := redisClient.Do(
        context.Background(),
        redisClient.B().Exists().Key(exampleTokenStoreKeyPrefix+tokenString).Build(),
    ).AsInt64()
    if nil != existsErr {
        fail("opaque token revocation: ask redis for %s out of band: %v", what, existsErr)
    }

    if 1 != exists {
        fail(
            "%s is gone from redis, so the 401 proves the entry was DELETED, not that the epoch refused it — this section asserts nothing about the revocation epoch",
            what,
        )
    }
}

func readExampleStoredClaims(redisClient rueidis.Client, tokenString string) exampleStoredClaims {
    payload, payloadErr := redisClient.Do(
        context.Background(),
        redisClient.B().Get().Key(exampleTokenStoreKeyPrefix+tokenString).Build(),
    ).ToString()
    if nil != payloadErr {
        fail("opaque token revocation: read the stored entry out of band: %v", payloadErr)
    }

    stored := exampleStoredClaims{}
    if decodeErr := json.Unmarshal([]byte(payload), &stored); nil != decodeErr {
        fail("opaque token revocation: the stored entry is not json: %v (%s)", decodeErr, payload)
    }

    return stored
}

func readExampleEpoch(redisClient rueidis.Client, userIdentifier string, deviceIdentifier string) int64 {
    field := exampleRevocationUserField
    if "" != deviceIdentifier {
        field = exampleRevocationDeviceFieldPre + deviceIdentifier
    }

    value, valueErr := redisClient.Do(
        context.Background(),
        redisClient.B().Hget().Key(exampleTokenStoreEpochKey+userIdentifier).Field(field).Build(),
    ).ToString()
    if nil != valueErr {
        return 0
    }

    parsed, parseErr := strconv.ParseInt(value, 10, 64)
    if nil != parseErr {
        fail("opaque token revocation: the boundary %q in redis is not a decimal instant", value)
    }

    return parsed
}

func plantExampleTokenEntry(redisClient rueidis.Client, tokenString string, deviceIdentifier string, issuedAt time.Time) {
    payload, marshalErr := json.Marshal(exampleStoredClaims{
        UserIdentifier:   exampleRevocationEditorIdentifier,
        Roles:            []string{"ROLE_USER"},
        DeviceIdentifier: deviceIdentifier,
        IssuedAt:         issuedAt,
    })
    if nil != marshalErr {
        fail("opaque token revocation: encode the planted entry: %v", marshalErr)
    }

    if setErr := redisClient.Do(
        context.Background(),
        redisClient.B().Set().Key(exampleTokenStoreKeyPrefix+tokenString).Value(string(payload)).Px(5*time.Minute).Build(),
    ).Error(); nil != setErr {
        fail("opaque token revocation: plant an entry out of band: %v", setErr)
    }
}

func deleteExampleTokenKeys(redisClient rueidis.Client, tokenStringList ...string) {
    for _, tokenString := range tokenStringList {
        _ = redisClient.Do(
            context.Background(),
            redisClient.B().Del().Key(exampleTokenStoreKeyPrefix+tokenString).Build(),
        ).Error()
    }
}

func exampleJwtExpiry(tokenString string) time.Time {
    parts := strings.Split(tokenString, ".")
    if 3 != len(parts) {
        fail("opaque token revocation: the minted json web token does not have three parts")
    }

    payload, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != decodeErr {
        fail("opaque token revocation: the json web token payload is not base64url: %v", decodeErr)
    }

    claims := struct {
        Expiry int64 `json:"exp"`
    }{}
    if unmarshalErr := json.Unmarshal(payload, &claims); nil != unmarshalErr {
        fail("opaque token revocation: the json web token payload is not json: %v", unmarshalErr)
    }

    if 0 == claims.Expiry {
        fail("opaque token revocation: the minted json web token carries no exp, so an expiry cannot be ruled out")
    }

    return time.Unix(claims.Expiry, 0)
}
