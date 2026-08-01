package logging

import (
    "regexp"
    "testing"
)

/* @info the process id is what ties every record of one run together, and it is generated once at boot with no test ever reading its shape. The two masked bytes are the part a reader relies on: version 4 in the third group and the 10xx variant in the fourth are what make it a uuid rather than 16 random bytes wearing dashes */
func TestGenerateProcessId_HasTheUuidVersionFourShape(t *testing.T) {
    pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

    for iteration := 0; iteration < 50; iteration++ {
        processId := GenerateProcessId()

        if false == pattern.MatchString(processId) {
            t.Fatalf("unexpected process id shape: %q", processId)
        }
    }
}

/* @info two boots must not share an id: the whole point of the value is telling one run's records from another's, and a generator that lost its randomness would be invisible to the shape assertion above */
func TestGenerateProcessId_DoesNotRepeat(t *testing.T) {
    seenIdList := map[string]bool{}

    for iteration := 0; iteration < 200; iteration++ {
        processId := GenerateProcessId()

        if true == seenIdList[processId] {
            t.Fatalf("process id %q was generated twice", processId)
        }

        seenIdList[processId] = true
    }
}
