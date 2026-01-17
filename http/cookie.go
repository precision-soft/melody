package http

import (
	nethttp "net/http"

	"github.com/precision-soft/melody/exception"
	httpcontract "github.com/precision-soft/melody/http/contract"
)

func SetCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
	if "" == cookie.Name {
		exception.Panic(
			exception.NewError("the cookie name is empty and can not be set", nil, nil),
		)
	}

	response.Headers().Add("Set-Cookie", cookie.String())
}

func DeleteCookie(response httpcontract.Response, name string, path string) {
	if "" == name {
		exception.Panic(
			exception.NewError("the cookie name is empty and can not be deleted", nil, nil),
		)
	}

	if "" == path {
		path = "/"
	}

	cookie := &nethttp.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
	}

	SetCookie(response, cookie)
}
