package services

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/hostshell"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	buildapi "go.getarcane.app/builds/api"
)

// Host-shell session limits. These bound the blast radius of a feature that
// grants full root on the Docker host: at most a handful of concurrent
// sessions, and every session is torn down on its own even if the client
// that opened it vanishes without closing cleanly.
const (
	hostShellMaxConcurrentSessions = 3
	hostShellIdleTimeout           = 15 * time.Minute
	hostShellMaxLifetime           = 4 * time.Hour
	hostShellRemoveTimeout         = 30 * time.Second

	// hostShellScriptDefaultMaxOutputBytes bounds RunScript output when the
	// caller does not specify ScriptRequest.MaxOutputBytes.
	hostShellScriptDefaultMaxOutputBytes = 16 * 1024
)

// Sentinel errors returned by HostShellService entry points, distinct from
// the hostshell package's preflight/validation sentinels so callers can
// distinguish "feature disabled" and "at capacity" from "this Docker host
// can't do this at all".
var (
	// ErrHostShellDisabled means the hostTerminalEnabled setting is off.
	ErrHostShellDisabled = errors.New("host shell is disabled")
	// ErrHostShellSessionLimit means hostShellMaxConcurrentSessions
	// interactive sessions are already open on this instance.
	ErrHostShellSessionLimit = errors.New("too many concurrent host shell sessions")
)

// HostShellService runs commands on the real Docker host — not inside a
// container — via a privileged throwaway helper container that nsenters
// into PID 1's namespaces (see pkg/libarcane/hostshell). It backs both the
// interactive host terminal and (in a later phase) Snippets host-targeted
// runs.
//
// Every entry point is full root on the host. Callers must not skip the
// Enabled gate, and StartInteractive/RunScript both refuse to proceed when
// hostshell.Preflight rejects the connected daemon (non-Linux, Docker
// Desktop, rootless).
type HostShellService struct {
	dockerService    *DockerClientService
	containerService *ContainerService
	settingsService  *SettingsService
	eventService     *EventService

	mu          sync.Mutex
	activeCount int
	sessions    map[string]*HostShellSession
}

func NewHostShellService(
	dockerService *DockerClientService,
	containerService *ContainerService,
	settingsService *SettingsService,
	eventService *EventService,
) *HostShellService {
	return &HostShellService{
		dockerService:    dockerService,
		containerService: containerService,
		settingsService:  settingsService,
		eventService:     eventService,
		sessions:         make(map[string]*HostShellSession),
	}
}

// Enabled reports whether host-shell access is turned on for this instance.
// This is the only gate StartInteractive/RunScript check for the opt-in —
// callers must not cache the result across a request.
func (s *HostShellService) Enabled(ctx context.Context) bool {
	return s.settingsService.GetBoolSetting(ctx, "hostTerminalEnabled", false)
}

// HostShellActor identifies who opened a session, for audit events. In the
// env-proxy case this must be the human user forwarded from the manager, not
// the agent's own sudo identity — that header-forwarding fix belongs to the
// WS route wiring, not this service.
type HostShellActor struct {
	UserID    *string
	Username  *string
	ClientIP  string
	UserAgent string
}

// StartInteractiveRequest configures an interactive host-shell session.
type StartInteractiveRequest struct {
	// Shell is validated by hostshell.ValidateShell; it must be an absolute
	// path with no arguments.
	Shell string
	Actor HostShellActor
}

// HostShellSession is a live interactive host-shell exec session. Callers
// pump Stdin()/Stdout() (typically from a WebSocket) and must call Touch on
// every read and write so the idle timer reflects real activity, and must
// call Close exactly once when the session ends (Close is safe to call more
// than once; only the first call has effect).
type HostShellSession struct {
	id           string
	shell        string
	containerID  string
	execID       string
	image        string
	execSession  *ExecSession
	dockerClient *client.Client
	startedAt    time.Time

	closeOnce     sync.Once
	idleTimer     *time.Timer
	lifetimeTimer *time.Timer
	onClose       func(reason string, durationMs int64)
}

func (sess *HostShellSession) ID() string            { return sess.id }
func (sess *HostShellSession) Stdin() io.WriteCloser { return sess.execSession.Stdin() }
func (sess *HostShellSession) Stdout() io.Reader     { return sess.execSession.Stdout() }

// Touch resets the idle timeout. Call this from both the input and output
// pumps so a session with traffic in either direction stays alive.
func (sess *HostShellSession) Touch() {
	if sess.idleTimer != nil {
		sess.idleTimer.Reset(hostShellIdleTimeout)
	}
}

// Resize resizes the session's TTY.
func (sess *HostShellSession) Resize(ctx context.Context, cols, rows uint) error {
	_, err := sess.dockerClient.ExecResize(ctx, sess.execID, client.ExecResizeOptions{Height: rows, Width: cols})
	return err
}

