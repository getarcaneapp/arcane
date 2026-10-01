// Package francis owns the embedded Francis host lifecycle.
package francis

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/components"
	"github.com/italypaleale/francis/host/local"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const tablePrefixInternal = "arcane_francis"

// Runtime starts one durable actor host and publishes its service after readiness.
// Services must wait for Ready before using Service, including during startup retries.
type Runtime struct {
	mu            sync.Mutex
	options       []local.HostOption
	registrations []registrationInternal
	service       *actor.Service
	ready         chan struct{}
	done          chan struct{}
	cancel        context.CancelFunc
	started       bool
	runError      error
	address       string
}

type registrationInternal struct {
	actorType string
	factory   actor.Factory
	options   []local.RegisterActorOption
}

func New(databaseURL, encryptionKey, instanceID, port string, options ...local.HostOption) (*Runtime, error) {
	if port == "" {
		port = "3551"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("ACTOR_PORT must be between 1 and 65535")
	}
	providerOption, err := providerOptionInternal(databaseURL)
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort("127.0.0.1", port)
	runtime := &Runtime{
		service: &actor.Service{}, ready: make(chan struct{}), done: make(chan struct{}), address: address,
		options: []local.HostOption{
			local.WithAddress(address), providerOption,
			local.WithLogger(slog.Default().With("scope", "actor-host")),
			local.WithMaxHosts(1), local.WithHostHealthCheckDeadline(90 * time.Second),
			local.WithShutdownGracePeriod(10 * time.Second), local.WithAlarmsPollInterval(time.Second),
			local.WithAlarmsFetchAheadInterval(30 * time.Second), local.WithAlarmsLeaseDuration(180 * time.Second),
		},
	}
	if encryptionKey != "" && instanceID != "" {
		if err := runtime.ConfigureIdentity(encryptionKey, instanceID); err != nil {
			return nil, err
		}
	}
	runtime.options = append(runtime.options, options...)
	return runtime, nil
}

// ConfigureIdentity binds the final persisted identity before Start.
func (r *Runtime) ConfigureIdentity(encryptionKey, instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("actor identity cannot change after startup")
	}
	if encryptionKey == "" || instanceID == "" {
		return errors.New("actor host requires the encryption key and instance ID")
	}
	psk, err := hkdf.Key(sha256.New, []byte(encryptionKey), nil, "arcane/actors-psk/"+instanceID, 32)
	if err != nil {
		return fmt.Errorf("derive actor authentication key: %w", err)
	}
	r.options = append(r.options, local.WithRuntimePSKs(psk))
	return nil
}

func (r *Runtime) Service() *actor.Service { return r.service }
func (r *Runtime) Ready() <-chan struct{}  { return r.ready }

func (r *Runtime) RegisterActor(actorType string, factory actor.Factory, options ...local.RegisterActorOption) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("actor factories must be registered before startup")
	}
	for _, registration := range r.registrations {
		if registration.actorType == actorType {
			return fmt.Errorf("actor type %q is already registered", actorType)
		}
	}
	r.registrations = append(r.registrations, registrationInternal{actorType: actorType, factory: factory, options: append([]local.RegisterActorOption(nil), options...)})
	return nil
}

