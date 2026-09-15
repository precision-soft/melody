package twofactor

import (
    "context"
    "bytes"
    "database/sql"
    "os"
    "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    store2fa "github.com/precision-soft/melody/v3/.example/twofactor"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func TestEnrollHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/enroll?user=admin")

    response, handlerErr := EnrollHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the enrollment door", response, handlerErr)
}

func TestVerifyHandlerRefusesACallerWithoutAToken(t *testing.T) {
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/verify?user=admin")

    response, handlerErr := VerifyHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    assertTwoFactorUnauthorized(t, "the verification door", response, handlerErr)
}

func TestTwoFactorHandlersUseAuthenticatedIdentityOnMySQL(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN is not set")
    }
    driverConfig, err := mysql.ParseDSN(dsn)
    if nil != err {
        t.Fatal("invalid MYSQL_DSN")
    }
    driverConfig.ParseTime = true
    connection, err := sql.Open("mysql", driverConfig.FormatDSN())
    if nil != err {
        t.Fatal(err)
    }
    connection.SetMaxOpenConns(1)
    database := bun.NewDB(connection, mysqldialect.New())
    defer database.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    if _, err := database.ExecContext(ctx, `CREATE TEMPORARY TABLE melody_example_v3_two_factor (
        user_identifier VARCHAR(100) PRIMARY KEY,
        secret VARBINARY(512) NOT NULL,
        recovery_codes VARBINARY(2048) NOT NULL,
        created_at DATETIME(6) NOT NULL
    )`); nil != err {
        t.Fatal(err)
    }
    melodyencrypt.UseCipher(melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider("test", map[string][]byte{"test": bytes.Repeat([]byte{37}, 32)})))
    defer melodyencrypt.UseCipher(nil)
    store := store2fa.NewStore(database)
    request, runtimeInstance := twoFactorRequest(t, "/twofactor/enroll?user=victim")
    token := melodysecurity.NewAuthenticatedToken("caller", []string{"ROLE_USER"})
    firewall := melodysecurity.NewCompiledFirewall(
        "main", melodysecurity.NewPathPrefixMatcher("/"), "", nil,
        melodysecurity.NewResolverTokenSource(func(request melodyhttpcontract.Request) securitycontract.Token { return token }),
        melodysecurity.NewAccessControl(), melodysecurity.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, melodysecurity.NewRoleVoter()), nil, nil, nil,
        "", "", nil, nil,
        melodysecurity.SourceNone, melodysecurity.SourceFirewall, melodysecurity.SourceFirewall, melodysecurity.SourceNone, melodysecurity.SourceNone,
    )
    melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, token))
    victimSecret, _, victimCodes, err := store.Enroll(ctx, "victim", "test")
    if nil != err {
        t.Fatal(err)
    }
    response, err := EnrollHandler(store)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("enroll: response=%v error=%v", response, err)
    }
    if _, found, err := store.FindTotpSecret(runtimeInstance, "caller"); nil != err || false == found {
        t.Fatalf("caller not enrolled: found=%v error=%v", found, err)
    }
    if secret, found, err := store.FindTotpSecret(runtimeInstance, "victim"); nil != err || false == found || victimSecret != secret {
        t.Fatalf("victim enrollment changed: found=%v error=%v", found, err)
    }
    request.HttpRequest().Header.Set(melodysecurity.DefaultTotpRecoveryHeaderName, victimCodes[0])
    response, err = VerifyHandler(store)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err || nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("victim code accepted as caller: response=%v error=%v", response, err)
    }
    if redeemed, err := store.RedeemRecoveryCode(runtimeInstance, "victim", victimCodes[0]); nil != err || false == redeemed {
        t.Fatalf("victim code consumed by caller: redeemed=%v error=%v", redeemed, err)
    }
}
