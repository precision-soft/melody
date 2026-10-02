package currency

import (
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

type currencyEnvelope struct {
    Payload CurrencyResponse `json:"payload"`
    Errors  []string         `json:"errors"`
}

func TestApiCreateDoorRefusesAUserWithoutTheEditorRole(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleUser), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":"Swiss Franc","rate":0.94}`, nil)
    if nethttp.StatusForbidden != status {
        t.Fatalf("expected 403, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-chf"); true == found {
        t.Fatalf("a refused create wrote the currency")
    }
}

func TestApiCreateDoorAnswersOneErrorPerViolatedField(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"has space","code":"chf","name":"a","rate":0}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    joined := strings.Join(envelope.Errors, "\n")
    for _, expected := range []string{"id: ", "code: ", "name: ", "rate: "} {
        if false == strings.Contains(joined, expected) {
            t.Fatalf("expected an entry for %q, got %v", expected, envelope.Errors)
        }
    }
}

func TestApiCreateDoorRefusesAnUnusableRateAsTheClientsInput(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":"Swiss Franc","rate":1e12}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for a rate past the admitted range, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || false == strings.HasPrefix(envelope.Errors[0], "rate: ") {
        t.Fatalf("expected the refusal to name the rate, got %v", envelope.Errors)
    }

    if _, found := fixture.stored(t, "cur-chf"); true == found {
        t.Fatalf("a refused rate was written")
    }
}

func TestApiCreateDoorCreatesTheCurrency(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":"Swiss Franc","rate":0.94}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected 201, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if "cur-chf" != envelope.Payload.Id || "CHF" != envelope.Payload.Code || "Swiss Franc" != envelope.Payload.Name || 0.94 != envelope.Payload.Rate {
        t.Fatalf("expected the created currency in the payload, got %+v", envelope.Payload)
    }

    stored, found := fixture.stored(t, "cur-chf")
    if false == found || "CHF" != stored.Code || 0.94 != stored.Rate {
        t.Fatalf("expected the currency in the repository, got %+v found=%v", stored, found)
    }
}

func TestApiCreateDoorMintsTheIdentifierWhenNoneIsGiven(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"code":"CHF","name":"Swiss Franc","rate":0.94}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected 201, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if "" == envelope.Payload.Id {
        t.Fatalf("expected a minted identifier, got %+v", envelope.Payload)
    }

    if _, found := fixture.stored(t, envelope.Payload.Id); false == found {
        t.Fatalf("expected the minted currency %q in the repository", envelope.Payload.Id)
    }
}

func TestApiCreateDoorAnswersATakenIdentifierAsAConflict(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":"Swiss Franc","rate":0.94}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected the first create to answer 201, got %d with body %q", status, body)
    }

    status, body = fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHX","name":"Second Franc","rate":0.5}`, nil)
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409 for a taken identifier, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || "id already exists" != envelope.Errors[0] {
        t.Fatalf("expected the refusal to name the identifier, got %v", envelope.Errors)
    }

    stored, found := fixture.stored(t, "cur-chf")
    if false == found || "CHF" != stored.Code {
        t.Fatalf("expected the first currency kept, got %+v", stored)
    }
}

func TestApiCreateDoorAnswersATakenCodeAsAConflictAndStoresNoSecondRow(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"code":"EUR","name":"Euro bis","rate":2}`, nil)
    if nethttp.StatusConflict != status {
        t.Fatalf("expected 409 for a taken code, got %d with body %q", status, body)
    }

    envelope := currencyEnvelope{}
    decodeBody(t, body, &envelope)

    if 1 != len(envelope.Errors) || "code already exists" != envelope.Errors[0] {
        t.Fatalf("expected the refusal to name the code, got %v", envelope.Errors)
    }

    if holders := fixture.holdingCode(t, "EUR"); 1 != len(holders) || "cur-eur" != holders[0] {
        t.Fatalf("expected EUR held by cur-eur alone, got %v", holders)
    }
}

/* the binding validates the body as sent; the door stores it trimmed, so it validates that spelling too: a name of one rune padded to two is refused, not stored */
func TestApiCreateDoorValidatesTheTrimmedBody(t *testing.T) {
    fixture := newCurrencyDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":" X","rate":0.94}`, nil)
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for a name that is one rune once trimmed, got %d with body %q", status, body)
    }

    if _, found := fixture.stored(t, "cur-chf"); true == found {
        t.Fatalf("a refused create wrote the currency")
    }

    status, body = fixture.call(t, ApiCreateHandler(), fixture.runtimeFor(entity.RoleEditor), nethttp.MethodPost, `{"id":"cur-chf","code":"CHF","name":" Franc ","rate":0.94}`, nil)
    if nethttp.StatusCreated != status {
        t.Fatalf("expected 201 for a padded name that is valid once trimmed, got %d with body %q", status, body)
    }

    if stored, _ := fixture.stored(t, "cur-chf"); "Franc" != stored.Name {
        t.Fatalf("expected the trimmed name stored, got %q", stored.Name)
    }
}