// Start waits for registration and the peer listener. appCtx owns the running host;
// ctx only limits startup. onFailure must cancel the application without blocking.
func (r *Runtime) Start(ctx, appCtx context.Context, onFailure func(error)) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("actor host already started")
	}
	r.started = true
	runCtx, cancel := context.WithCancel(appCtx)
	r.cancel = cancel
	r.mu.Unlock()
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.ListenPacket(ctx, "udp", r.address)
	if err != nil {
		cancel()
		err = fmt.Errorf("bind actor loopback UDP listener: %w", err)
		r.finishInternal(err)
		return err
	}
	if err = listener.Close(); err != nil {
		cancel()
		r.finishInternal(err)
		return err
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, 120*time.Second)
	defer startupCancel()
	// An ephemeral probe socket can claim the port between the preflight and host bind.
	bindRetries := 0

	for {
		host, errCh, err := r.runHostInternal(runCtx)
		if err != nil {
			cancel()
			r.finishInternal(err)
			return err
		}
		select {
		case <-host.Ready():
			err = waitForPeerInternal(startupCtx, r.address, errCh)
			if err != nil {
				if errors.Is(err, syscall.EADDRINUSE) && bindRetries < 3 {
					<-errCh
					bindRetries++
					continue
				}
				cancel()
				r.finishInternal(<-errCh)
				return err
			}
			select {
			case runErr := <-errCh:
				cancel()
				r.finishInternal(runErr)
				return fmt.Errorf("actor host stopped during startup: %w", runErr)
			default:
			}
			r.publishReadyInternal(runCtx, host, errCh, onFailure)
			return nil
		case err = <-errCh:
			if errors.Is(err, syscall.EADDRINUSE) && bindRetries < 3 {
				bindRetries++
				continue
			}
			if !errors.Is(err, components.ErrClusterFull) && !errors.Is(err, components.ErrHostAlreadyRegistered) {
				cancel()
				r.finishInternal(err)
				return err
			}
			slog.WarnContext(ctx, "Waiting for the previous actor host registration to expire", "error", err)
		case <-startupCtx.Done():
			cancel()
			r.finishInternal(<-errCh)
			return startupCtx.Err()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-runCtx.Done():
			timer.Stop()
			r.finishInternal(runCtx.Err())
			return runCtx.Err()
		case <-startupCtx.Done():
			timer.Stop()
			cancel()
			err = fmt.Errorf("another Arcane process owns this database or its registration has not expired: %w", startupCtx.Err())
			r.finishInternal(err)
			return err
		}
	}
}

func (r *Runtime) runHostInternal(ctx context.Context) (*local.Host, chan error, error) {
	host, err := local.NewHost(r.options...)
	if err != nil {
		return nil, nil, err
	}
	for _, registration := range r.registrations {
		if err := host.RegisterActor(registration.actorType, registration.factory, registration.options...); err != nil {
			// Running a canceled host closes its provider-owned connections.
			cleanupCtx, cancel := context.WithCancel(ctx)
			cancel()
			if cleanupErr := host.Run(cleanupCtx); cleanupErr != nil {
				return nil, nil, errors.Join(err, fmt.Errorf("clean up unregistered actor host: %w", cleanupErr))
			}
			return nil, nil, err
		}
	}
	errors := make(chan error, 1)
	go func() { errors <- host.Run(ctx) }()
	return host, errors, nil
}

func (r *Runtime) publishReadyInternal(ctx context.Context, host *local.Host, runErrors <-chan error, onFailure func(error)) {
	*r.service = *host.Service()
	close(r.ready)
	go func() {
		err := <-runErrors
		r.finishInternal(err)
		if ctx.Err() != nil || onFailure == nil {
			return
		}
		if err == nil {
			err = errors.New("actor host stopped unexpectedly")
		}
		onFailure(err)
	}()
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel, started := r.cancel, r.started
	r.mu.Unlock()
	if !started {
		return nil
	}
	cancel()
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.runError
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) finishInternal(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runError = err
	close(r.done)
}

func waitForPeerInternal(ctx context.Context, address string, runErrors chan error) error {
	// A TLS rejection proves the loopback listener is serving before shutdown.
	//nolint:gosec // This readiness probe never sends credentials or application data.
	tlsConfig := &tls.Config{InsecureSkipVerify: true, NextProtos: []string{http3.NextProtoH3}}
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		connection, err := quic.DialAddr(probeCtx, address, tlsConfig, &quic.Config{})
		cancel()
		if connection != nil {
			if err := connection.CloseWithError(0, "readiness probe complete"); err != nil {
				return fmt.Errorf("close actor readiness probe: %w", err)
			}
			return nil
		}
		var transportError *quic.TransportError
		if errors.As(err, &transportError) && transportError.Remote {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case runErr := <-runErrors:
			timer.Stop()
			runErrors <- runErr
			return fmt.Errorf("actor peer listener stopped before readiness: %w", runErr)
		case <-timer.C:
		}
	}
}
