package output

import (
    "math"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
)

func TestNewListPayload_KeepsTheReportedTotalIndependentOfTheItems(t *testing.T) {
    payload := NewListPayload([]string{"a", "b"}, 47, bound.Of(2), bound.Of(10))

    if 2 != len(payload.Items) {
        t.Fatalf("expected 2 items, got %d", len(payload.Items))
    }
    if 47 != payload.Total {
        t.Fatalf("expected total 47, got %d", payload.Total)
    }
    if bound.Of(2) != payload.Limit {
        t.Fatalf("expected limit 2, got %v", payload.Limit)
    }
    if bound.Of(10) != payload.Offset {
        t.Fatalf("expected offset 10, got %v", payload.Offset)
    }
}

func TestWindowItems_SelectsTheRequestedWindow(t *testing.T) {
    items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

    cases := []struct {
        name     string
        limit    bound.Count
        offset   bound.Count
        expected []int
    }{
        {"noLimitNoOffset", bound.Count{}, bound.Count{}, items},
        {"unlimitedLimit", bound.UnlimitedCount(), bound.Count{}, items},
        {"zeroLimit", bound.Of(0), bound.Count{}, []int{}},
        {"limitOnly", bound.Of(3), bound.Count{}, []int{0, 1, 2}},
        {"offsetOnly", bound.Count{}, bound.Of(7), []int{7, 8, 9}},
        {"limitAndOffset", bound.Of(2), bound.Of(4), []int{4, 5}},
        {"limitPastTheEnd", bound.Of(50), bound.Of(8), []int{8, 9}},
        {"offsetAtTheEnd", bound.Count{}, bound.Of(10), []int{}},
        {"offsetPastTheEnd", bound.Of(3), bound.Of(25), []int{}},
        {"maximumLimit", bound.Of(math.MaxInt), bound.Of(5), []int{5, 6, 7, 8, 9}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            window, windowErr := WindowItems(items, testCase.limit, testCase.offset)
            if nil != windowErr {
                t.Fatalf("expected the window admitted, got %v", windowErr)
            }

            if len(testCase.expected) != len(window) {
                t.Fatalf("expected %v, got %v", testCase.expected, window)
            }

            for index, value := range testCase.expected {
                if value != window[index] {
                    t.Fatalf("expected %v, got %v", testCase.expected, window)
                }
            }
        })
    }
}

func TestWindowItems_HandlesAnEmptyInput(t *testing.T) {
    window, windowErr := WindowItems([]string{}, bound.Of(5), bound.Of(5))
    if nil != windowErr {
        t.Fatalf("expected the window admitted, got %v", windowErr)
    }

    if 0 != len(window) {
        t.Fatalf("expected no items, got %v", window)
    }
}

func TestWindowItems_RefusesANegativeLimitOrOffsetByName(t *testing.T) {
    _, limitErr := WindowItems([]int{1}, bound.Of(-1), bound.Count{})
    assertRefusedOptionField(t, limitErr, "Option.Limit")

    _, offsetErr := WindowItems([]int{1}, bound.Count{}, bound.Of(-5))
    assertRefusedOptionField(t, offsetErr, "Option.Offset")
}

func TestReverseItems_ReversesInPlace(t *testing.T) {
    items := []int{0, 1, 2, 3, 4}

    ReverseItems(items)

    expected := []int{4, 3, 2, 1, 0}

    for index, value := range expected {
        if value != items[index] {
            t.Fatalf("expected %v, got %v", expected, items)
        }
    }
}

func TestReverseItems_HandlesTheDegenerateLengths(t *testing.T) {
    empty := []string{}
    ReverseItems(empty)

    if 0 != len(empty) {
        t.Fatalf("expected an empty slice, got %v", empty)
    }

    single := []string{"only"}
    ReverseItems(single)

    if "only" != single[0] {
        t.Fatalf("expected the single item to be kept, got %v", single)
    }
}

func TestApplySortOrder_ReversesOnlyForTheDescendingOrder(t *testing.T) {
    cases := map[SortOrder][]int{
        SortOrderAscending:  {0, 1, 2},
        SortOrderDescending: {2, 1, 0},
    }

    for order, expected := range cases {
        t.Run(string(order), func(t *testing.T) {
            items := []int{0, 1, 2}

            ApplySortOrder(items, order)

            for index, value := range expected {
                if value != items[index] {
                    t.Fatalf("expected %v, got %v", expected, items)
                }
            }
        })
    }
}

func TestApplySortOrder_ThenWindowItemsReturnsTheDescendingHead(t *testing.T) {
    items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

    ApplySortOrder(items, SortOrderDescending)

    window, windowErr := WindowItems(items, bound.Of(3), bound.Count{})
    if nil != windowErr {
        t.Fatalf("expected the window admitted, got %v", windowErr)
    }

    expected := []int{9, 8, 7}

    if len(expected) != len(window) {
        t.Fatalf("expected %v, got %v", expected, window)
    }

    for index, value := range expected {
        if value != window[index] {
            t.Fatalf("expected %v, got %v", expected, window)
        }
    }
}
