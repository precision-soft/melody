package main

import (
    "net/http"
    "os"
    "regexp"
    "strings"
)

const userCreateLabel = "console account creation"

/* exampleCreatedUserPattern reads the identifier example:user:create prints for the account it wrote */
var exampleCreatedUserPattern = regexp.MustCompile(`created user "[^"]+" \(([^)]+)\)`)

/* assertConsoleCreatedAccountSignsIn drives the door the first administrator of a deployment comes through, since outside development the directory starts empty: example:user:create writes an account whose password it read from standard input, and that account signs in over http with it. A password passed as an argument is refused before anything is written. The account leaves through the admin door with its audit trail, on a failure too. */
func assertConsoleCreatedAccountSignsIn(administrator *http.Client, baseUrl string) {
    mysqlDsn := os.Getenv("MYSQL_DSN")
    if "" == mysqlDsn {
        skip("%s: MYSQL_DSN is cleared, so the created account's audit trail could not be removed with it — the console creation was NOT checked", userCreateLabel)

        return
    }

    database := openMysql(userCreateLabel, mysqlDsn)
    defer database.Close()

    username := liveExampleUnique("console-operator")
    password := "console-chosen-password"

    refusedCommand := runExampleMintCommandExpectingRefusal(userCreateLabel, "", "example:user:create", "--role", "ROLE_EDITOR", username, password)
    if false == strings.Contains(refusedCommand, "never from an argument") {
        fail("%s: a password passed as an argument was not refused: %s", userCreateLabel, exampleTail(refusedCommand, 5))
    }

    output := runExampleMintCommandWithInput(userCreateLabel, password+"\n", "example:user:create", "--role", "ROLE_EDITOR", username)
    match := exampleCreatedUserPattern.FindStringSubmatch(output)
    if 2 != len(match) {
        fail("%s: example:user:create printed no created account: %s", userCreateLabel, exampleTail(output, 5))
    }

    userId := match[1]

    removeOnFailure := pushFailureCleanup(func() {
        deleteExampleAccount(administrator, baseUrl, userId)
        removeExampleV3AuditTrail(userCreateLabel, database, "user", userId)
    })
    defer removeOnFailure()

    owner := newExampleHttpClient()
    signInExampleHttp(owner, baseUrl, "", username, password)

    deleteExampleAccount(administrator, baseUrl, userId)
    removeExampleV3AuditTrail(userCreateLabel, database, "user", userId)

    pass("example:user:create wrote %q (%s) with the password read from standard input, the account signed in with it, and a password passed as an argument was refused", username, userId)
}

/* deleteExampleAccount removes an account through the admin door, the door that publishes its deletion */
func deleteExampleAccount(administrator *http.Client, baseUrl string, userId string) {
    request, requestErr := http.NewRequest("DELETE", strings.TrimRight(baseUrl, "/")+"/users/api/delete/"+userId+"/", nil)
    if nil != requestErr {
        fail("%s: build the account deletion: %v", userCreateLabel, requestErr)
    }

    request.Header.Set("Accept", "application/json")

    deleted, deleteErr := administrator.Do(request)
    if nil != deleteErr {
        fail("%s: delete the account %q: %v", userCreateLabel, userId, deleteErr)
    }
    _ = deleted.Body.Close()

    if http.StatusOK != deleted.StatusCode {
        fail("%s: deleting the account %q answered %d", userCreateLabel, userId, deleted.StatusCode)
    }
}
