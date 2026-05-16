package cron

import (
    "strings"
)

const shellMetacharacters = " \t\n'\"$`;&|()<>\\*?[]{}~#"

func shellQuoteIfNeeded(token string) string {
    if "" == token {
        return "''"
    }

    if false == strings.ContainsAny(token, shellMetacharacters) {
        return token
    }

    return singleQuote(token)
}

func singleQuote(value string) string {
    return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func joinShellTokens(tokens []string) string {
    quoted := make([]string, len(tokens))
    for index, token := range tokens {
        quoted[index] = shellQuoteIfNeeded(token)
    }

    return strings.Join(quoted, " ")
}
