package cron

type Schedule struct {
    Minute     string
    Hour       string
    DayOfMonth string
    Month      string
    DayOfWeek  string
}

func (instance *Schedule) Defaults() *Schedule {
    if nil == instance {
        return nil
    }

    if "" == instance.Minute {
        instance.Minute = "*"
    }

    if "" == instance.Hour {
        instance.Hour = "*"
    }

    if "" == instance.DayOfMonth {
        instance.DayOfMonth = "*"
    }

    if "" == instance.Month {
        instance.Month = "*"
    }

    if "" == instance.DayOfWeek {
        instance.DayOfWeek = "*"
    }

    return instance
}

func (instance *Schedule) Expression() string {
    if nil == instance {
        return "* * * * *"
    }

    return fieldOrWildcard(instance.Minute) + " " +
        fieldOrWildcard(instance.Hour) + " " +
        fieldOrWildcard(instance.DayOfMonth) + " " +
        fieldOrWildcard(instance.Month) + " " +
        fieldOrWildcard(instance.DayOfWeek)
}

func fieldOrWildcard(field string) string {
    if "" == field {
        return "*"
    }

    return field
}
