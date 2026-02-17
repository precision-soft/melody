package security

import (
	"net/http"

	melodyhttp "github.com/precision-soft/melody/v2/http"
	melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
	melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewSessionLoginHandler() melodysecuritycontract.LoginHandler {
	return &sessionLoginHandler{}
}

type sessionLoginHandler struct{}

func (instance *sessionLoginHandler) Login(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	input melodysecuritycontract.LoginInput,
) (*melodysecuritycontract.LoginResult, error) {
	sessionInstance := getSession(request)
	if nil == sessionInstance {
		response := melodyhttp.JsonErrorResponse(http.StatusInternalServerError, "session is not available")

		return &melodysecuritycontract.LoginResult{
			Token:    input.Token,
			Response: response,
		}, nil
	}

	userIdentifier := ""
	if nil != input.Token {
		userIdentifier = input.Token.UserIdentifier()
	}

	sessionInstance.Set(SessionKeySecurityUserId, userIdentifier)

	response, err := melodyhttp.JsonResponse(http.StatusOK, map[string]any{
		"success": true,
		"userId":  userIdentifier,
	})
	if nil != err {
		return nil, err
	}

	return &melodysecuritycontract.LoginResult{
		Token:    input.Token,
		Response: response,
	}, nil
}

var _ melodysecuritycontract.LoginHandler = (*sessionLoginHandler)(nil)
