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

func TestParameterBag_Remove_TakesTheNameOutOfEveryReader(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("kept", "value")
    parameterBag.Set("removed", "value")

    parameterBag.Remove("removed")
    parameterBag.Remove("never-present")

    if _, exists := parameterBag.Get("removed"); true == exists {
        t.Fatalf("expected Get to stop seeing the removed name")
    }

    if true == parameterBag.Has("removed") {
        t.Fatalf("expected Has to stop seeing the removed name")
    }

    if 1 != parameterBag.Count() {
        t.Fatalf("expected one remaining parameter, got %d", parameterBag.Count())
    }

    if false == parameterBag.Has("kept") {
        t.Fatalf("expected Remove to leave every other name alone")
    }
}

func TestParameterBag_Count_CountsNamesNotValues(t *testing.T) {
    parameterBag := NewParameterBag()

    if 0 != parameterBag.Count() {
        t.Fatalf("expected a fresh bag to count zero, got %d", parameterBag.Count())
    }

    parameterBag.Set("first", "value")
    parameterBag.Set("second", "value")

    if 2 != parameterBag.Count() {
        t.Fatalf("expected two names, got %d", parameterBag.Count())
    }

    if appendErr := parameterBag.AppendString("second", "another"); nil != appendErr {
        t.Fatalf("unexpected append error: %v", appendErr)
    }

    if 2 != parameterBag.Count() {
        t.Fatalf("expected the appended value to stay under the same name, got %d names", parameterBag.Count())
    }
}

func TestParameterBag_TheZeroValueAcceptsItsFirstWrite(t *testing.T) {
    var bagInstance ParameterBag

    if true == bagInstance.Has("key") {
        t.Fatalf("expected the zero bag to hold nothing")
    }

    bagInstance.Set("key", "value")

    value, exists := bagInstance.Get("key")
    if false == exists || "value" != value {
        t.Fatalf("expected the first write to land, got %#v (exists %v)", value, exists)
    }
}

func TestParameterBag_TheZeroValueAcceptsItsFirstAppend(t *testing.T) {
    var bagInstance ParameterBag

    if appendErr := bagInstance.AppendString("key", "value"); nil != appendErr {
        t.Fatalf("unexpected append error: %v", appendErr)
    }

    value, exists := bagInstance.Get("key")
    values, isSlice := value.([]string)
    if false == exists || false == isSlice || 1 != len(values) || "value" != values[0] {
        t.Fatalf("expected the first append to land as a one-element slice, got %#v (exists %v)", value, exists)
    }
}

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

func TestParameterBag_SetAllocatesTheZeroValueMap(t *testing.T) {
    parameterBag := &ParameterBag{}

    parameterBag.Set("name", "melody")

    value, exists := parameterBag.Get("name")
    if false == exists || "melody" != value {
        t.Fatalf("expected the zero value to accept a write, got %v (exists: %v)", value, exists)
    }
}

func TestParameterBag_GetHandsBackACopyOfTheAliasingShapes(t *testing.T) {
    bag := NewParameterBag()
    bag.Set("roles", []string{"admin", "editor"})
    bag.Set("labels", map[string]string{"env": "dev"})

    gotSlice, exists := bag.Get("roles")
    if false == exists {
        t.Fatalf("expected the slice parameter")
    }
    gotSlice.([]string)[0] = "mutated"

    storedSlice, _ := bag.Get("roles")
    if "admin" != storedSlice.([]string)[0] {
        t.Fatalf("expected the bag to keep its own slice, got %#v", storedSlice)
    }

    gotMap, exists := bag.Get("labels")
    if false == exists {
        t.Fatalf("expected the map parameter")
    }
    gotMap.(map[string]string)["env"] = "mutated"

    storedMap, _ := bag.Get("labels")
    if "dev" != storedMap.(map[string]string)["env"] {
        t.Fatalf("expected the bag to keep its own map, got %#v", storedMap)
    }
}
