package encrypt

import (
    "strings"
    "testing"
)

func candidateFormats(candidates [][]byte) []string {
    formats := make([]string, 0, len(candidates))
    for _, candidate := range candidates {
        formats = append(formats, string(candidate[len(markerFamily):len(markerFamily)+4])+" "+strings.SplitN(string(candidate[len(markerPrefix):]), ":", 2)[0])
    }

    return formats
}

func TestCiphertextCandidatesFor_ABoundColumnAnswersBoundThenUnbound(t *testing.T) {
    newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, false)

    candidates, candidatesErr := CiphertextCandidatesFor[crmIbanBinding]("RO49AAAA1B31007593840000")
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    expected := []string{"gcm2 crm-v2", "gcm2 crm-v1", "gcm1 crm-v2", "gcm1 crm-v1"}
    if formats := candidateFormats(candidates); strings.Join(expected, ",") != strings.Join(formats, ",") {
        t.Fatalf("expected %v, got %v", expected, formats)
    }
}

func TestCiphertextCandidatesFor_RefusingUnboundAnswersBoundAlone(t *testing.T) {
    newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, true)

    candidates, _ := CiphertextCandidatesFor[crmIbanBinding]("RO49AAAA1B31007593840000")
    if expected := "gcm2 crm-v2,gcm2 crm-v1"; expected != strings.Join(candidateFormats(candidates), ",") {
        t.Fatalf("expected %s, got %v", expected, candidateFormats(candidates))
    }
}

func TestCiphertextCandidatesFor_AnUnboundMarkerAnswersTheCipherCandidates(t *testing.T) {
    cipherInstance := newCrmCipher(t)

    fromMarker, _ := CiphertextCandidatesFor[crmCipherReference]("RO49AAAA1B31007593840000")
    fromCipher, _ := cipherInstance.CiphertextCandidates("RO49AAAA1B31007593840000")

    if len(fromMarker) != len(fromCipher) || string(fromMarker[0]) != string(fromCipher[0]) || string(fromMarker[1]) != string(fromCipher[1]) {
        t.Fatalf("expected the cipher's candidates, got %q and %q", fromMarker, fromCipher)
    }
}

func TestCiphertextCandidatesFor_HoldsWhatTheColumnWrites(t *testing.T) {
    newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, false)

    stored, _ := EncryptedDeterministicStringFor[crmIbanBinding]("RO49AAAA1B31007593840000").Value()
    candidates, _ := CiphertextCandidatesFor[crmIbanBinding]("RO49AAAA1B31007593840000")

    if string(stored.([]byte)) != string(candidates[0]) {
        t.Fatalf("expected the column's value as the first candidate, got %q and %q", stored, candidates[0])
    }
}
