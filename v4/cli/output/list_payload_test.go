package output

import (
    "math"
    "testing"
)

func TestNewListPayload_KeepsTheReportedTotalIndependentOfTheItems(t *testing.T) {
    payload := NewListPayload([]string{"a", "b"}, 47, 2, 10)

    if 2 != len(payload.Items) {
        t.Fatalf("expected 2 items, got %d", len(payload.Items))
    }
    if 47 != payload.Total {
        t.Fatalf("expected total 47, got %d", payload.Total)
    }
    if 2 != payload.Limit {
        t.Fatalf("expected limit 2, got %d", payload.Limit)
    }
    if 10 != payload.Offset {
        t.Fatalf("expected offset 10, got %d", payload.Offset)
    }
}

func TestWindowItems_SelectsTheRequestedWindow(t *testing.T) {
    items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

    cases := []struct {
        name     string
        limit    int
        offset   int
        expected []int
    }{
        {"noLimitNoOffset", 0, 0, items},
        {"limitOnly", 3, 0, []int{0, 1, 2}},
        {"offsetOnly", 0, 7, []int{7, 8, 9}},
        {"limitAndOffset", 2, 4, []int{4, 5}},
        {"limitPastTheEnd", 50, 8, []int{8, 9}},
        {"offsetAtTheEnd", 0, 10, []int{}},
        {"offsetPastTheEnd", 3, 25, []int{}},
        {"negativeOffset", 2, -5, []int{0, 1}},
        {"maximumLimit", math.MaxInt, 5, []int{5, 6, 7, 8, 9}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            window := WindowItems(items, testCase.limit, testCase.offset)

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
    window := WindowItems([]string{}, 5, 5)

    if 0 != len(window) {
        t.Fatalf("expected no items, got %v", window)
    }
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

    window := WindowItems(items, 3, 0)

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
