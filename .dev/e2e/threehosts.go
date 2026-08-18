package main

import (
    "context"
    "io"
    "net/http"
    "strings"

    "github.com/uptrace/bun"
)

/* the hostnames the load balancer serves the three example applications under. They are the whole point of the
section: one development stack, three majors reachable at once, each under a name that says which it is. */
var threeHostCatalog = []struct {
    label    string
    hostName string
    table    string
    /* the major whose database holds that table: the three examples share the development mysql and no longer
       share a database in it, so each host's catalogue is read through a connection of its own */
    major int
}{
    {label: "v1", hostName: "v1-example.melody.localhost.precision-soft.com", table: "melody_example_v1_product", major: 1},
    {label: "v2", hostName: "v2-example.melody.localhost.precision-soft.com", table: "melody_example_v2_product", major: 2},
    {label: "v3", hostName: "example.melody.localhost.precision-soft.com", table: exampleV3ProductTable, major: 3},
}

/* the probe name carries the host it was written through, because the three majors number their products
independently: each seeded catalogue ends at prod-5, so all three hand out prod-6 next and an identifier alone
cannot say which application wrote a row. */
func threeHostProbeName(label string) string {
    return "e2e three-host probe " + label
}

/* runThreeHostsCheck proves the load balancer routes each hostname to a different application, and does it
where a status code cannot: all three answer 200 on every shared route, and v1 and v2 are the same application
on different framework majors, so nothing in the http surface tells them apart.

What does tell them apart is where their writes land. Each host is asked to create a product, and the harness
then reads the three catalogue tables straight out of mysql: the row must be in that major's table and in
neither of the others. A vhost pointed at the wrong upstream — or a load balancer still serving the
configuration it started with, which is what a mounted file that changed under a running nginx looks like —
fails here and passes every reachability check. */
func runThreeHostsCheck(loadBalancerUrl string, mysqlDsn string, redisAddress string) {
    if "" == mysqlDsn {
        skip("three hosts: MYSQL_DSN is cleared, so which application answered cannot be told apart out of band")

        return
    }

    if "" == redisAddress {
        skip("three hosts: REDIS_ADDRESS is cleared, so the write budget a previous section spent cannot be reset first")

        return
    }

    /* one connection per major, because one dsn no longer reaches all three catalogues. The cross-check below
       gets stronger for it: a row must be in its own major's database AND absent from the other two databases,
       where before it only had to be absent from two other tables of one. */
    databaseByLabel := map[string]*bun.DB{}
    for _, host := range threeHostCatalog {
        major, exists := exampleMajorByNumber(host.major)
        if false == exists {
            fail("three hosts: %s names major %d, which the harness does not know", host.hostName, host.major)
        }

        database := openMysql("three hosts "+host.label, exampleMysqlDsn(major, mysqlDsn))
        defer func() {
            _ = database.Close()
        }()

        databaseByLabel[host.label] = database
    }

    /* a run that failed midway leaves its probe behind, and the assertion below is about which catalogue a row
       is in — so every catalogue starts clean rather than carrying somebody else's answer */
    for _, host := range threeHostCatalog {
        for _, other := range threeHostCatalog {
            removeThreeHostProbe(databaseByLabel[other.label], other.table, host.label)
        }
    }

    for _, host := range threeHostCatalog {
        /* EXAMPLE OVER HTTP measures an exact exhaustion point of the v3 write budget just before this
           section runs, and it measures it through this same load balancer from this same address. Each
           host therefore starts from a budget of its own rather than from whatever is left of one. */
        resetExampleRateLimitCounters("three hosts", redisAddress, "melody-example-"+host.label+":rate_limit:")

        client := newExampleHttpClient()

        readiness := requestExample(client, "GET", loadBalancerUrl, "/routes/", host.hostName, "", "")
        if http.StatusOK != readiness {
            fail("three hosts: %s answered %d on /routes/ — the vhost reaches no application", host.hostName, readiness)
        }

        signInExampleHttpEditor(client, loadBalancerUrl, host.hostName)

        productId := createThreeHostProbe(client, loadBalancerUrl, host.hostName, host.label)

        for _, other := range threeHostCatalog {
            found := threeHostProbeExists(databaseByLabel[other.label], other.table, productId, host.label)

            if other.label == host.label && false == found {
                fail(
                    "three hosts: the product %s created through %s is not in %s — the vhost reached another major",
                    productId, host.hostName, other.table,
                )
            }

            if other.label != host.label && true == found {
                fail(
                    "three hosts: the product %s created through %s turned up in %s as well — two vhosts share one application",
                    productId, host.hostName, other.table,
                )
            }
        }

        removeThreeHostProbe(databaseByLabel[host.label], host.table, host.label)

        pass("[%s] %s reached the application that writes to %s, and no other", host.label, host.hostName, host.table)
    }
}

func createThreeHostProbe(client *http.Client, loadBalancerUrl string, hostName string, label string) string {
    body := `{"name":"` + threeHostProbeName(label) + `","description":"probe","categoryId":"cat-1","price":3,"currencyId":"cur-eur","stock":1}`

    request, requestErr := http.NewRequest(
        "POST",
        strings.TrimRight(loadBalancerUrl, "/")+"/products/api/create/",
        strings.NewReader(body),
    )
    if nil != requestErr {
        fail("three hosts: build the create request: %v", requestErr)
    }

    request.Host = hostName
    request.Header.Set("Content-Type", "application/json")
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("three hosts: create through %s: %v", hostName, responseErr)
    }
    defer response.Body.Close()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("three hosts: read the create response from %s: %v", hostName, readErr)
    }

    if http.StatusOK != response.StatusCode && http.StatusCreated != response.StatusCode {
        fail("three hosts: %s refused the probe write with %d: %s", hostName, response.StatusCode, string(payload))
    }

    created := exampleProduct{}
    decodeExampleData("three hosts", hostName, string(payload), &created)

    if "" == created.Id {
        fail("three hosts: %s accepted the probe write without reporting an identifier: %s", hostName, string(payload))
    }

    return created.Id
}

/* threeHostProbeExists answers whether the probe landed in one particular major's catalogue. A table that does
not exist holds nothing — that is the state of a major nobody has written to yet on a freshly created database,
and it is a correct answer to "is the row here" rather than a reason to fail. */
func threeHostProbeExists(database *bun.DB, table string, productId string, label string) bool {
    if false == exampleTableExists("three hosts", database, table) {
        return false
    }

    count, countErr := database.
        NewSelect().
        Table(table).
        Where("id = ?", productId).
        Where("name = ?", threeHostProbeName(label)).
        Count(context.Background())
    if nil != countErr {
        fail("three hosts: count %s in %s: %v", productId, table, countErr)
    }

    return 0 < count
}

/* removeThreeHostProbe removes the probe row and, in the v3 catalogue, everything the application recorded
about it. The v3 write is journalled and audited, and the example hands a freed identifier to the next probe, so
records left here would turn up in a later section reading "the trail of prod-6" as somebody else's history. */
func removeThreeHostProbe(database *bun.DB, table string, label string) {
    if exampleV3ProductTable == table {
        removeExampleV3ProductProbes("three hosts", database, []string{threeHostProbeName(label)})

        return
    }

    if false == exampleTableExists("three hosts", database, table) {
        return
    }

    _, deleteErr := database.
        NewDelete().
        Table(table).
        Where("name = ?", threeHostProbeName(label)).
        Exec(context.Background())
    if nil != deleteErr {
        fail("three hosts: the probe row this run created could not be removed from %s (%v)", table, deleteErr)
    }
}
