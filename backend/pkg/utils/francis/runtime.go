// Package francis owns the embedded Francis host lifecycle.
package francis

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/components"
	"github.com/italypaleale/francis/host/local"
	"github.com/libtnb/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TablePrefix namespaces the Francis tables in their store.
const TablePrefix = "arcane_francis"

// Runtime starts one durable actor host and publishes its service after readiness.
// Services must wait for Ready before using Service, including during startup retries.
type Runtime struct {
	mu            sync.Mutex
	options       []local.HostOption
	registrations []registration
	service       *actor.Service
	ready         chan struct{}
	done          chan struct{}
	cancel        context.CancelFunc
	started       bool
	runError      error
	address       string
	storeURL      string
	store         *gorm.DB
	actorNames    sync.Map
}

type registration struct {
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
	storeURL, err := StoreURL(databaseURL)
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort("127.0.0.1", port)
	runtime := &Runtime{
		service: &actor.Service{}, ready: make(chan struct{}), done: make(chan struct{}), address: address, storeURL: storeURL,
		options: []local.HostOption{
			local.WithAddress(address), providerOption(storeURL),
			local.WithMaxHosts(1), local.WithHostHealthCheckDeadline(90 * time.Second),
			local.WithShutdownGracePeriod(10 * time.Second), local.WithAlarmsPollInterval(time.Second),
			local.WithAlarmsFetchAheadInterval(30 * time.Second), local.WithAlarmsLeaseDuration(180 * time.Second),
		},
	}
	runtime.options = append(runtime.options, local.WithLogger(slog.New(&actorLogHandlerInternal{
		Handler: slog.Default().With("scope", "francis.arcane.internal").Handler(),
		names:   &runtime.actorNames,
	})))
	if encryptionKey != "" && instanceID != "" {
		if configureIdentityErr := runtime.ConfigureIdentity(encryptionKey, instanceID); configureIdentityErr != nil {
			return nil, configureIdentityErr
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

// Store lazily opens a small pool on Francis storage for diagnostics and backups.
func (r *Runtime) Store() (*gorm.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		return r.store, nil
	}
	dialector := postgres.Open(r.storeURL)
	if strings.HasPrefix(r.storeURL, "file:") {
		dialector = sqlite.Open(r.storeURL + "?_pragma=busy_timeout(2500)")
	}
	store, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("open actor storage: %w", err)
	}
	sqlDB, err := store.DB()
	if err != nil {
		return nil, fmt.Errorf("open actor storage: %w", err)
	}
	sqlDB.SetMaxOpenConns(2)
	r.store = store
	return store, nil
}

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
	r.registrations = append(r.registrations, registration{actorType: actorType, factory: factory, options: append([]local.RegisterActorOption(nil), options...)})
	return nil
}

// Start waits for host registration. appCtx owns the running host; ctx only
// limits startup. onFailure must cancel the application without blocking.
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
		r.finish(err)
		return err
	}
	if err = listener.Close(); err != nil {
		cancel()
		r.finish(err)
		return err
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, 120*time.Second)
	defer startupCancel()
	bindRetries := 0

	for {
		host, errCh, runHostErr := r.runHost(runCtx)
		if runHostErr != nil {
			cancel()
			r.finish(runHostErr)
			return runHostErr
		}
		select {
		case <-host.Ready():
			select {
			case runHostErr = <-errCh:
			default:
				r.publishReady(runCtx, host, errCh, onFailure)
				return nil
			}
		case runHostErr = <-errCh:
		case <-startupCtx.Done():
			cancel()
			r.finish(<-errCh)
			return startupCtx.Err()
		}
		if runHostErr == nil {
			runHostErr = errors.New("actor host stopped during startup")
		}
		switch {
		case errors.Is(runHostErr, syscall.EADDRINUSE) && bindRetries < 3:
			bindRetries++
			slog.WarnContext(ctx, "Retrying actor host after a UDP port conflict", "error", runHostErr, "retry", bindRetries)
		case errors.Is(runHostErr, components.ErrClusterFull), errors.Is(runHostErr, components.ErrHostAlreadyRegistered):
			slog.WarnContext(ctx, "Waiting for the previous actor host registration to expire", "error", runHostErr)
		default:
			cancel()
			r.finish(runHostErr)
			return runHostErr
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-runCtx.Done():
			timer.Stop()
			r.finish(runCtx.Err())
			return runCtx.Err()
		case <-startupCtx.Done():
			timer.Stop()
			cancel()
			runHostErr = fmt.Errorf("actor host startup timed out: %w", errors.Join(startupCtx.Err(), runHostErr))
			r.finish(runHostErr)
			return runHostErr
		}
	}
}

func (r *Runtime) runHost(ctx context.Context) (*local.Host, chan error, error) {
	host, err := local.NewHost(r.options...)
	if err != nil {
		return nil, nil, err
	}
	for _, registration := range r.registrations {
		if registerActorErr := host.RegisterActor(registration.actorType, registration.factory, registration.options...); registerActorErr != nil {
			// Running a canceled host closes its provider-owned connections.
			cleanupCtx, cancel := context.WithCancel(ctx)
			cancel()
			if cleanupErr := host.Run(cleanupCtx); cleanupErr != nil {
				return nil, nil, errors.Join(registerActorErr, fmt.Errorf("clean up unregistered actor host: %w", cleanupErr))
			}
			return nil, nil, registerActorErr
		}
	}
	localErrors := make(chan error, 1)
	go func() { localErrors <- host.Run(ctx) }()
	return host, localErrors, nil
}

func (r *Runtime) publishReady(ctx context.Context, host *local.Host, runErrors <-chan error, onFailure func(error)) {
	*r.service = *host.Service()
	close(r.ready)
	go func() {
		err := <-runErrors
		r.finish(err)
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
	var err error
	if r.store != nil {
		var sqlDB *sql.DB
		if sqlDB, err = r.store.DB(); err == nil {
			err = sqlDB.Close()
		}
		r.store = nil
	}
	if !r.started {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	r.cancel()
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return errors.Join(r.runError, err)
	case <-ctx.Done():
		return errors.Join(ctx.Err(), err)
	}
}

func (r *Runtime) finish(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runError = err
	close(r.done)
}
