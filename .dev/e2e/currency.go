package main

import (
    "context"
    "database/sql"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/uptrace/bun"
)

const (
    exampleV3CurrencyTable = "melody_example_v3_currency"

    mysqlCurrencyProbeId      = "cur-e2e-probe"
    mysqlCurrencyProbeName    = "e2e currency probe"
    mysqlCurrencyProbeRenamed = "e2e currency probe renamed"
)

type exampleCurrency struct {
    Id   string  `json:"id"`
    Code string  `json:"code"`
    Name string  `json:"name"`
    Rate float64 `json:"rate"`
}

/* assertMysqlCurrencyWrites drives the three currency write doors as the administrator and reads each outcome out of band: the row the create wrote, the name the rename wrote with the rate kept, and the row gone after the delete, and a second create on the probe's identifier refused with 409 and the row left as it was; the listing is read after the create and after the delete, since it is what a client sees. A plain user is refused at the create by the access rule. Currencies are not audited, so no trail is asserted. */
func assertMysqlCurrencyWrites(client *http.Client, baseUrl string, database *bun.DB) {
    removeMysqlCurrencyProbe(database)
    defer removeMysqlCurrencyProbe(database)

    plain := newSignedInLiveExampleClient(baseUrl, "user", "user")
    refused := plain.call(mysqlLabel, liveExampleRequest{
        method:      "POST",
        path:        "/currencies/api/create/",
        contentType: "application/json",
        body:        []byte(`{"id":"` + mysqlCurrencyProbeId + `","code":"ZZE","name":"` + mysqlCurrencyProbeName + `","rate":2.5}`),
    })
    requireLiveExampleStatus(mysqlLabel, "a currency create by a plain user", refused, http.StatusForbidden)
    if _, found := readMysqlCurrencyProbe(database); true == found {
        fail("%s: the create refused to a plain user wrote the currency", mysqlLabel)
    }

    createPayload := requireMysqlWrite(client, "POST", baseUrl, "/currencies/api/create/", `{"id":"`+mysqlCurrencyProbeId+`","code":"ZZE","name":"`+mysqlCurrencyProbeName+`","rate":2.5}`, "create the currency probe")
    created := exampleCurrency{}
    decodeExampleData(mysqlLabel, "the currency probe", createPayload, &created)
    if mysqlCurrencyProbeId != created.Id || "ZZE" != created.Code || 2.5 != created.Rate {
        fail("%s: the currency create answered %+v, wanted the probe as sent", mysqlLabel, created)
    }

    stored, found := readMysqlCurrencyProbe(database)
    if false == found || mysqlCurrencyProbeName != stored.Name || 2.5 != stored.Rate {
        fail("%s: the currency create wrote %+v (found %v), wanted the probe as sent", mysqlLabel, stored, found)
    }

    if false == mysqlCurrencyListed(client, baseUrl) {
        fail("%s: the listing does not carry the created currency %s", mysqlLabel, mysqlCurrencyProbeId)
    }

    conflictStatus, conflictBody := mysqlWriteStatus(client, "POST", baseUrl, "/currencies/api/create/", `{"id":"`+mysqlCurrencyProbeId+`","code":"ZZF","name":"`+mysqlCurrencyProbeRenamed+`","rate":3.5}`, "create a second currency on the probe's identifier")
    if http.StatusConflict != conflictStatus || false == strings.Contains(conflictBody, "id already exists") {
        fail("%s: a create on a taken identifier answered %d: %s, wanted 409 naming the identifier", mysqlLabel, conflictStatus, exampleTruncate(conflictBody))
    }

    kept, found := readMysqlCurrencyProbe(database)
    if false == found || "ZZE" != kept.Code || 2.5 != kept.Rate {
        fail("%s: the refused create on a taken identifier left %+v (found %v), wanted the probe untouched", mysqlLabel, kept, found)
    }

    requireMysqlWrite(client, "PUT", baseUrl, "/currencies/api/update/"+mysqlCurrencyProbeId+"/", `{"code":"ZZE","name":"`+mysqlCurrencyProbeRenamed+`"}`, "rename the currency probe")

    renamed, found := readMysqlCurrencyProbe(database)
    if false == found || mysqlCurrencyProbeRenamed != renamed.Name || 2.5 != renamed.Rate {
        fail("%s: the currency rename left %+v (found %v), wanted the new name and the rate kept", mysqlLabel, renamed, found)
    }

    requireMysqlWrite(client, "DELETE", baseUrl, "/currencies/api/delete/"+mysqlCurrencyProbeId+"/", "", "delete the currency probe")

    if _, found := readMysqlCurrencyProbe(database); true == found {
        fail("%s: the currency delete answered success and the row is still there", mysqlLabel)
    }

    if true == mysqlCurrencyListed(client, baseUrl) {
        fail("%s: the listing still carries the deleted currency %s", mysqlLabel, mysqlCurrencyProbeId)
    }
}

func mysqlCurrencyListed(client *http.Client, baseUrl string) bool {
    payload := requireMysqlWrite(client, "GET", baseUrl, "/currencies/api/read/", "", "list the currencies")

    currencies := make([]exampleCurrency, 0)
    decodeExampleData(mysqlLabel, "the currency listing", payload, &currencies)

    for _, currency := range currencies {
        if mysqlCurrencyProbeId == currency.Id {
            return true
        }
    }

    return false
}

func readMysqlCurrencyProbe(database *bun.DB) (exampleCurrency, bool) {
    row := exampleCurrency{}

    selectErr := database.
        NewSelect().
        ColumnExpr("id AS id").
        ColumnExpr("code AS code").
        ColumnExpr("name AS name").
        ColumnExpr("rate AS rate").
        Table(exampleV3CurrencyTable).
        Where("id = ?", mysqlCurrencyProbeId).
        Scan(context.Background(), &row.Id, &row.Code, &row.Name, &row.Rate)
    if true == errors.Is(selectErr, sql.ErrNoRows) {
        return row, false
    }
    if nil != selectErr {
        fail("%s: read the currency probe out of band: %v", mysqlLabel, selectErr)
    }

    return row, true
}

func removeMysqlCurrencyProbe(database *bun.DB) {
    _, deleteErr := database.
        NewDelete().
        Table(exampleV3CurrencyTable).
        Where("id = ?", mysqlCurrencyProbeId).
        Exec(context.Background())
    if nil != deleteErr {
        fail("%s: remove the currency probe: %v", mysqlLabel, deleteErr)
    }
}

/* mysqlWriteStatus performs one write and answers its status and body, for the refusals requireMysqlWrite turns into failures */
func mysqlWriteStatus(client *http.Client, method string, baseUrl string, path string, body string, what string) (int, string) {
    request, requestErr := http.NewRequest(method, strings.TrimRight(baseUrl, "/")+path, strings.NewReader(body))
    if nil != requestErr {
        fail("%s: build the request to %s: %v", mysqlLabel, what, requestErr)
    }

    request.Header.Set("Content-Type", "application/json")
    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("%s: %s: %v", mysqlLabel, what, responseErr)
    }
    defer response.Body.Close()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("%s: read the response to %s: %v", mysqlLabel, what, readErr)
    }

    return response.StatusCode, string(payload)
}
