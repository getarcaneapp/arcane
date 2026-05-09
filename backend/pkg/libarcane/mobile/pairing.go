package mobile

import (
	"context"
	"strings"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
)

// RedeemCode is the unauthenticated pairing entry point. The CodeRedeemer
// callback is responsible for the atomic DB transaction that validates the
// code, upserts the device, creates the api key, and marks the session
// redeemed.
func (s *MobileServer) RedeemCode(ctx context.Context, req *mobilepb.RedeemCodeRequest) (*mobilepb.RedeemCodeResponse, error) {
	if s.callbacks.RedeemCode == nil {
		return nil, statusFromError(ErrInvalidCode)
	}

	code := strings.TrimSpace(req.GetCode())
	if code == "" {
		return nil, statusFromError(ErrInvalidCode)
	}

	deviceID := strings.TrimSpace(req.GetDeviceId())
	if deviceID == "" {
		return nil, statusFromError(ErrInvalidCode)
	}

	out, err := s.callbacks.RedeemCode(ctx, RedeemInput{
		Code:        code,
		DeviceID:    deviceID,
		DeviceName:  req.GetDeviceName(),
		AppVersion:  req.GetAppVersion(),
		OsVersion:   req.GetOsVersion(),
		DeviceModel: req.GetDeviceModel(),
	})
	if err != nil {
		return nil, statusFromError(err)
	}

	return &mobilepb.RedeemCodeResponse{
		DeviceToken: out.DeviceToken,
		Device:      deviceToProto(out.Device),
		ServerUrl:   out.ServerURL,
		Username:    out.Username,
	}, nil
}
