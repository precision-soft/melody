package exception

func NewExitError(exitCode int, err *Error) *ExitError {
	if nil == err {
		Panic(
			NewEmergency("exit error called with nil error", nil, nil),
		)
	}

	return &ExitError{
		exitCode: exitCode,
		err:      err,
	}
}

type ExitError struct {
	exitCode int
	err      *Error
}

func (instance *ExitError) Error() string {
	return instance.err.Error()
}

func (instance *ExitError) Unwrap() error {
	return instance.err
}

func (instance *ExitError) ExitCode() int {
	return instance.exitCode
}

func (instance *ExitError) ErrorValue() *Error {
	return instance.err
}