// Close ends the session: it terminates the exec, force-removes the helper
// container, stops the idle/lifetime timers, and invokes the registered
// close callback exactly once. ctx is only used to derive a
// context.WithoutCancel base for the teardown calls, so a session whose
// originating request context is already canceled (e.g. the client
// disconnected) still tears down cleanly.
func (sess *HostShellSession) Close(ctx context.Context, reason string) {
	sess.closeOnce.Do(func() {
		if sess.idleTimer != nil {
			sess.idleTimer.Stop()
		}
		if sess.lifetimeTimer != nil {
			sess.lifetimeTimer.Stop()
		}

		closeCtx := context.WithoutCancel(ctx)
		_ = sess.execSession.Close(closeCtx)
		forceRemoveHostShellHelperInternal(closeCtx, sess.dockerClient, sess.containerID)

		durationMs := time.Since(sess.startedAt).Milliseconds()
		if sess.onClose != nil {
			sess.onClose(reason, durationMs)
		}
	})
}

// StartInteractive opens a new interactive host-shell session: it resolves
// and pulls the helper image if needed, starts a long-lived helper container
// running "sleep infinity" under host namespaces, and execs the requested
// shell into it via nsenter. The returned session's Stdin/Stdout back a
// WebSocket pump exactly like the existing container exec terminal.
func (s *HostShellService) StartInteractive(ctx context.Context, req StartInteractiveRequest) (*HostShellSession, error) {
	if !s.Enabled(ctx) {
		s.emitDeniedEventInternal(ctx, req.Actor, ErrHostShellDisabled)
		return nil, ErrHostShellDisabled
	}

	shell, err := hostshell.ValidateShell(req.Shell)
	if err != nil {
		s.emitDeniedEventInternal(ctx, req.Actor, err)
		return nil, err
	}

	if err := s.acquireSlotInternal(); err != nil {
		s.emitDeniedEventInternal(ctx, req.Actor, err)
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.releaseSlotInternal()
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	if err := s.preflightInternal(ctx, dockerClient); err != nil {
		s.releaseSlotInternal()
		s.emitDeniedEventInternal(ctx, req.Actor, err)
		return nil, err
	}

	image, err := hostshell.ResolveImage(ctx, dockerClient, s.settingsService.GetStringSetting(ctx, "hostTerminalImage", ""))
	if err != nil {
		s.releaseSlotInternal()
		return nil, err
	}

	sessionID := uuid.NewString()
	containerID, err := s.createHelperContainerInternal(ctx, dockerClient, image, sessionID)
	if err != nil {
		s.releaseSlotInternal()
		return nil, err
	}

	execID, err := s.containerService.CreateExec(ctx, containerID, hostshell.NsenterCommand("", []string{shell}), []string{"TERM=xterm-256color"})
	if err != nil {
		forceRemoveHostShellHelperInternal(ctx, dockerClient, containerID)
		s.releaseSlotInternal()
		return nil, err
	}

	execSession, err := s.containerService.AttachExec(ctx, containerID, execID)
	if err != nil {
		forceRemoveHostShellHelperInternal(ctx, dockerClient, containerID)
		s.releaseSlotInternal()
		return nil, err
	}

	// From here on, the session owns its slot and its helper container:
	// releasing the slot and tearing down the container both happen in
	// onClose, called from HostShellSession.Close.
	session := &HostShellSession{
		id:           sessionID,
		shell:        shell,
		containerID:  containerID,
		execID:       execID,
		image:        image,
		execSession:  execSession,
		dockerClient: dockerClient,
		startedAt:    time.Now(),
	}
	session.onClose = func(reason string, durationMs int64) {
		s.removeSessionInternal(sessionID)
		s.releaseSlotInternal()
		s.emitCloseEventInternal(context.WithoutCancel(ctx), req.Actor, session, reason, durationMs)
	}
	session.idleTimer = time.AfterFunc(hostShellIdleTimeout, func() { session.Close(context.Background(), "idle") })
	session.lifetimeTimer = time.AfterFunc(hostShellMaxLifetime, func() { session.Close(context.Background(), "max_lifetime") })

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	s.emitOpenEventInternal(ctx, req.Actor, session)

	return session, nil
}

// ScriptRequest configures a one-shot host-shell script run, backing
// Snippets. Script is delivered to "/bin/sh -s" on stdin — never as an argv
// element or a "-c" string — so a parameter value can never be interpreted
// as shell syntax; the only shell-injection surface is the script body
// itself, which is trusted author-supplied content. Env entries are applied
// as the exec process environment, never folded into Script or any other
// string field.
type ScriptRequest struct {
	Script         string
	Env            map[string]string
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int
}

// ScriptResult is the outcome of a completed RunScript call.
type ScriptResult struct {
	ExitCode int64
	Output   string
}

// RunScript runs Script once on the real Docker host inside a throwaway
// privileged helper container (the same mechanism as StartInteractive) and
// returns its combined, capped stdout+stderr and exit code. It shares
// Enabled/Preflight/ResolveImage and the session-count gate with
// StartInteractive so the total number of concurrent privileged helper
// containers on the host stays bounded regardless of which feature opened
// them.
//
// Callers own audit logging for the run itself (see SnippetService) — this
// method does not emit host.terminal.* events, since every snippet run
// already gets its own audit event and duplicating it here would double the
// events list for no benefit.
func (s *HostShellService) RunScript(ctx context.Context, req ScriptRequest) (ScriptResult, error) {
	if !s.Enabled(ctx) {
		return ScriptResult{}, ErrHostShellDisabled
	}

	if err := s.acquireSlotInternal(); err != nil {
		return ScriptResult{}, err
	}
	defer s.releaseSlotInternal()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return ScriptResult{}, errors.WrapIf(err, "failed to connect to Docker")
	}

	if err := s.preflightInternal(ctx, dockerClient); err != nil {
		return ScriptResult{}, err
	}

	image, err := hostshell.ResolveImage(ctx, dockerClient, s.settingsService.GetStringSetting(ctx, "hostTerminalImage", ""))
	if err != nil {
		return ScriptResult{}, err
	}

	sessionID := uuid.NewString()
	containerID, err := s.createHelperContainerInternal(ctx, dockerClient, image, sessionID)
	if err != nil {
		return ScriptResult{}, err
	}
	defer forceRemoveHostShellHelperInternal(ctx, dockerClient, containerID)

	cmd := hostshell.NsenterCommand(req.WorkingDir, []string{"/bin/sh", "-s"})
	execResp, err := dockerClient.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
		Env:          envMapToSliceInternal(req.Env),
	})
	if err != nil {
		return ScriptResult{}, errors.WrapIf(err, "failed to create host shell script exec")
	}

	attachResp, err := dockerClient.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return ScriptResult{}, errors.WrapIf(err, "failed to attach to host shell script exec")
	}
	defer attachResp.Close()

	if _, err := attachResp.Conn.Write([]byte(req.Script)); err != nil {
		return ScriptResult{}, errors.WrapIf(err, "failed to write script to host shell stdin")
	}
	// Half-close so /bin/sh -s sees EOF on stdin after the script and exits
	// once it finishes running it, instead of waiting for more input.
	_ = attachResp.CloseWrite()

	maxOutputBytes := req.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = hostShellScriptDefaultMaxOutputBytes
	}
	capture := buildapi.NewLogCapture(maxOutputBytes)

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(capture, capture, attachResp.Reader)
		copyDone <- copyErr
	}()

	timedOut := false
	select {
	case <-copyDone:
	case <-time.After(req.Timeout):
		timedOut = true
		// Force-close the attach connection so the copy goroutine's blocked
		// Read unblocks; the deferred forceRemoveHostShellHelperInternal
		// above kills the still-running process.
		attachResp.Close()
		<-copyDone
	}

	output := capture.String()
	if capture.Truncated() {
		output += "\n...<truncated>"
	}

	if timedOut {
		return ScriptResult{Output: output}, errors.WrapIf(context.DeadlineExceeded, "snippet script timed out")
	}

	inspect, err := dockerClient.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
	if err != nil {
		return ScriptResult{Output: output}, errors.WrapIf(err, "failed to inspect host shell script exec result")
	}

	return ScriptResult{ExitCode: int64(inspect.ExitCode), Output: output}, nil
}

