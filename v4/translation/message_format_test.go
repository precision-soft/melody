package translation

import (
    "encoding/json"
    "math/rand"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func TestInterpolate_PoundStaysBoundThroughNestedSelect(t *testing.T) {
    pattern := "{count, plural, other {{gender, select, female {# guests (she)} other {# guests}}}}"

    result := formatMessage(pattern, map[string]any{"count": 3, "gender": "female"}, "en")
    if "3 guests (she)" != result {
        t.Fatalf("expected # to stay bound to the enclosing plural inside a nested select, got %q", result)
    }

    other := formatMessage(pattern, map[string]any{"count": 5, "gender": "robot"}, "en")
    if "5 guests" != other {
        t.Fatalf("expected the nested select other branch to substitute #, got %q", other)
    }
}

func TestEvaluatePlural_PoundFloat32MatchesPlaceholder(t *testing.T) {
    pattern := "{value, plural, other {# km}}"

    result := formatMessage(pattern, map[string]any{"value": float32(0.1)}, "en")
    if "0.1 km" != result {
        t.Fatalf("expected the `#` substitution to render float32 with shortest representation like {value}, got %q", result)
    }
}

func TestEvaluatePlural_PoundPreservesLargeIntegerPrecision(t *testing.T) {
    pattern := "{count, plural, other {# items}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "int64 above 2^53", value: int64(9007199254740993), expected: "9007199254740993 items"},
        {name: "uint64 above 2^53", value: uint64(9007199254740993), expected: "9007199254740993 items"},
        {name: "json.Number above 2^53", value: json.Number("9007199254740993"), expected: "9007199254740993 items"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (the `#` substitution must render the exact integer, not the float64-rounded value)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}

func TestEvaluatePlural_JsonNumberSelectsCategory(t *testing.T) {
    pattern := "{count, plural, one {# message} other {# messages}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "json.Number one", value: json.Number("1"), expected: "1 message"},
        {name: "json.Number other", value: json.Number("5"), expected: "5 messages"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (toFloat must accept json.Number, the type json.Decoder produces with UseNumber)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}

func TestEvaluatePlural_NonStandardNumericTypesSelectCategory(t *testing.T) {
    pattern := "{count, plural, one {# message} other {# messages}}"

    cases := []struct {
        name     string
        value    any
        expected string
    }{
        {name: "int8", value: int8(1), expected: "1 message"},
        {name: "int16", value: int16(1), expected: "1 message"},
        {name: "int32", value: int32(1), expected: "1 message"},
        {name: "uint", value: uint(1), expected: "1 message"},
        {name: "uint32", value: uint32(1), expected: "1 message"},
        {name: "float32", value: float32(1), expected: "1 message"},
        {name: "int32 plural", value: int32(5), expected: "5 messages"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := formatMessage(pattern, map[string]any{"count": testCase.value}, "en")
            if testCase.expected != result {
                t.Fatalf("count=%v (%T): expected %q, got %q (toFloat must accept the same numeric kinds the placeholder renderer does)", testCase.value, testCase.value, testCase.expected, result)
            }
        })
    }
}

func TestFormatMessage_AbsentParameterRendersTheVisiblePlaceholder(t *testing.T) {
    /* rendered as an empty string — what a nil map lookup stringifies to — the missing value vanished from the message with no channel through which anyone learns of it; the visible placeholder is the observable form */
    if rendered := formatMessage("Hello, {name}!", nil, "en"); "Hello, {name}!" != rendered {
        t.Fatalf("expected the absent parameter to stay visible, got %q", rendered)
    }

    if rendered := formatMessage("Hello, {name}!", map[string]any{"other": 1}, "en"); "Hello, {name}!" != rendered {
        t.Fatalf("expected the absent parameter to stay visible beside present ones, got %q", rendered)
    }
}

func TestFormatMessage_PresentNilParameterStillRendersEmpty(t *testing.T) {
    /* a parameter present with a nil value is the caller saying so explicitly — only the ABSENT key renders as the placeholder */
    if rendered := formatMessage("Hello, {name}!", map[string]any{"name": nil}, "en"); "Hello, !" != rendered {
        t.Fatalf("expected the explicit nil to render empty, got %q", rendered)
    }
}

func TestValidatePattern_RefusesASelectOrPluralThatIsNotKeywordAndBlockPairs(t *testing.T) {
    cases := []struct {
        name    string
        pattern string
        offset  int
        reason  string
    }{
        {name: "a keyword without a block drops the selectors after it", pattern: "{g, select, male {he} female other {they}}", offset: 22, reason: patternDefectKeywordWithoutBlock},
        {name: "a trailing keyword without a block", pattern: "{n, plural, one {# item} other}", offset: 25, reason: patternDefectKeywordWithoutBlock},
        {name: "a block without a keyword", pattern: "{g, select, {x} other {y}}", offset: 12, reason: patternDefectBlockWithoutKeyword},
        {name: "a select without other", pattern: "{g, select, male {he}}", offset: 0, reason: patternDefectWithoutOther},
        {name: "a plural without other", pattern: "You have {n, plural, one {# item}}", offset: 9, reason: patternDefectWithoutOther},
        {name: "a nested select inside a plural", pattern: "{n, plural, other {{g, select, male {he}}}}", offset: 19, reason: patternDefectWithoutOther},
        {name: "a keyword without a block in a nested block", pattern: "{n, plural, other {{g, select, male other {x}}}}", offset: 31, reason: patternDefectKeywordWithoutBlock},
        {name: "the offset counts runes, not bytes", pattern: "ăîș {g, select, male {el}}", offset: 4, reason: patternDefectWithoutOther},
        {name: "a multi-byte argument name counts as runes in the style offset", pattern: "{ă, select, é {x} bad other {y}}", offset: 18, reason: patternDefectKeywordWithoutBlock},
        {name: "a block without a keyword in a nested block after a multi-byte rune", pattern: "{n, plural, other {é {g, select, {x} other {y}}}}", offset: 33, reason: patternDefectBlockWithoutKeyword},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            validateErr := ValidatePattern(testCase.pattern)
            if nil == validateErr {
                t.Fatalf("expected %q refused", testCase.pattern)
            }

            context := exception.FromError(validateErr).Context()
            if testCase.offset != context["offset"] || testCase.reason != context["reason"] {
                t.Fatalf("expected offset %d reason %q, got %v", testCase.offset, testCase.reason, context)
            }
        })
    }
}

