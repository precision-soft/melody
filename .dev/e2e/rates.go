package main

import (
    "encoding/json"
    "io"
    "net/http"
    "strings"
)

/* The conversion door is the reason the catalogue carries an exchange rate at all, and it is the only place
   in this repository where melody/v3/httpclient's work reaches a reader: the refresh writes the quotes
   through the client, and this door spends them. Nothing else imports that package on any major, so before
   this the whole of it was proven by compilation.

   Every assertion here is INDEPENDENT of what the quotes currently are, and deliberately so: whether the
   rates hold the values the application ships with or the ones a refresh last read depends on which driver
   ran before this one, and an assertion on a number would be an assertion about run order. What is asserted
   instead is what the arithmetic guarantees whatever the quotes say — converting into a product's own
   currency cannot change its price, because the rate divides out — plus the three answers the door owes a
   caller who names a currency badly. The exact numbers are pinned where they can be: in the package tests,
   against quotes the test states, and in the stack section, against a provider that answers a constant. */
func runExampleCurrencyConversionCheck(client *http.Client, baseUrl string) {
    document := readExampleProduct(client, baseUrl, "")
    if nil != document.Converted {
        fail("example currency: a read that named no currency carried a conversion anyway")
    }
    pass("example currency: a read that asks for no conversion carries no converted key")

    ownCode := exampleCurrencyCodeOf(document, document.Product.CurrencyId)
    if "" == ownCode {
        fail("example currency: the product is quoted in %q and the catalogue does not carry it", document.Product.CurrencyId)
    }

    /* the identity: amount / rate * rate is the amount, whatever the rate is. It is the one number this can
       assert without knowing the quotes, and it fails for every wrong arithmetic — a multiply where the
       divide belongs, a rate read off the wrong currency, a rounding applied twice. */
    sameCurrency := readExampleProduct(client, baseUrl, ownCode)
    if nil == sameCurrency.Converted {
        fail("example currency: converting into the product's own currency (%s) produced no conversion", ownCode)
    }
    if sameCurrency.Product.Price != sameCurrency.Converted.Price {
        fail(
            "example currency: converting %v %s into %s answered %v, and a conversion into a product's own currency cannot move its price",
            sameCurrency.Product.Price,
            ownCode,
            ownCode,
            sameCurrency.Converted.Price,
        )
    }
    pass("example currency: converting into the product's own currency leaves the price where it was (%v %s)", sameCurrency.Product.Price, ownCode)

    otherCode := exampleOtherCurrencyCode(sameCurrency, ownCode)
    if "" == otherCode {
        fail("example currency: the catalogue carries only one currency, so the conversion cannot be exercised")
    }

    converted := readExampleProduct(client, baseUrl, otherCode)
    if nil == converted.Converted {
        fail("example currency: converting into %s produced no conversion", otherCode)
    }
    if otherCode != converted.Converted.Code {
        fail("example currency: a read that asked for %s was answered a conversion into %s", otherCode, converted.Converted.Code)
    }
    if "" == converted.Converted.RateAsOf {
        fail("example currency: the conversion into %s carries no quote instant, so a reader cannot tell how old it is", otherCode)
    }
    pass("example currency: converting into %s answers a price stamped with the quote it used (%s)", otherCode, converted.Converted.RateAsOf)

    /* the shape of a query parameter is the client's to choose, and the string accessor beside the one this
       door uses panics on a repeated key — through a public door that is an unauthenticated five hundred */
    if http.StatusOK != exampleProductReadStatus(client, baseUrl, "?currency="+otherCode+"&currency="+ownCode) {
        fail("example currency: a repeated currency parameter was not answered with 200 — the door read it through an accessor that refuses a repeated key")
    }
    pass("example currency: a repeated currency parameter is answered rather than refused")

    if http.StatusBadRequest != exampleProductReadStatus(client, baseUrl, "?currency=XXX") {
        fail("example currency: a currency code the catalogue does not carry was not refused with 400")
    }
    pass("example currency: a currency code the catalogue does not carry is refused with 400")
}

type exampleProductDocument struct {
    Product struct {
        Price      float64 `json:"price"`
        CurrencyId string  `json:"currencyId"`
    } `json:"product"`
    Currencies []struct {
        Id   string `json:"id"`
        Code string `json:"code"`
    } `json:"currencies"`
    Converted *struct {
        Code     string  `json:"code"`
        Price    float64 `json:"price"`
        RateAsOf string  `json:"rateAsOf"`
    } `json:"converted"`
}

func readExampleProduct(client *http.Client, baseUrl string, currencyCode string) exampleProductDocument {
    query := ""
    if "" != currencyCode {
        query = "?currency=" + currencyCode
    }

    body, status := exampleProductReadBody(client, baseUrl, query)
    if http.StatusOK != status {
        fail("example currency: reading the product with %q answered %d", query, status)
    }

    var envelope struct {
        Payload exampleProductDocument `json:"payload"`
    }
    if decodeErr := json.Unmarshal([]byte(body), &envelope); nil != decodeErr {
        fail("example currency: the product read answered a body that is not the envelope: %v", decodeErr)
    }

    return envelope.Payload
}

func exampleProductReadStatus(client *http.Client, baseUrl string, query string) int {
    _, status := exampleProductReadBody(client, baseUrl, query)

    return status
}

func exampleProductReadBody(client *http.Client, baseUrl string, query string) (string, int) {
    request, requestErr := http.NewRequest("GET", strings.TrimRight(baseUrl, "/")+"/products/api/read/prod-1/"+query, nil)
    if nil != requestErr {
        fail("example currency: build the product read request: %v", requestErr)
    }

    request.Header.Set("Accept", "application/json")

    response, responseErr := client.Do(request)
    if nil != responseErr {
        fail("example currency: %s: %v", request.URL, responseErr)
    }
    defer response.Body.Close()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("example currency: read the product body: %v", readErr)
    }

    return string(body), response.StatusCode
}

func exampleCurrencyCodeOf(document exampleProductDocument, currencyId string) string {
    for _, currency := range document.Currencies {
        if currencyId == currency.Id {
            return currency.Code
        }
    }

    return ""
}

func exampleOtherCurrencyCode(document exampleProductDocument, exceptCode string) string {
    for _, currency := range document.Currencies {
        if exceptCode != currency.Code {
            return currency.Code
        }
    }

    return ""
}
