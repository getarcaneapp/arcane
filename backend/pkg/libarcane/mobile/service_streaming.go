package mobile

import (
	"context"
	"errors"
	"io"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
)

// ---------- Server-streaming RPCs ----------

func (s *MobileServer) StreamContainerLogs(req *mobilepb.StreamContainerLogsRequest, stream mobilepb.MobileService_StreamContainerLogsServer) error {
	ctx := stream.Context()
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return err
	}
	if s.callbacks.StreamContainerLogs == nil {
		return statusFromError(ErrUnauthenticated)
	}
	send := func(chunk []byte) error {
		return stream.Send(&mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_Data{Data: chunk}})
	}
	err := s.callbacks.StreamContainerLogs(ctx, req.GetEnvironmentId(), req.GetId(), LogOptions{
		Follow:     req.GetFollow(),
		Tail:       req.GetTail(),
		Timestamps: req.GetTimestamps(),
		Stdout:     req.GetStdout(),
		Stderr:     req.GetStderr(),
		Since:      req.GetSince(),
		Until:      req.GetUntil(),
	}, send)
	return finalizeStream(stream, err)
}

func (s *MobileServer) StreamContainerStats(req *mobilepb.EnvIDAndIDRequest, stream mobilepb.MobileService_StreamContainerStatsServer) error {
	ctx := stream.Context()
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return err
	}
	if s.callbacks.StreamContainerStats == nil {
		return statusFromError(ErrUnauthenticated)
	}
	send := func(chunk []byte) error {
		return stream.Send(&mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_Data{Data: chunk}})
	}
	err := s.callbacks.StreamContainerStats(ctx, req.GetEnvironmentId(), req.GetId(), send)
	return finalizeStream(stream, err)
}

func (s *MobileServer) StreamProjectLogs(req *mobilepb.StreamProjectLogsRequest, stream mobilepb.MobileService_StreamProjectLogsServer) error {
	ctx := stream.Context()
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return err
	}
	if s.callbacks.StreamProjectLogs == nil {
		return statusFromError(ErrUnauthenticated)
	}
	send := func(chunk []byte) error {
		return stream.Send(&mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_Data{Data: chunk}})
	}
	err := s.callbacks.StreamProjectLogs(ctx, req.GetEnvironmentId(), req.GetId(), LogOptions{
		Follow:     req.GetFollow(),
		Tail:       req.GetTail(),
		Timestamps: req.GetTimestamps(),
		Stdout:     true,
		Stderr:     true,
	}, send)
	return finalizeStream(stream, err)
}

func (s *MobileServer) StreamSystemStats(req *mobilepb.StreamSystemStatsRequest, stream mobilepb.MobileService_StreamSystemStatsServer) error {
	ctx := stream.Context()
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return err
	}
	if s.callbacks.StreamSystemStats == nil {
		return statusFromError(ErrUnauthenticated)
	}
	send := func(chunk []byte) error {
		return stream.Send(&mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_Data{Data: chunk}})
	}
	err := s.callbacks.StreamSystemStats(ctx, req.GetEnvironmentId(), req.GetIntervalMs(), send)
	return finalizeStream(stream, err)
}

func (s *MobileServer) StreamPullImage(req *mobilepb.PullImageRequest, stream mobilepb.MobileService_StreamPullImageServer) error {
	ctx := stream.Context()
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return err
	}
	if s.callbacks.StreamPullImage == nil {
		return statusFromError(ErrUnauthenticated)
	}
	send := func(chunk []byte) error {
		return stream.Send(&mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_Data{Data: chunk}})
	}
	err := s.callbacks.StreamPullImage(ctx, req.GetEnvironmentId(), req.GetRef(), req.GetAuthCredentialsJson(), send)
	return finalizeStream(stream, err)
}

// finalizeStream emits the trailing End frame (with optional error) so the
// client always sees an explicit termination signal.
func finalizeStream[T endSender](stream T, err error) error {
	end := &mobilepb.StreamFrame{Payload: &mobilepb.StreamFrame_End{End: &mobilepb.StreamEnd{}}}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		end.GetEnd().Error = err.Error()
	}
	if sendErr := stream.Send(end); sendErr != nil {
		return sendErr
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		return statusFromError(err)
	}
	return nil
}

type endSender interface {
	Send(*mobilepb.StreamFrame) error
}

// ---------- Bidi terminal ----------

func (s *MobileServer) ContainerTerminal(stream mobilepb.MobileService_ContainerTerminalServer) error {
	ctx := stream.Context()
	if _, ok := UserIDFromContext(ctx); !ok {
		return statusFromError(ErrUnauthenticated)
	}
	if s.callbacks.TerminalSession == nil {
		return statusFromError(ErrUnauthenticated)
	}

	// First frame must be Start.
	first, err := stream.Recv()
	if err != nil {
		return statusFromError(err)
	}
	startMsg := first.GetStart()
	if startMsg == nil {
		return statusFromError(ErrUnauthenticated)
	}

	// Acknowledge readiness so the client may begin streaming input.
	if err := stream.Send(&mobilepb.TerminalServerFrame{Payload: &mobilepb.TerminalServerFrame_Ready{Ready: &mobilepb.TerminalReady{}}}); err != nil {
		return statusFromError(err)
	}

	recv := func() ([]byte, error) {
		frame, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		switch p := frame.Payload.(type) {
		case *mobilepb.TerminalClientFrame_Input:
			return p.Input, nil
		case *mobilepb.TerminalClientFrame_Resize:
			// Resize frames are encoded inline as a tiny JSON header so the
			// callback can dispatch them without proto coupling.
			return []byte("\x1bRESIZE:" + itoa(int(p.Resize.GetCols())) + "x" + itoa(int(p.Resize.GetRows()))), nil
		case *mobilepb.TerminalClientFrame_Close:
			return nil, io.EOF
		default:
			return nil, nil
		}
	}
	send := func(data []byte) error {
		return stream.Send(&mobilepb.TerminalServerFrame{Payload: &mobilepb.TerminalServerFrame_Output{Output: data}})
	}

	err = s.callbacks.TerminalSession(ctx, TerminalSessionInput{
		EnvID:       startMsg.GetEnvironmentId(),
		ContainerID: startMsg.GetContainerId(),
		Shell:       startMsg.GetShell(),
		Cols:        startMsg.GetCols(),
		Rows:        startMsg.GetRows(),
	}, recv, send)

	closeMsg := ""
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		closeMsg = err.Error()
	}
	_ = stream.Send(&mobilepb.TerminalServerFrame{Payload: &mobilepb.TerminalServerFrame_Close{Close: &mobilepb.TerminalClose{Reason: closeMsg}}})
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		return statusFromError(err)
	}
	return nil
}

// itoa avoids pulling strconv just for two ints.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
