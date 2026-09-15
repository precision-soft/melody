package cron

import (
    "strings"
)

const shellMetacharacters = " \t\n'\"$`;&|()<>\\*?[]{}~#"

/* ShellQuoteIfNeeded renders one shell word, including embedded quotes and an empty token. Use it or JoinShellTokens when building a shell command line. */
func ShellQuoteIfNeeded(token string) string {
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

/* JoinShellTokens renders tokens as one shell command line: each token through ShellQuoteIfNeeded, the words separated by a single space. It is what the builtin crontab dialects write after the user column, and what a custom dialect that hands its command to a shell writes in the same place. */
func JoinShellTokens(tokens []string) string {
    quoted := make([]string, len(tokens))
    for index, token := range tokens {
        quoted[index] = ShellQuoteIfNeeded(token)
    }

    return strings.Join(quoted, " ")
}
