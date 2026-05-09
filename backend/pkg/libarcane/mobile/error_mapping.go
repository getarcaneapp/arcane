package mobile

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Reason strings attached to gRPC errors via google.rpc.ErrorInfo so iOS can
// match on them without parsing free-form messages. Mirror these values in
// the iOS error layer.
const (
	ReasonInvalidCode     = "PAIRING_CODE_INVALID"
	ReasonCodeExpired     = "PAIRING_CODE_EXPIRED"
	ReasonCodeRedeemed    = "PAIRING_CODE_ALREADY_REDEEMED"
	ReasonInvalidToken    = "DEVICE_TOKEN_INVALID"
	ReasonDeviceRevoked   = "DEVICE_REVOKED"
	ReasonDeviceNotFound  = "DEVICE_NOT_FOUND"
	ReasonRateLimited     = "RATE_LIMITED"
	ReasonRemoteEnvUnsupp = "REMOTE_ENVIRONMENT_UNSUPPORTED"
	ReasonInternal        = "INTERNAL"

	errorInfoDomain = "arcane.mobile.v1"
)

// statusFromError converts a sentinel error from this package (or any
// callback-returned error) into a gRPC status.Status carrying ErrorInfo
// details. Unknown errors map to codes.Internal with reason="INTERNAL".
func statusFromError(err error) error {
	if err == nil {
		return nil
	}

	var (
		code   codes.Code
		reason string
		msg    string
	)

	switch {
	case errors.Is(err, ErrInvalidCode):
		code, reason, msg = codes.NotFound, ReasonInvalidCode, "pairing code is invalid"
	case errors.Is(err, ErrCodeExpired):
		code, reason, msg = codes.FailedPrecondition, ReasonCodeExpired, "pairing code has expired"
	case errors.Is(err, ErrCodeRedeemed):
		code, reason, msg = codes.FailedPrecondition, ReasonCodeRedeemed, "pairing code has already been redeemed"
	case errors.Is(err, ErrInvalidToken), errors.Is(err, ErrUnauthenticated):
		code, reason, msg = codes.Unauthenticated, ReasonInvalidToken, "device token is invalid"
	case errors.Is(err, ErrDeviceRevoked):
		code, reason, msg = codes.PermissionDenied, ReasonDeviceRevoked, "this device has been revoked"
	case errors.Is(err, ErrDeviceNotFound):
		code, reason, msg = codes.NotFound, ReasonDeviceNotFound, "device not found"
	case errors.Is(err, ErrRateLimited):
		code, reason, msg = codes.ResourceExhausted, ReasonRateLimited, "too many pairing attempts; try again later"
	case errors.Is(err, ErrEnvironmentLocal):
		code, reason, msg = codes.Unimplemented, ReasonRemoteEnvUnsupp, "only the local environment is supported in this version"
	default:
		code, reason, msg = codes.Internal, ReasonInternal, "internal server error"
	}

	st := status.New(code, msg)
	stWithInfo, attachErr := st.WithDetails(&errdetails.ErrorInfo{
		Reason: reason,
		Domain: errorInfoDomain,
	})
	if attachErr != nil {
		return st.Err()
	}
	return stWithInfo.Err()
}
