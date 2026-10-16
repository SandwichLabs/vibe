package awssso

import (
	"errors"
	"fmt"
)

// Reason says why a profile needs the person to sign in.
type Reason string

const (
	// ReasonMissing: there is no usable token in the cache.
	ReasonMissing Reason = "missing"
	// ReasonExpired: the cached token has expired and cannot be refreshed.
	ReasonExpired Reason = "expired"
	// ReasonRefused: AWS refused the cached token (revoked, or the session ended).
	ReasonRefused Reason = "refused"
)

// SignInNeeded means the person must sign in (the device flow, or
// `aws sso login --profile NAME`) before the profile gives credentials.
type SignInNeeded struct {
	Profile string
	Reason  Reason
}

func (e *SignInNeeded) Error() string {
	switch e.Reason {
	case ReasonExpired:
		return fmt.Sprintf("AWS session for profile %s has expired", e.Profile)
	case ReasonRefused:
		return fmt.Sprintf("AWS refused the session for profile %s; sign in again", e.Profile)
	default:
		return fmt.Sprintf("no AWS session for profile %s; sign in", e.Profile)
	}
}

// ProfileError is a profile that cannot be used. The text names the profile and
// the problem, never the contents or the path of the config file.
type ProfileError struct {
	Profile string
	Problem string
}

func (e *ProfileError) Error() string {
	return fmt.Sprintf("AWS profile %s %s", e.Profile, e.Problem)
}

// ServiceError is an answer from AWS that is not a sign-in problem (throttling,
// an account or role this session cannot use, a server error). Code is AWS's
// error code, never its message.
type ServiceError struct {
	Profile string
	Code    string
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("AWS sign-in service answered an error (%s) for profile %s", e.Code, e.Profile)
}

var (
	// ErrCacheWrite: AWS answered, but the refreshed session could not be saved
	// to the token cache (read-only or missing folder). Not remembered.
	ErrCacheWrite = errors.New("couldn't save the AWS session to the SSO cache")
	// ErrUnreachable: the AWS sign-in endpoints could not be reached (offline,
	// timeout, proxy). Not a sign-in problem.
	ErrUnreachable = errors.New("couldn't reach AWS sign-in")
	// ErrDeviceCodeExpired: the person did not finish the browser step in time.
	ErrDeviceCodeExpired = errors.New("the AWS sign-in code expired before it was approved")
	// ErrAccessDenied: the person (or the portal) denied the sign-in request.
	ErrAccessDenied = errors.New("the AWS sign-in request was denied")
)
