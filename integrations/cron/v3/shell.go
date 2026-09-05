package cron

import (
    "strings"
)

const shellMetacharacters = " \t\n'\"$`;&|()<>\\*?[]{}~#"

/* ShellQuoteIfNeeded renders token as ONE shell word: unchanged when it carries no shell metacharacter, single-quoted (with an embedded quote escaped) when it does, and '' when it is empty — so the shell behind a crontab line hands the process exactly the argument that was configured. A custom template whose dialect ends in a shell command line — ansible.builtin.cron's job does — quotes every token through it, or through JoinShellTokens, rather than joining the tokens on a space: joined raw, an argument carrying a space arrives as two, and one carrying ; or | is read by the shell as its own. */
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
