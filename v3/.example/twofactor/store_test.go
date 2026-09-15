package twofactor

import (
    "bytes"
    "context"
    "database/sql"
    "os"
    "strings"
    "testing"
    "time"
    mysql "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

func TestEnrollmentUpsertReplacesAnEnrollmentThatIsAlreadyThere(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    if false == strings.Contains(rendered, "DUPLICATE KEY UPDATE") {
        t.Fatalf(
            "expected a second enrollment to replace the first rather than collide with the primary key; got %q",
            rendered,
        )
    }
}

func TestEnrollmentUpsertReplacesTheRecoveryCodesWithTheSecret(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    for _, column := range []string{"secret = VALUES(secret)", "recovery_codes = VALUES(recovery_codes)", "created_at = VALUES(created_at)"} {
        if false == strings.Contains(rendered, column) {
            t.Fatalf("expected the replacement to write %s, got %q", column, rendered)
        }
    }
}

func TestEnrollmentDeleteIsKeyedOnTheAccountIdentifierAlone(t *testing.T) {
    rendered := NewStore(newRenderingDatabase()).enrollmentDelete("user-4").String()

    if false == strings.HasPrefix(rendered, "DELETE FROM `melody_example_v3_two_factor`") || false == strings.Contains(rendered, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", rendered)
    }
}

func TestStoreReenrollmentOnMySQL(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN is not set")
    }
    driverConfig, configErr := mysql.ParseDSN(dsn)
    if nil != configErr {
        t.Fatal("invalid MYSQL_DSN")
    }
    driverConfig.ParseTime = true
    connection, openErr := sql.Open("mysql", driverConfig.FormatDSN())
    if nil != openErr {
        t.Fatal(openErr)
    }
    connection.SetMaxOpenConns(1)
    database := bun.NewDB(connection, mysqldialect.New())
    defer database.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    _, createErr := database.ExecContext(ctx, `CREATE TEMPORARY TABLE melody_example_v3_two_factor (
        user_identifier VARCHAR(100) PRIMARY KEY,
        secret VARBINARY(512) NOT NULL,
        recovery_codes VARBINARY(2048) NOT NULL,
        created_at DATETIME(6) NOT NULL
    )`)
    if nil != createErr {
        t.Fatal(createErr)
    }
    melodyencrypt.UseCipher(melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider("test", map[string][]byte{"test": bytes.Repeat([]byte{37}, 32)})))
    defer melodyencrypt.UseCipher(nil)
    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(ctx, containerInstance.NewScope(), containerInstance)
    store := NewStore(database)
    oldSecret, _, oldCodes, enrollErr := store.Enroll(ctx, "member", "test")
    if nil != enrollErr {
        t.Fatal(enrollErr)
    }
    neighborSecret, _, _, neighborErr := store.Enroll(ctx, "neighbor", "test")
    if nil != neighborErr {
        t.Fatal(neighborErr)
    }
    newSecret, _, newCodes, replaceErr := store.Enroll(ctx, "member", "test")
    if nil != replaceErr {
        t.Fatal(replaceErr)
    }
    if oldSecret == newSecret || 0 == len(oldCodes) || 0 == len(newCodes) {
        t.Fatal("replacement did not mint a new factor")
    }
    secret, found, findErr := store.FindTotpSecret(runtimeInstance, "member")
    if nil != findErr || false == found || newSecret != secret {
        t.Fatalf("replacement lookup failed: found=%v err=%v", found, findErr)
    }
    if redeemed, redeemErr := store.RedeemRecoveryCode(runtimeInstance, "member", oldCodes[0]); nil != redeemErr || true == redeemed {
        t.Fatalf("old recovery code remained usable: redeemed=%v err=%v", redeemed, redeemErr)
    }
    for index, expected := range []bool{true, false} {
        redeemed, redeemErr := store.RedeemRecoveryCode(runtimeInstance, "member", newCodes[0])
        if nil != redeemErr || expected != redeemed {
            t.Fatalf("new recovery code attempt %d: redeemed=%v err=%v", index, redeemed, redeemErr)
        }
    }
    var storedSecret, storedCodes string
    if readErr := connection.QueryRowContext(ctx, "SELECT secret, recovery_codes FROM melody_example_v3_two_factor WHERE user_identifier = 'member'").Scan(&storedSecret, &storedCodes); nil != readErr {
        t.Fatal(readErr)
    }
    if strings.Contains(storedSecret, newSecret) || strings.Contains(storedCodes, newCodes[1]) {
        t.Fatal("plaintext factor persisted")
    }
    if deleted, deleteErr := store.DeleteEnrollment(runtimeInstance, "member"); nil != deleteErr || false == deleted {
        t.Fatalf("delete failed: deleted=%v err=%v", deleted, deleteErr)
    }
    if _, found, findErr := store.FindTotpSecret(runtimeInstance, "member"); nil != findErr || true == found {
        t.Fatalf("deleted enrollment found=%v err=%v", found, findErr)
    }
    if secret, found, findErr := store.FindTotpSecret(runtimeInstance, "neighbor"); nil != findErr || false == found || neighborSecret != secret {
        t.Fatalf("neighbor changed: found=%v err=%v", found, findErr)
    }
}
