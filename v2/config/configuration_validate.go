package config

import (
    "github.com/precision-soft/melody/v2/exception"
)

func (instance *Configuration) validate() error {
    validateNoUnresolvedPlaceholdersErr := instance.validateNoUnresolvedPlaceholders()
    if nil != validateNoUnresolvedPlaceholdersErr {
        return validateNoUnresolvedPlaceholdersErr
    }

    return nil
}

func (instance *Configuration) validateNoUnresolvedPlaceholders() error {
    for parameterName, parameter := range instance.parameters {
        if nil == parameter {
            continue
        }

        value := parameter.Value()
        stringValue, ok := value.(string)
        if false == ok {
            continue
        }

        /* a value that escaped a literal percent with %% legitimately looks like a placeholder once unescaped ("%APP_NAME%" written as "%%APP_NAME%%"); it resolved correctly, so it is not an unresolved placeholder */
        if true == instance.parametersWithEscapedPercents[parameterName] {
            continue
        }

        if true == envPlaceholderPattern.MatchString(stringValue) || true == parameterPlaceholderPattern.MatchString(stringValue) {
            /* @important report only the offending placeholder (an identifier such as "%env(DB_TLS)%"), never the resolved value: it commonly holds inline credentials that would otherwise reach logs unredacted via the exception cause-context chain */
            offendingPlaceholder := envPlaceholderPattern.FindString(stringValue)
            if "" == offendingPlaceholder {
                offendingPlaceholder = parameterPlaceholderPattern.FindString(stringValue)
            }

            return exception.NewError(
                "parameter contains unresolved placeholders",
                map[string]any{
                    "parameterName": parameterName,
                    "placeholder":   offendingPlaceholder,
                },
                nil,
            )
        }
    }

    return nil
}
