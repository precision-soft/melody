package bag

import (
    "net/url"
    "sync"
    "testing"
)

func TestParameterBagSetGetHas(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("name", "value")

    value, exists := parameterBag.Get("name")
    if false == exists {
        t.Fatalf("expected parameter to exist")
    }

    if "value" != value {
        t.Fatalf("expected value 'value', got %v", value)
    }

    if false == parameterBag.Has("name") {
        t.Fatalf("expected Has(name) to return true")
    }

    if true == parameterBag.Has("missing") {
        t.Fatalf("expected Has(missing) to return false")
    }
}

func TestParameterBagOverwriteValue(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("key", "value1")
    parameterBag.Set("key", "value2")

    value, exists := parameterBag.Get("key")
    if false == exists {
        t.Fatalf("expected parameter to exist")
    }

    if "value2" != value {
        t.Fatalf("expected overwritten value")
    }
}

func TestNewParameterBagFromValuesDeepCopy(t *testing.T) {
    values := url.Values{}
    values.Add("tag", "a")
    values.Add("tag", "b")

    parameterBag := NewParameterBagFromValues(values)

    original := values["tag"]
    original[0] = "modified"

    valueAny, exists := parameterBag.Get("tag")
    if false == exists {
        t.Fatalf("expected tag to exist")
    }

    sliceValue, ok := valueAny.([]string)
    if false == ok {
        t.Fatalf("expected []string, got %T", valueAny)
    }

    if "a" != sliceValue[0] {
        t.Fatalf("expected NewParameterBagFromValues to deep copy url.Values content")
    }
}

/* @info the request bags keep the single and the repeated key apart by type: one occurrence is the string it really is, a repeated key is a genuine slice, an empty key and an empty list are absent */
func TestNewParameterBagFromValues_SeparatesSingleFromRepeated(t *testing.T) {
    parameterBag := NewParameterBagFromValues(url.Values{
        "single":   {"melody"},
        "repeated": {"1", "2"},
        "":         {"dropped"},
        "empty":    {},
    })

    singleValue, singleExists := parameterBag.Get("single")
    if false == singleExists {
        t.Fatalf("expected the single key to exist")
    }
    if stringValue, isString := singleValue.(string); false == isString || "melody" != stringValue {
        t.Fatalf("expected the single occurrence to be stored as its string, got %T: %v", singleValue, singleValue)
    }

    repeatedValue, repeatedExists := parameterBag.Get("repeated")
    if false == repeatedExists {
        t.Fatalf("expected the repeated key to exist")
    }
    if sliceValue, isSlice := repeatedValue.([]string); false == isSlice || 2 != len(sliceValue) {
        t.Fatalf("expected the repeated key to stay a string slice, got %T: %v", repeatedValue, repeatedValue)
    }

    if true == parameterBag.Has("") {
        t.Fatalf("expected the empty key to be dropped")
    }
    if true == parameterBag.Has("empty") {
        t.Fatalf("expected the empty value list to be absent")
    }
}

/* @info All copies as deep as the bag's own writers go: a mutation on the returned slice or map must not write into the stored value behind the lock */
func TestParameterBag_All_CopiesKnownShapesDeep(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("slice", []string{"a", "b"})
    parameterBag.Set("stringMap", map[string]string{"k": "v"})

    all := parameterBag.All()

    all["slice"].([]string)[0] = "mutated"
    all["stringMap"].(map[string]string)["k"] = "mutated"

    storedSlice, _ := parameterBag.Get("slice")
    if "a" != storedSlice.([]string)[0] {
        t.Fatalf("expected the stored slice to be isolated from mutations on the copy")
    }

    storedMap, _ := parameterBag.Get("stringMap")
    if "v" != storedMap.(map[string]string)["k"] {
        t.Fatalf("expected the stored map to be isolated from mutations on the copy")
    }
}

/* @info the concrete bag appends inside one critical section: two writers appending concurrently keep every value — the helper's contract fallback reads and writes under two separate locks, and that window loses appends without any error and without anything the race detector can see */
func TestParameterBag_AppendString_KeepsEveryConcurrentAppend(t *testing.T) {
    parameterBag := NewParameterBag()

    appendsPerWriter := 500

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)

    for writer := 0; writer < 2; writer++ {
        go func() {
            defer waitGroup.Done()

            for index := 0; index < appendsPerWriter; index++ {
                if appendErr := AppendString(parameterBag, "collected", "v"); nil != appendErr {
                    t.Errorf("append error: %v", appendErr)
                    return
                }
            }
        }()
    }

    waitGroup.Wait()

    values, exists := StringSlice(parameterBag, "collected")
    if false == exists {
        t.Fatalf("expected the appended key to exist")
    }
    if 2*appendsPerWriter != len(values) {
        t.Fatalf("expected %d appended values, got %d — a lost update between Get and Set", 2*appendsPerWriter, len(values))
    }
}