// CleanupOrphaned force-removes any host-shell helper container left behind
// by a previous, uncleanly-terminated process (e.g. a crash between
// ContainerCreate and the deferred remove). Intended to run once at startup,
// alongside VolumeService's equivalent sweep for volume-browse helpers.
func (s *HostShellService) CleanupOrphaned(ctx context.Context) (int, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to get docker client for host shell orphan cleanup")
	}

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return 0, errors.WrapIf(err, "failed to list containers for host shell orphan cleanup")
	}

	removedCount := 0
	for _, c := range containers.Items {
		if !hostshell.IsHelperContainer(c.Labels) {
			continue
		}
		if _, err := dockerClient.ContainerRemove(ctx, c.ID, hostshell.RemoveOptions()); err != nil {
			slog.WarnContext(ctx, "host shell: failed to remove orphaned helper container",
				"containerID", c.ID, "containerNames", c.Names, "error", err.Error())
			continue
		}
		removedCount++
	}

	return removedCount, nil
}

// CleanupAll closes every live host-shell session. Intended for graceful
// shutdown (fx OnStop) so no privileged helper container outlives the
// process on a clean stop; CleanupOrphaned covers the unclean-stop case at
// the next startup.
func (s *HostShellService) CleanupAll(ctx context.Context) {
	s.mu.Lock()
	live := make([]*HostShellSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		live = append(live, sess)
	}
	s.mu.Unlock()

	for _, sess := range live {
		sess.Close(ctx, "shutdown")
	}
}

