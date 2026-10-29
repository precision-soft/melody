package bound

import (
    "strconv"
    "time"
)

/* Unit is what a limit bounds: a time.Duration for timeouts, budgets, delays and ttls, an int for counts and sizes. */
type Unit interface {
    time.Duration | int
}

type limitState uint8

const (
    limitStateAbsent limitState = iota
    limitStateValue
    limitStateUnlimited
)

/* Limit is a limit in one of three states. Its fields are unexported, so no state outside the three can be built: the zero value is absent, Of builds a value, UnlimitedDuration and UnlimitedCount build unlimited. */
type Limit[T Unit] struct {
    state limitState
    value T
}

/* Duration is the limit of a timeout, a budget, a delay or a ttl. */
type Duration = Limit[time.Duration]

/* Count is the limit of a count or a size. */
type Count = Limit[int]

/* Of answers the limit set to the value. The value is judged when the component resolves the field, so a value below the field's minimum is refused there, naming the field. */
func Of[T Unit](value T) Limit[T] {
    return Limit[T]{
        state: limitStateValue,
        value: value,
    }
}

/* UnlimitedDuration answers the duration limit that lifts the bound, refused by name on a field that does not admit it. */
func UnlimitedDuration() Duration {
    return Duration{state: limitStateUnlimited}
}

/* UnlimitedCount answers the count limit that lifts the bound, refused by name on a field that does not admit it. */
func UnlimitedCount() Count {
    return Count{state: limitStateUnlimited}
}

/* IsAbsent answers whether the limit was left out, which takes the field's default. */
func (instance Limit[T]) IsAbsent() bool {
    return limitStateAbsent == instance.state
}

/* IsUnlimited answers whether the limit lifts the bound. */
func (instance Limit[T]) IsUnlimited() bool {
    return limitStateUnlimited == instance.state
}

/* Value answers the value and true when the limit is set to a value, and the zero value and false when it is absent or unlimited. */
func (instance Limit[T]) Value() (T, bool) {
    if limitStateValue != instance.state {
        var zero T

        return zero, false
    }

    return instance.value, true
}

/* String answers "absent", "unlimited" or the value, a duration in time.Duration's form. */
func (instance Limit[T]) String() string {
    switch instance.state {
    case limitStateUnlimited:
        return unlimitedText
    case limitStateValue:
        return formatUnit(instance.value)
    }

    return "absent"
}

func formatUnit[T Unit](value T) string {
    switch typed := any(value).(type) {
    case time.Duration:
        return typed.String()
    case int:
        return strconv.Itoa(typed)
    }

    return ""
}
