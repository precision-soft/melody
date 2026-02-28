package output

import "time"

type Envelope struct {
    Meta     Meta       `json:"meta"`
    Data     any        `json:"data"`
    Table    *TableData `json:"-"`
    Warnings []Warning  `json:"warnings"`
    Error    *Error     `json:"error"`
}

type Meta struct {
    Command              string    `json:"command"`
    Arguments            []string  `json:"arguments"`
    Flags                Flags     `json:"flags"`
    StartedAt            time.Time `json:"startedAt"`
    DurationMilliseconds int64     `json:"durationMilliseconds"`
    Version              Version   `json:"version"`
}

type Flags struct {
    Format  Format    `json:"format"`
    NoColor bool      `json:"noColor"`
    Verbose bool      `json:"verbose"`
    Quiet   bool      `json:"quiet"`
    Fields  []string  `json:"fields"`
    SortKey string    `json:"sortKey"`
    Order   SortOrder `json:"order"`
    Limit   int       `json:"limit"`
    Offset  int       `json:"offset"`
}

type Version struct {
    Application string `json:"application"`
    Melody      string `json:"melody"`
    Go          string `json:"go"`
}

type Warning struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details"`
}

type Error struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details"`
    Cause   *ErrorCause    `json:"cause"`
}

type ErrorCause struct {
    Message string         `json:"message"`
    Details map[string]any `json:"details"`
}

func (instance *Envelope) AddWarning(
    code string,
    message string,
    details map[string]any,
) {
    instance.Warnings = append(
        instance.Warnings,
        NewWarning(code, message, details),
    )
}

func (instance *Envelope) SetError(
    code string,
    message string,
    details map[string]any,
    cause *ErrorCause,
) {
    instance.Error = NewError(code, message, details, cause)
}