func TestValidatePattern_AdmitsWhatTransRendersWhole(t *testing.T) {
    patterns := []string{
        "plain text",
        "Hello, {name}!",
        "open { brace",
        "close } brace",
        "{g, select, male {he} other {they}",
        "{g, unknown, a {x}}",
        "{count, plural}",
        "{count, plural, =0 {No messages} one {# message} other {# messages}}",
        "{count, plural, other {{gender, select, female {# guests (she)} other {# guests}}}}",
        "{g, select, male{he} other{they}}",
        "{g, select,\n    male {he}\n    other {they}\n}",
    }

    for _, pattern := range patterns {
        if validateErr := ValidatePattern(pattern); nil != validateErr {
            t.Fatalf("expected %q admitted, got %v", pattern, exception.FromError(validateErr).Context())
        }
    }
}

/* a catalog of its own is not validated by Trans, which has no error to return: its malformed select renders leniently */
func TestTrans_ACustomCatalogsMalformedPatternStillRenders(t *testing.T) {
    manager := NewManager([]translationcontract.Catalog{unvalidatedCatalog{locale: "en", message: "{g, select, male {he} female other {they}}"}}, "en", nil)

    if rendered := manager.Trans("pronoun", map[string]any{"g": "male"}, "messages", "en"); "he" != rendered {
        t.Fatalf("expected the selector before the defect rendered, got %q", rendered)
    }

    if rendered := manager.Trans("pronoun", map[string]any{"g": "female"}, "messages", "en"); "" != rendered {
        t.Fatalf("expected the lenient rendering kept, got %q", rendered)
    }
}

type unvalidatedCatalog struct {
    locale  string
    message string
}

func (instance unvalidatedCatalog) Locale() string {
    return instance.locale
}

func (instance unvalidatedCatalog) Get(messageId string, domain string) (string, bool) {
    return instance.message, true
}

/* the validator and the renderer each walk the style: for every generated select the validator admits, the renderer must hold its other block and answer an unmatched value with it */
func TestValidatePattern_AgreesWithTheRenderersParseOfTheStyle(t *testing.T) {
    tokens := []string{"male", "other", "{x}", " ", "{y {z}}", "one", "{}", "\n"}
    random := rand.New(rand.NewSource(7))
    admitted := 0

    for attempt := 0; attempt < 20000; attempt++ {
        var style strings.Builder
        for count := 1 + random.Intn(8); 0 < count; count-- {
            style.WriteString(tokens[random.Intn(len(tokens))])
        }

        pattern := "{g, select," + style.String() + "}"
        if nil != ValidatePattern(pattern) {
            continue
        }

        admitted++

        selectors := parseSelectors([]rune(style.String()))
        otherBlock, hasOther := selectors["other"]
        if false == hasOther {
            t.Fatalf("admitted %q, but the renderer's parse holds no other block", pattern)
        }

        expected := formatMessage(string(otherBlock), nil, "en")
        if rendered := formatMessage(pattern, map[string]any{"g": "unmatched"}, "en"); expected != rendered {
            t.Fatalf("admitted %q, but an unmatched value rendered %q, not the other block %q", pattern, rendered, expected)
        }
    }

    if 0 == admitted {
        t.Fatalf("expected the generator to produce admitted patterns")
    }
}