func (s *HostShellService) preflightInternal(ctx context.Context, dockerClient *client.Client) error {
	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return errors.WrapIf(err, "failed to get docker info")
	}
	return hostshell.Preflight(infoResult.Info)
}

func (s *HostShellService) createHelperContainerInternal(ctx context.Context, dockerClient *client.Client, image, sessionID string) (string, error) {
	config := hostshell.Config(image, sessionID, []string{"sleep", "infinity"}, nil, false)
	hostConfig := hostshell.HostConfig()

	apiTimeoutSec := s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt()

	createCtx, createCancel := context.WithTimeout(ctx, timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
	resp, err := dockerClient.ContainerCreate(createCtx, client.ContainerCreateOptions{Config: config, HostConfig: hostConfig})
	createCancel()
	if err != nil {
		return "", errors.WrapIf(err, "failed to create host shell helper container")
	}
	containerID := resp.ID

	startCtx, startCancel := context.WithTimeout(ctx, timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
	_, startErr := dockerClient.ContainerStart(startCtx, containerID, client.ContainerStartOptions{})
	startCancel()
	if startErr != nil {
		forceRemoveHostShellHelperInternal(ctx, dockerClient, containerID)
		return "", errors.WrapIf(startErr, "failed to start host shell helper container")
	}

	return containerID, nil
}

func (s *HostShellService) acquireSlotInternal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCount >= hostShellMaxConcurrentSessions {
		return ErrHostShellSessionLimit
	}
	s.activeCount++
	return nil
}

func (s *HostShellService) releaseSlotInternal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCount > 0 {
		s.activeCount--
	}
}

func (s *HostShellService) removeSessionInternal(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func forceRemoveHostShellHelperInternal(ctx context.Context, dockerClient *client.Client, containerID string) {
	removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostShellRemoveTimeout)
	defer cancel()
	if _, err := dockerClient.ContainerRemove(removeCtx, containerID, hostshell.RemoveOptions()); err != nil && !cerrdefs.IsNotFound(err) {
		slog.WarnContext(ctx, "host shell: failed to remove helper container", "containerID", containerID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Audit events. Metadata never carries keystrokes or command output — only
// session identifiers, the shell path, the helper container/image, and
// timing. Severity is warning for open/denied because this is root on the
// host and should stand out in the events list.
// ---------------------------------------------------------------------------

func (s *HostShellService) emitOpenEventInternal(ctx context.Context, actor HostShellActor, sess *HostShellSession) {
	_, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeHostTerminalOpen,
		Severity:     models.EventSeverityWarning,
		Title:        "Host terminal opened",
		Description:  "An interactive root shell was opened on the Docker host via a privileged helper container",
		ResourceType: new("host"),
		ResourceID:   new(sess.id),
		UserID:       actor.UserID,
		Username:     actor.Username,
		Metadata: models.JSON{
			"sessionId":         sess.id,
			"shell":             sess.shell,
			"helperContainerId": sess.containerID,
			"helperImage":       sess.image,
			"clientIp":          actor.ClientIP,
			"userAgent":         actor.UserAgent,
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit host.terminal.open event", "sessionID", sess.id, "error", err)
	}
}

func (s *HostShellService) emitCloseEventInternal(ctx context.Context, actor HostShellActor, sess *HostShellSession, reason string, durationMs int64) {
	_, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeHostTerminalClose,
		Severity:     models.EventSeverityInfo,
		Title:        "Host terminal closed",
		Description:  "The host terminal session ended",
		ResourceType: new("host"),
		ResourceID:   new(sess.id),
		UserID:       actor.UserID,
		Username:     actor.Username,
		Metadata: models.JSON{
			"sessionId":         sess.id,
			"shell":             sess.shell,
			"helperContainerId": sess.containerID,
			"helperImage":       sess.image,
			"durationMs":        durationMs,
			"reason":            reason,
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit host.terminal.close event", "sessionID", sess.id, "error", err)
	}
}

func (s *HostShellService) emitDeniedEventInternal(ctx context.Context, actor HostShellActor, cause error) {
	_, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeHostTerminalDenied,
		Severity:     models.EventSeverityWarning,
		Title:        "Host terminal denied",
		Description:  cause.Error(),
		ResourceType: new("host"),
		UserID:       actor.UserID,
		Username:     actor.Username,
		Metadata: models.JSON{
			"clientIp":  actor.ClientIP,
			"userAgent": actor.UserAgent,
			"reason":    cause.Error(),
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit host.terminal.denied event", "error", err)
	}
}
