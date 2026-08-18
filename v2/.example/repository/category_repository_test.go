package repository

import "testing"

func TestValidateCategoryNamesTheFieldThatFailed(t *testing.T) {
    for expected, breakIt := range map[string]func(*testing.T) error{
        "name is required": func(t *testing.T) error {
            category := validCategory()
            category.Name = " "

            return validateCategory(category)
        },
        "category is required": func(t *testing.T) error {
            return validateCategory(nil)
        },
    } {
        t.Run(expected, func(t *testing.T) {
            err := breakIt(t)

            if nil == err {
                t.Fatalf("the category was accepted")
            }

            if expected != err.Error() {
                t.Fatalf("got %q, want %q", err.Error(), expected)
            }
        })
    }
}

func TestValidateCategoryAcceptsAWholeCategory(t *testing.T) {
    if err := validateCategory(validCategory()); nil != err {
        t.Fatalf("a whole category was refused: %v", err)
    }
}

func TestNextCategoryIdContinuesTheSeededNumbering(t *testing.T) {
    if "cat-3" != nextCategoryId([]string{"cat-2", "cat-1"}) {
        t.Fatalf("unexpected identifier: %q", nextCategoryId([]string{"cat-2", "cat-1"}))
    }

    if "cat-1" != nextCategoryId(nil) {
        t.Fatalf("an empty catalogue must start at one, got %q", nextCategoryId(nil))
    }
}
