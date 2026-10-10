package main

import (
    "context"
    "net/http"

    "github.com/uptrace/bun"
)

/* assertMysqlCatalogueIntegrity drives the catalogue's integrity rules through the doors and reads the outcome out of band: a second currency under a taken code is refused 409 and the code keeps one row; a currency a product is priced in is refused 409 and stays; a minted currency deleted is not minted again; a product naming a currency that does not exist is refused 400; and an identifier the database would find under a padded spelling answers 404, since a copy cached under that spelling would outlive every update. */
func assertMysqlCatalogueIntegrity(client *http.Client, baseUrl string, database *bun.DB) {
    defer removeMysqlCatalogueIntegrityProbes(database)

    status, body := mysqlWriteStatus(client, "POST", baseUrl, "/currencies/api/create/", `{"code":"EUR","name":"Euro bis","rate":2}`, "create a second currency under the code EUR")
    if http.StatusConflict != status {
        fail("%s: a second currency under a taken code answered %d: %s, wanted 409", mysqlLabel, status, exampleTruncate(body))
    }

    if holders := mysqlCount(database, "SELECT COUNT(*) FROM "+exampleV3CurrencyTable+" WHERE code = 'EUR'"); 1 != holders {
        fail("%s: the code EUR is held by %d rows after the refused create, wanted 1", mysqlLabel, holders)
    }

    status, body = mysqlWriteStatus(client, "DELETE", baseUrl, "/currencies/api/delete/cur-ron/", "", "delete a currency a seeded product is priced in")
    if http.StatusConflict != status {
        fail("%s: the delete of a priced currency answered %d: %s, wanted 409", mysqlLabel, status, exampleTruncate(body))
    }

    if present := mysqlCount(database, "SELECT COUNT(*) FROM "+exampleV3CurrencyTable+" WHERE id = 'cur-ron'"); 1 != present {
        fail("%s: the refused delete removed cur-ron", mysqlLabel)
    }

    firstId := createMysqlCatalogueIntegrityProbe(client, baseUrl, "the first minted probe")
    requireMysqlWrite(client, "DELETE", baseUrl, "/currencies/api/delete/"+firstId+"/", "", "delete the first minted probe")

    secondId := createMysqlCatalogueIntegrityProbe(client, baseUrl, "the second minted probe")
    if firstId == secondId {
        fail("%s: the deleted currency's identifier %s was minted again", mysqlLabel, firstId)
    }

    requireMysqlWrite(client, "DELETE", baseUrl, "/currencies/api/delete/"+secondId+"/", "", "delete the second minted probe")

    status, body = mysqlWriteStatus(client, "POST", baseUrl, "/products/api/create/", `{"name":"Integrity probe","description":"d","categoryId":"cat-1","price":1,"currencyId":"cur-absent","stock":1}`, "create a product priced in a currency that does not exist")
    if http.StatusBadRequest != status {
        fail("%s: a product priced in a missing currency answered %d: %s, wanted 400", mysqlLabel, status, exampleTruncate(body))
    }

    status, body = mysqlWriteStatus(client, "GET", baseUrl, "/products/api/read/prod-1%20/", "", "read a product under a padded identifier")
    if http.StatusNotFound != status {
        fail("%s: the padded identifier answered %d: %s, wanted 404", mysqlLabel, status, exampleTruncate(body))
    }
}

/* createMysqlCatalogueIntegrityProbe creates a currency with no identifier, so the repository mints one, and answers it */
func createMysqlCatalogueIntegrityProbe(client *http.Client, baseUrl string, what string) string {
    payload := requireMysqlWrite(client, "POST", baseUrl, "/currencies/api/create/", `{"code":"ZZG","name":"Integrity probe","rate":1}`, "create "+what)

    created := exampleCurrency{}
    decodeExampleData(mysqlLabel, what, payload, &created)

    if "" == created.Id {
        fail("%s: %s answered no identifier", mysqlLabel, what)
    }

    return created.Id
}

func removeMysqlCatalogueIntegrityProbes(database *bun.DB) {
    if _, deleteErr := database.NewDelete().Table(exampleV3CurrencyTable).Where("code = ?", "ZZG").Exec(context.Background()); nil != deleteErr {
        fail("%s: remove the integrity probes: %v", mysqlLabel, deleteErr)
    }
}

func mysqlCount(database *bun.DB, statement string) int {
    count := 0
    if scanErr := database.NewRaw(statement).Scan(context.Background(), &count); nil != scanErr {
        fail("%s: %s: %v", mysqlLabel, statement, scanErr)
    }

    return count
}
