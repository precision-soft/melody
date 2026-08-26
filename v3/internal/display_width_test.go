package internal

import (
    "testing"
)

func TestRuneDisplayWidth_AnswersTheCellsATerminalRenders(t *testing.T) {
    cases := []struct {
        name     string
        value    rune
        expected int
    }{
        {name: "ascii letter", value: 'a', expected: 1},
        {name: "cjk ideogram", value: '世', expected: 2},
        {name: "hangul syllable", value: '한', expected: 2},
        {name: "fullwidth latin", value: 'Ａ', expected: 2},
        {name: "katakana", value: 'ア', expected: 2},
        {name: "emoji", value: '😀', expected: 2},
        {name: "combining acute", value: '\u0301', expected: 0},
        {name: "zero width joiner", value: '\u200d', expected: 0},
        {name: "variation selector", value: '\ufe0f', expected: 0},
        {name: "trailing hangul jamo", value: '\u1160', expected: 0},
        {name: "narrow punctuation", value: '-', expected: 1},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            actual := RuneDisplayWidth(testCase.value)
            if testCase.expected != actual {
                t.Fatalf("expected width %d for %q, got %d", testCase.expected, string(testCase.value), actual)
            }
        })
    }
}

func TestDisplayWidth_SumsCellsNotRunes(t *testing.T) {
    cases := []struct {
        name     string
        value    string
        expected int
    }{
        {name: "plain ascii", value: "abc", expected: 3},
        {name: "cjk pair", value: "世界", expected: 4},
        {name: "mixed", value: "id-世界", expected: 7},
        {name: "combining folds into its base", value: "é", expected: 1},
        {name: "empty", value: "", expected: 0},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            actual := DisplayWidth(testCase.value)
            if testCase.expected != actual {
                t.Fatalf("expected width %d for %q, got %d", testCase.expected, testCase.value, actual)
            }
        })
    }
}
