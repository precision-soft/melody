package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/uptrace/bun"
)

/* exampleConcurrentCreationCount is the number of creates started together: every one of them reads the identifier list before any commits, so without the lock around mint and insert they mint one identifier and the primary key refuses all but one */
const exampleConcurrentCreationCount = 20

const (
    exampleAdminUsername = "admin"
    exampleAdminPassword = "admin"
)

/* assertExampleCreatesConcurrentlyWithDistinctIdentifiers starts exampleConcurrentCreationCount user creates at once against the live application and requires every one answered 201 with an identifier of its own, then deletes each through the application, so its events clear the caches the sections after this one read. One create runs first, on its own: the first resolution of the services a create reaches is not what this section measures. */
func assertExampleCreatesConcurrentlyWithDistinctIdentifiers(major exampleMajor, redisAddress string, mysqlDsn string) {
    if "" == mysqlDsn {
        skip("[%s] MYSQL_DSN is cleared, so the application runs its in-memory repositories and the concurrent creation was not driven", major.label)

        return
    }
    if "" == redisAddress {
        skip("[%s] REDIS_ADDRESS is cleared, so the write budget cannot be reset around the burst and the concurrent creation was not driven", major.label)

        return
    }

    label := fmt.Sprintf("[%s] concurrent creation", major.label)
    rateLimitPrefix := exampleMajorRateLimitPrefix(major)

    client := newExampleClient(major)
    signedIn := client.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody(exampleAdminUsername, exampleAdminPassword))
    if http.StatusOK != signedIn.statusCode {
        fail("%s: the seeded admin could not sign in (%d): %s", label, signedIn.statusCode, exampleTruncate(signedIn.body))
    }

    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)
    defer resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)

    runTag := fmt.Sprintf("e2e-concurrent-%s-%d", major.label, time.Now().UnixNano()%1000000000)
    createdIdList := []string{}

    warmUpStatus, warmUpId := createExampleUser(client, runTag+"-warm")
    if http.StatusCreated != warmUpStatus {
        fail("%s: the warm-up create answered %d, wanted 201", label, warmUpStatus)
    }
    createdIdList = append(createdIdList, warmUpId)

    statusList := make([]int, exampleConcurrentCreationCount)
    idList := make([]string, exampleConcurrentCreationCount)
    start := make(chan struct{})
    var group sync.WaitGroup
    for index := 0; index < exampleConcurrentCreationCount; index++ {
        group.Add(1)
        go func(index int) {
            defer group.Done()
            <-start
            statusList[index], idList[index] = createExampleUser(client, fmt.Sprintf("%s-%d", runTag, index))
        }(index)
    }
    close(start)
    group.Wait()

    distinctIdList := map[string]struct{}{}
    refusedCount := 0
    for index, status := range statusList {
        if http.StatusCreated != status {
            refusedCount++

            continue
        }
        distinctIdList[idList[index]] = struct{}{}
        createdIdList = append(createdIdList, idList[index])
    }

    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)
    removeExampleUsers(label, client, mysqlDsn, major, createdIdList)

    if 0 < refusedCount {
        fail("%s: %d of %d concurrent creates were refused (statuses %v): the identifiers they minted collided", label, refusedCount, exampleConcurrentCreationCount, statusList)
    }
    if exampleConcurrentCreationCount != len(distinctIdList) {
        fail("%s: %d concurrent creates answered 201 with %d distinct identifiers", label, exampleConcurrentCreationCount, len(distinctIdList))
    }
    pass("%s: %d creates started together all answered 201 with distinct identifiers, and were removed", label, exampleConcurrentCreationCount)

    assertExampleHoldsOneUsernameAgainstConcurrentCreates(label, client, redisAddress, rateLimitPrefix, mysqlDsn, major, runTag+"-same")
}

/* exampleSameUsernameCreationCount creates of one username start together: every one of them passes the read that precedes the insert before any commits, so only the unique key on the folded username can hold the name */
const exampleSameUsernameCreationCount = 4

func assertExampleHoldsOneUsernameAgainstConcurrentCreates(label string, client *exampleClient, redisAddress string, rateLimitPrefix string, mysqlDsn string, major exampleMajor, username string) {
    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)

    statusList := make([]int, exampleSameUsernameCreationCount)
    idList := make([]string, exampleSameUsernameCreationCount)
    start := make(chan struct{})
    var group sync.WaitGroup
    for index := 0; index < exampleSameUsernameCreationCount; index++ {
        group.Add(1)
        go func(index int) {
            defer group.Done()
            <-start
            statusList[index], idList[index] = createExampleUser(client, username)
        }(index)
    }
    close(start)
    group.Wait()

    createdIdList := []string{}
    createdCount := 0
    refusedCount := 0
    for index, status := range statusList {
        switch status {
        case http.StatusCreated:
            createdCount++
            createdIdList = append(createdIdList, idList[index])
        case http.StatusBadRequest:
            refusedCount++
        }
    }

    resetExampleRateLimitCounters(label, redisAddress, rateLimitPrefix)
    removeExampleUsers(label, client, mysqlDsn, major, createdIdList)

    if 1 != createdCount || exampleSameUsernameCreationCount-1 != refusedCount {
        fail("%s: %d concurrent creates of one username answered %v, wanted one 201 and %d 400", label, exampleSameUsernameCreationCount, statusList, exampleSameUsernameCreationCount-1)
    }
    pass("%s: %d concurrent creates of one username answered one 201 and %d 400, and the account was removed", label, exampleSameUsernameCreationCount, exampleSameUsernameCreationCount-1)
}

func createExampleUser(client *exampleClient, username string) (int, string) {
    body := `{"username":"` + username + `","password":"concurrent-password","roles":["ROLE_USER"]}`
    response := client.call("POST", "/users/api/create/", "application/json", "application/json", body)

    created := struct {
        Payload struct {
            Id string `json:"id"`
        } `json:"payload"`
    }{}
    _ = json.Unmarshal([]byte(response.body), &created)

    return response.statusCode, created.Payload.Id
}

/* removeExampleUsers deletes each probe account through the application, and on the third major the audit trail its writes left, so a run leaves the trail as it found it */
func removeExampleUsers(label string, client *exampleClient, mysqlDsn string, major exampleMajor, idList []string) {
    var database *bun.DB
    if 3 == major.number {
        database = openMysql(label, mysqlDsn)
        defer database.Close()
    }

    for _, id := range idList {
        if "" == id {
            continue
        }

        removed := client.call("DELETE", "/users/api/delete/"+id+"/", "application/json", "", "")
        if http.StatusOK != removed.statusCode {
            fail("%s: removing the probe account %s answered %d: %s", label, id, removed.statusCode, exampleTruncate(removed.body))
        }

        if nil != database {
            removeExampleV3AuditTrail(label, database, "user", id)
        }
    }
}
