package podman

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	godbus "github.com/godbus/dbus/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

type (
	Systemd interface {
		Availability(ctx context.Context) types.PodmanSystemdAvailability
		Start(ctx context.Context, unit string) error
		Stop(ctx context.Context, unit string) error
		Restart(ctx context.Context, unit string) error
		UnitStatus(ctx context.Context, unit string) (types.PodmanUnitStatus, error)
	}

	SystemdConfig struct {
		Getenv   func(string) string
		Stat     func(string) (os.FileInfo, error)
		Host     string
		Rootless bool
	}

	dbusSystemd struct {
		address string
		scope   types.PodmanSystemdScope
		reason  string
	}
)

const (
	defaultSystemBusAddress = "unix:path=/run/dbus/system_bus_socket"
)

func NewSystemd(cfg SystemdConfig) Systemd {
	scope := kit.Ternary(cfg.Rootless, types.PodmanSystemdScopeUser, types.PodmanSystemdScopeSystem)
	busVariable := kit.Ternary(cfg.Rootless, "DBUS_SESSION_BUS_ADDRESS", "DBUS_SYSTEM_BUS_ADDRESS")
	address := cfg.Getenv(busVariable)
	scheme, _, _ := strings.Cut(cfg.Host, "://")

	switch {
	case address == "" && cfg.Host != "" && scheme != "unix" && scheme != "npipe":
		return &dbusSystemd{
			scope:  scope,
			reason: fmt.Sprintf("the Podman engine at %s is remote; set %s to that host's bus to manage its systemd units", cfg.Host, busVariable),
		}
	case address == "" && cfg.Rootless:
		sessionBus := defaultSessionBus(cfg)
		return &dbusSystemd{
			address: sessionBus,
			scope:   scope,
			reason:  kit.Ternary(sessionBus == "", "DBUS_SESSION_BUS_ADDRESS is not set; rootless Podman needs the user's session bus mounted and the variable set", ""),
		}
	default:
		return &dbusSystemd{
			address: cmp.Or(address, defaultSystemBusAddress),
			scope:   scope,
		}
	}
}

func defaultSessionBus(cfg SystemdConfig) string {
	for _, dir := range []string{cfg.Getenv("XDG_RUNTIME_DIR"), "/run/user/" + strconv.Itoa(os.Getuid())} {
		if dir == "" {
			continue
		}
		if _, err := cfg.Stat(filepath.Join(dir, "bus")); err == nil {
			return "unix:path=" + filepath.Join(dir, "bus")
		}
	}
	return ""
}

func (s *dbusSystemd) Availability(ctx context.Context) types.PodmanSystemdAvailability {
	conn, err := s.connect(ctx)
	if err != nil {
		return types.PodmanSystemdAvailability{
			Scope:  s.scope,
			Reason: err.Error(),
		}
	}
	conn.Close()

	return types.PodmanSystemdAvailability{
		Available: true,
		Scope:     s.scope,
	}
}

func (s *dbusSystemd) Start(ctx context.Context, unit string) error {
	return s.runJob(ctx, unit, "start", func(conn *dbus.Conn, ch chan<- string) (int, error) {
		return conn.StartUnitContext(ctx, unit, "replace", ch)
	})
}

func (s *dbusSystemd) Stop(ctx context.Context, unit string) error {
	return s.runJob(ctx, unit, "stop", func(conn *dbus.Conn, ch chan<- string) (int, error) {
		return conn.StopUnitContext(ctx, unit, "replace", ch)
	})
}

func (s *dbusSystemd) Restart(ctx context.Context, unit string) error {
	return s.runJob(ctx, unit, "restart", func(conn *dbus.Conn, ch chan<- string) (int, error) {
		return conn.RestartUnitContext(ctx, unit, "replace", ch)
	})
}

func (s *dbusSystemd) UnitStatus(ctx context.Context, unit string) (types.PodmanUnitStatus, error) {
	conn, err := s.connect(ctx)
	if err != nil {
		return types.PodmanUnitStatus{}, err
	}
	defer conn.Close()

	units, err := conn.ListUnitsByNamesContext(ctx, []string{unit})
	if err != nil {
		return types.PodmanUnitStatus{}, fmt.Errorf("read systemd unit %s: %w", unit, err)
	}
	if len(units) == 0 || units[0].LoadState == "not-found" {
		return types.PodmanUnitStatus{}, common.Classify(common.ErrNotFound, fmt.Errorf("systemd unit %s not found", unit))
	}

	return types.PodmanUnitStatus{
		Name:        units[0].Name,
		Description: units[0].Description,
		LoadState:   units[0].LoadState,
		ActiveState: units[0].ActiveState,
		SubState:    units[0].SubState,
	}, nil
}

func (s *dbusSystemd) connect(ctx context.Context) (*dbus.Conn, error) {
	if s.address == "" {
		return nil, common.Classify(common.ErrUnavailable, errors.New(s.reason))
	}

	conn, err := dbus.NewConnection(func() (*godbus.Conn, error) {
		busConn, dialErr := godbus.Dial(s.address, godbus.WithContext(ctx))
		if dialErr != nil {
			return nil, dialErr
		}
		if authErr := busConn.Auth([]godbus.Auth{godbus.AuthExternal(strconv.Itoa(os.Getuid()))}); authErr != nil {
			_ = busConn.Close()
			return nil, authErr
		}
		if helloErr := busConn.Hello(); helloErr != nil {
			_ = busConn.Close()
			return nil, helloErr
		}
		return busConn, nil
	})
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("connect to systemd %s bus at %s: %w", s.scope, s.address, err))
	}
	return conn, nil
}

func (s *dbusSystemd) runJob(ctx context.Context, unit, action string, start func(*dbus.Conn, chan<- string) (int, error)) error {
	conn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan string, 1)
	if _, startErr := start(conn, done); startErr != nil {
		var dbusErr godbus.Error
		if errors.As(startErr, &dbusErr) && dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit" {
			return common.Classify(common.ErrNotFound, fmt.Errorf("%s systemd unit %s: %w", action, unit, startErr))
		}
		return fmt.Errorf("%s systemd unit %s: %w", action, unit, startErr)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("%s systemd unit %s: %w", action, unit, ctx.Err())
	case result := <-done:
		if result != "done" {
			return common.Classify(common.ErrConflict, fmt.Errorf("%s systemd unit %s: job %s", action, unit, result))
		}
		return nil
	}
}
