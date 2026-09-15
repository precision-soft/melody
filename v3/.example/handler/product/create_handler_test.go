package product

import (
    nethttp "net/http"
    "strings"
    "testing"
)

func TestApiCreateDoorAnswersOneErrorPerViolatedField(t *testing.T) {
    status, body := callCreateDoor(t, `{"id":"has space","name":"a","description":"","categoryId":"","price":-1,"currencyId":"","stock":-5}`)

    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    errorList := errorListOf(t, body)
    if 2 > len(errorList) {
        t.Fatalf("expected one entry per violated field, got %v", errorList)
    }

    joined := strings.Join(errorList, "\n")
    for _, expected := range []string{"id: ", "name: ", "description: ", "categoryId: ", "price: ", "currencyId: ", "stock: "} {
        if false == strings.Contains(joined, expected) {
            t.Fatalf("expected an entry for %q, got %v", expected, errorList)
        }
    }

    if true == strings.Contains(joined, "validation failed") {
        t.Fatalf("the generic message replaced the per-field detail: %v", errorList)
    }
}

func TestApiCreateDoorKeepsTheDecoderDiagnosisOutOfTheErrorsList(t *testing.T) {
    status, body := callCreateDoor(t, `{"name": not json`)

    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400, got %d with body %q", status, body)
    }

    errorList := errorListOf(t, body)
    if 1 != len(errorList) {
        t.Fatalf("expected exactly the public message, got %v", errorList)
    }

    if true == strings.Contains(errorList[0], "invalid character") {
        t.Fatalf("the decoder diagnosis reached the errors list: %v", errorList)
    }
}
