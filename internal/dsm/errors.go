package dsm

import "fmt"

// Error is a structured DSM API error returned by the `error.code` field.
type Error struct {
	Code   int
	API    string
	Method string
}

func (e *Error) Error() string {
	if msg, ok := errorMessages[e.Code]; ok {
		return fmt.Sprintf("dsm: %s (code %d) [api=%s method=%s]", msg, e.Code, e.API, e.Method)
	}
	return fmt.Sprintf("dsm: error code %d [api=%s method=%s]", e.Code, e.API, e.Method)
}

// Common DSM error codes (auth + generic only — per-API codes overlap
// these numbers with different meanings, so use err.Error()'s api/method
// context to disambiguate).
var errorMessages = map[int]string{
	100: "unknown error",
	101: "no parameter of API, method or version",
	102: "the requested API does not exist",
	103: "the requested method does not exist",
	104: "the requested version does not support the functionality",
	105: "the logged-in session does not have permission",
	106: "session timeout",
	107: "session interrupted by duplicate login",
	108: "failed to upload the file",
	109: "the network connection is unstable or system is busy",
	110: "the network connection is unstable or system is busy",
	111: "the network connection is unstable or system is busy",
	112: "preserve for other purpose",
	113: "preserve for other purpose",
	114: "lost parameters for API",
	115: "not allowed to upload a file",
	116: "not allowed to perform for a demo site",
	117: "the network connection is unstable or system is busy",
	118: "the network connection is unstable or system is busy",
	119: "invalid session",

	400: "no such account or incorrect password",
	401: "disabled account",
	402: "denied permission",
	403: "2-step verification code required",
	404: "failed to authenticate 2-step verification code",
	405: "app portal incorrect",
	406: "OTP code enforced",
	407: "max tries exceeded (IP blocked)",
	408: "password expired and cannot change",
	409: "password expired",
	410: "password must be changed",
	411: "account locked (within retry period)",
}

// IsOTPRequired returns true if the error indicates a missing or invalid OTP.
func IsOTPRequired(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == 403 || e.Code == 404 || e.Code == 406
	}
	return false
}

// IsAuthFailure returns true for the auth code range.
func IsAuthFailure(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code >= 400 && e.Code < 500
	}
	return false
}

// IsSessionExpired returns true when the SID is stale and a re-login is needed.
func IsSessionExpired(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == 105 || e.Code == 106 || e.Code == 107 || e.Code == 119
	}
	return false
}
