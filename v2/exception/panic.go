package exception

func Panic(err *Error) {
	if nil == err {
		panic(
			NewEmergency("panic called with nil error", nil, nil),
		)
	}

	panic(err)
}

func Exit(err *ExitError) {
	if nil == err {
		panic(
			NewEmergency("exit called with nil error", nil, nil),
		)
	}

	panic(err)
}
