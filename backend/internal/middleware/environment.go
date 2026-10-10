package middleware

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	httpxtypes "github.com/getarcaneapp/arcane/types/v2/httpx"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/labstack/echo/v5"
	"github.com/samber/mo"
	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"

	// proxyTimeout is generous because proxied operations such as image pulls can stream for minutes.
	proxyTimeout = 30 * time.Minute

	maxProxiedWorkspaceManifestBytes = 1024 * 1024
	maxProxiedGitOpsSyncBodyBytes    = 1024 * 1024
)

// managementEndpointSet contains paths handled locally and never proxied to remote environments.
var managementEndpointSet = map[string]struct{}{
	"/test":            {},
	"/heartbeat":       {},
	"/sync-registries": {},
	"/sync":            {},
	"/deployment":      {},
	"/agent/pair":      {},
	"/version":         {},
	"/settings":        {},
	"/job-schedules":   {},
	"/jobs":            {},
}

// EnvResolver resolves an environment ID to its connection details.
// Returns: apiURL, accessToken, enabled, error
type EnvResolver func(ctx context.Context, id string) (string, *string, bool, error)

// AuthValidator authenticates a request and returns the caller's permission set and user (nil for non-user callers).
// Sudo permission sets (internal agent proxies) bypass authorization.
type AuthValidator func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *usertypes.Actor, bool)

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID       string
	paramName     string
	resolver      EnvResolver
	authValidator AuthValidator
	httpClient    *http.Client
	registry      *edge.TunnelRegistry
	matcher       *authz.PermissionMatcher
	// checkOrigin is the local WebSocket Origin validator, so proxied upgrades cannot ride a cross-origin session.
	checkOrigin func(*http.Request) bool
}

// NewEnvProxyMiddlewareWithParamAndRegistry creates middleware with an injected tunnel registry.
func NewEnvProxyMiddlewareWithParamAndRegistry(
	localID,
	paramName string,
	resolver EnvResolver,
	authValidator AuthValidator,
	matcher *authz.PermissionMatcher,
	registry *edge.TunnelRegistry,
	checkOrigin func(*http.Request) bool,
) echo.MiddlewareFunc {
	if registry == nil {
		registry = edge.NewTunnelRegistry()
	}

	// Traced so direct agents continue the manager's trace.
	httpClient := httpx.NewHTTPClient(httpxtypes.ClientOptions{Timeout: proxyTimeout, TLSHandshakeTimeout: 10 * time.Second})
	httpClient.Transport = otelhttp.NewTransport(httpClient.Transport)

	m := &EnvironmentMiddleware{
		localID:       localID,
		paramName:     paramName,
		resolver:      resolver,
		authValidator: authValidator,
		httpClient:    httpClient,
		registry:      registry,
		matcher:       matcher,
		checkOrigin:   checkOrigin,
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			return m.Handle(c, next)
		}
	}
}

// Handle is the main middleware handler.
func (m *EnvironmentMiddleware) Handle(c *echo.Context, next echo.HandlerFunc) error {
	req := c.Request()
	ctx := req.Context()

	_, rest, hasMarker := strings.Cut(req.URL.Path, environmentsPathMarker)
	pathEnvID, _, _ := strings.Cut(rest, "/")
	envID := cmp.Or(c.Param(m.paramName), pathEnvID)
	if !hasMarker || envID == "" || envID == m.localID {
		return next(c)
	}

	suffix, ok := strings.CutPrefix(req.URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' || isManagementPath(req.Method, suffix) {
		return next(c)
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	var perms *authz.PermissionSet
	var user *usertypes.Actor
	if m.authValidator != nil {
		var authenticated bool
		perms, user, authenticated = m.authValidator(ctx, c)
		if !authenticated {
			return writeProxyError(c, http.StatusUnauthorized, "Authentication required to access remote environments")
		}
		if user != nil {
			RecordAuthenticatedRequest(ctx, req.Header, user)
		}
	}

	// SECURITY: forwarded identity headers are always cleared first; only server-resolved values are sent.
	req.Header.Del(HeaderIconCatalog)
	if user != nil && user.Preferences.IconCatalog != nil && *user.Preferences.IconCatalog != "" {
		req.Header.Set(HeaderIconCatalog, *user.Preferences.IconCatalog)
	}
	req.Header.Del(HeaderUpdateInitiatorID)
	req.Header.Del(HeaderUpdateInitiatorName)
	req.Header.Del(HeaderUpdateInitiatorDisplayName)
	isContainerUpdate := req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/containers/") && strings.HasSuffix(req.URL.Path, "/update")
	if isContainerUpdate && user != nil && user.ID != "" {
		req.Header.Set(HeaderUpdateInitiatorID, user.ID)
		req.Header.Set(HeaderUpdateInitiatorName, user.Username)
		if user.DisplayName != nil {
			req.Header.Set(HeaderUpdateInitiatorDisplayName, *user.DisplayName)
		}
	}

	apiURL, accessToken, enabled, err := m.resolver(ctx, envID)
	if err != nil || apiURL == "" {
		return writeProxyError(c, http.StatusNotFound, "Environment not found")
	}
	if !enabled {
		return writeProxyError(c, http.StatusBadRequest, "Environment is disabled")
	}

	// SECURITY: Remote agents trust the manager as sudo, so per-environment authorization happens here.
	if m.proxyPermissionDenied(c, perms, envID, suffix) {
		return writeProxyError(c, http.StatusForbidden, "You don't have permission to perform this action on this environment")
	}

	tunnel, hasTunnel := m.getActiveEdgeTunnel(envID).Get()
	if !hasTunnel && strings.HasPrefix(strings.ToLower(strings.TrimSpace(apiURL)), "edge://") {
		if tunnel, hasTunnel = m.waitForActiveEdgeTunnel(ctx, envID).Get(); !hasTunnel {
			slog.WarnContext(ctx, "No active edge tunnel for environment", "environmentId", envID)
			return writeProxyError(c, http.StatusBadGateway, "Edge agent is not connected")
		}
		slog.InfoContext(ctx, "Recovered edge tunnel during request", "environmentId", envID)
	}

	proxyPath := path.Join(apiEnvironmentsPrefix, m.localID) + suffix
	isWebSocket := httpx.IsWebSocketUpgradeRequest(req)
	if hasTunnel {
		slog.DebugContext(ctx, "Routing request through edge tunnel", "environmentId", envID, "path", req.URL.Path)
		edge.SetAgentToken(req, accessToken)
		if isWebSocket {
			return edge.ProxyWebSocketRequest(c, tunnel, proxyPath, m.checkOrigin)
		}
		return edge.ProxyHTTPRequest(c, tunnel, proxyPath)
	}

	target := strings.TrimRight(apiURL, "/") + proxyPath
	if req.URL.RawQuery != "" {
		target += "?" + req.URL.RawQuery
	}
	if isWebSocket {
		if proxyErr := ws.ProxyHTTP(c.Response(), req, edge.HTTPToWebSocketURL(target), edge.BuildWebSocketHeaders(c, accessToken), m.checkOrigin); proxyErr != nil {
			slog.ErrorContext(ctx, "websocket proxy failed", "err", proxyErr)
		}
		return nil
	}

	targetURL, err := httpx.ValidateOutboundHTTPURL(target)
	if err != nil {
		return writeProxyError(c, http.StatusInternalServerError, "Invalid proxy target URL: "+err.Error())
	}

	// Stream the body unbuffered (uploads reach gigabytes); ContentLength 0 means no body, -1 means chunked.
	var requestBody io.ReadCloser
	var contentLength int64
	switch {
	case req.Body == nil:
	case req.ContentLength != 0:
		requestBody, contentLength = req.Body, req.ContentLength
	default:
		_ = req.Body.Close()
	}

	// The body is never logged: it can carry compose files and registry credentials.
	slog.DebugContext(ctx, "Creating proxy request", "method", req.Method, "target", target, "contentLength", req.ContentLength, "contentType", req.Header.Get("Content-Type"))

	proxyReq := (&http.Request{
		Method:        req.Method,
		URL:           targetURL,
		Host:          targetURL.Host,
		Header:        make(http.Header),
		Body:          requestBody,
		ContentLength: contentLength,
	}).WithContext(ctx)

	edge.CopyRequestHeaders(req.Header, proxyReq.Header)
	edge.SetAuthHeader(proxyReq, c)
	edge.SetAgentToken(proxyReq, accessToken)
	edge.SetForwardedHeaders(proxyReq, c.RealIP(), req.Host)

	resp, err := m.httpClient.Do(proxyReq)
	if err != nil {
		return writeProxyError(c, http.StatusBadGateway, "Proxy request failed: "+err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	// An upstream 401 means the agent rejected the manager's credentials; a 502 keeps browsers from dropping the session.
	if resp.StatusCode == http.StatusUnauthorized {
		slog.WarnContext(ctx, "Remote environment rejected the manager's credentials", "target", resp.Request.URL.Host)
		return writeProxyError(c, http.StatusBadGateway, "Remote environment rejected the manager's credentials")
	}
	w := c.Response()
	edge.CopyResponseHeaders(resp.Header, w.Header(), edge.BuildHopByHopHeaders(resp.Header))
	w.WriteHeader(resp.StatusCode)
	if req.Method != http.MethodHead {
		edge.CopyBodyWithFlush(w, resp.Body)
	}
	return nil
}

// proxyPermissionDenied reports whether the caller lacks permission for the proxied request, mirroring local
// RequirePermission checks. Sudo bypasses it; routes without a permission mapping are denied by default.
func (m *EnvironmentMiddleware) proxyPermissionDenied(c *echo.Context, ps *authz.PermissionSet, envID, suffix string) bool {
	if m.matcher == nil || (ps != nil && ps.Sudo) {
		return false
	}

	req := c.Request()
	ctx := req.Context()
	method := req.Method
	segments := strings.Split(strings.Trim(suffix, "/"), "/")

	// Upload-session routes derive their permission from the {kind} segment; unknown kinds fail closed.
	if len(segments) >= 2 && segments[0] == "uploads" && segments[1] != "" {
		var isUploadSession bool
		switch len(segments) {
		case 2:
			isUploadSession = method == http.MethodPost
		case 3:
			isUploadSession = method == http.MethodGet || method == http.MethodDelete
		case 5:
			isUploadSession = method == http.MethodPut && segments[3] == "chunks"
		}
		if isUploadSession {
			perm, known := authz.UploadKindPermission(segments[1])
			denied := !known || !ps.Allows(perm, envID)
			if denied {
				slog.DebugContext(ctx, "Denying proxied upload session request: permission denied",
					"method", method, "path", suffix, "kind", segments[1], "environmentId", envID)
			}
			return denied
		}
	}

	perm, ok := m.matcher.Lookup(method, suffix).Get()
	if !ok {
		slog.WarnContext(ctx, "Denying proxied request with no known permission mapping", "method", method, "path", suffix, "environmentId", envID)
		return true
	}
	if perm == "" {
		// Explicitly public route: allowed for any authenticated caller.
		return false
	}
	if !ps.Allows(perm, kit.Ternary(authz.IsEnvScoped(perm), envID, "")) {
		slog.DebugContext(ctx, "Denying proxied request: permission denied", "method", method, "path", suffix, "permission", perm, "environmentId", envID)
		return true
	}

	var required []string
	var err error
	if method == http.MethodPut && len(segments) == 3 && segments[0] == "volumes" && segments[1] != "" && segments[2] == "workspace" {
		required, err = proxiedVolumeWorkspacePermissions(req)
	} else {
		required, err = proxiedGitOpsSyncPermissions(req, method, segments)
	}
	if err != nil {
		slog.DebugContext(ctx, "Denying proxied request with invalid body", "path", suffix, "environmentId", envID, "error", err)
		return true
	}
	// Resource-sorted container lists collect per-container stats, so they also need containers:read.
	if method == http.MethodGet && len(segments) == 1 && segments[0] == "containers" && container.IsResourceSort(req.URL.Query().Get("sort")) {
		required = append(required, authz.PermContainersRead)
	}
	for _, operationPermission := range required {
		if !ps.Allows(operationPermission, envID) {
			slog.DebugContext(ctx, "Denying proxied request: body-derived permission denied",
				"path", suffix, "permission", operationPermission, "environmentId", envID)
			return true
		}
	}
	return false
}

func proxiedVolumeWorkspacePermissions(request *http.Request) ([]string, error) {
	if request.Body == nil {
		return nil, errors.New("missing multipart request body")
	}
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, errors.New("invalid multipart content type")
	}

	originalBody := request.Body
	// System temp scratch for the multipart replay buffer: no acfs root exists for it.
	captured, err := os.CreateTemp("", "arcane-volume-workspace-manifest-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace manifest buffer: %w", err)
	}
	defer func() {
		_, _ = captured.Seek(0, io.SeekStart)
		request.Body = &proxiedReplayBody{Reader: io.MultiReader(captured, originalBody), captured: captured, original: originalBody}
	}()

	reader := multipart.NewReader(io.TeeReader(originalBody, captured), params["boundary"])
	for {
		part, nextErr := reader.NextPart()
		if nextErr != nil {
			return nil, fmt.Errorf("find workspace manifest part: %w", nextErr)
		}
		if part.FormName() != "manifest" || part.FileName() != "" {
			if closeErr := part.Close(); closeErr != nil {
				return nil, fmt.Errorf("skip multipart field before workspace manifest: %w", closeErr)
			}
			continue
		}
		manifestJSON, readErr := io.ReadAll(io.LimitReader(part, maxProxiedWorkspaceManifestBytes+1))
		closeErr := part.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read workspace manifest: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close workspace manifest part: %w", closeErr)
		}
		if len(manifestJSON) > maxProxiedWorkspaceManifestBytes {
			return nil, errors.New("workspace manifest is too large")
		}
		var manifest volume.WorkspaceUpdateManifest
		if unmarshalErr := json.Unmarshal(manifestJSON, &manifest); unmarshalErr != nil {
			return nil, fmt.Errorf("decode workspace manifest: %w", unmarshalErr)
		}
		required, valid := authz.VolumeWorkspaceRequiredPermissions(manifest.FileChanges)
		if !valid {
			return nil, errors.New("workspace manifest contains an unknown operation")
		}
		return required, nil
	}
}

// proxiedGitOpsSyncPermissions mirrors the handler lifecycle and backup gates for gitops sync create, import,
// and update, since the agent trusts forwarded requests as sudo. Returns nil for any other route.
func proxiedGitOpsSyncPermissions(request *http.Request, method string, segments []string) ([]string, error) {
	isCreate := len(segments) == 1 && segments[0] == "gitops-syncs" && method == http.MethodPost
	isImport := len(segments) == 2 && segments[0] == "gitops-syncs" && segments[1] == "import" && method == http.MethodPost
	isUpdate := len(segments) == 2 && segments[0] == "gitops-syncs" && segments[1] != "" && method == http.MethodPut
	if !isCreate && !isImport && !isUpdate || request.Body == nil {
		return nil, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(request.Body, maxProxiedGitOpsSyncBodyBytes+1))
	closeErr := request.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read gitops sync body: %w", err)
	}
	if len(body) > maxProxiedGitOpsSyncBodyBytes {
		return nil, errors.New("gitops sync body is too large")
	}
	request.Body = io.NopCloser(bytes.NewReader(body))

	type syncBody struct {
		gitops.PreDeployConfigRequest

		Mode string `json:"mode"`
	}
	var items []syncBody
	var decodeErr error
	if isImport {
		decodeErr = json.Unmarshal(body, &items)
	} else {
		items = make([]syncBody, 1)
		decodeErr = json.Unmarshal(body, &items[0])
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode gitops sync body: %w", decodeErr)
	}

	var required []string
	if slices.ContainsFunc(items, func(item syncBody) bool { return item.HasPreDeployConfig() }) {
		required = append(required, authz.PermGitOpsLifecycle)
	}
	if isCreate && slices.ContainsFunc(items, func(item syncBody) bool { return item.Mode == gitops.SyncModeBackup }) {
		required = append(required, authz.PermGitOpsBackup)
	}
	return required, nil
}

type proxiedReplayBody struct {
	io.Reader

	captured *os.File
	original io.ReadCloser
}

func (b *proxiedReplayBody) Close() error {
	capturedErr := b.captured.Close()
	originalErr := b.original.Close()
	// System temp scratch: no acfs root exists for it.
	removeErr := os.Remove(b.captured.Name())
	return errors.Join(capturedErr, originalErr, removeErr)
}

// isManagementPath reports whether an environment-scoped path is served by the manager instead of being proxied.
func isManagementPath(method, suffix string) bool {
	if _, ok := managementEndpointSet[suffix]; ok {
		return true
	}
	// Webhooks live in the manager DB and their public trigger endpoint resolves tokens there.
	for _, prefix := range []string{"/jobs", "/activities", "/webhooks"} {
		if suffix == prefix || strings.HasPrefix(suffix, prefix+"/") {
			return true
		}
	}
	if strings.HasPrefix(suffix, "/notifications") || strings.HasPrefix(suffix, "/deployment/mtls/") {
		return true
	}

	switch method + " " + suffix {
	case "GET /swarm/join-candidates", "GET /swarm/nodes", "POST /swarm/join-environments", "POST /swarm/nodes/agents/reconcile":
		return true
	}

	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 3 && parts[0] == "volumes" && parts[1] != "" && parts[2] == "backup-policy" {
		return method == http.MethodGet || method == http.MethodPut
	}
	if len(parts) < 3 || parts[0] != "swarm" || parts[1] != "nodes" {
		return false
	}
	switch {
	case len(parts) == 3:
		return method == http.MethodGet
	case len(parts) == 5 && parts[3] == "agent" && parts[4] == "binding":
		return method == http.MethodPut || method == http.MethodDelete
	case len(parts) == 5 && parts[3] == "agent" && parts[4] == "deployment":
		return method == http.MethodPost || method == http.MethodDelete
	}
	return false
}

func (m *EnvironmentMiddleware) getActiveEdgeTunnel(envID string) mo.Option[*edge.AgentTunnel] {
	if m.registry == nil {
		return mo.None[*edge.AgentTunnel]()
	}

	tunnel, ok := m.registry.Get(envID).Get()
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return mo.None[*edge.AgentTunnel]()
	}
	return mo.Some(tunnel)
}

// waitForActiveEdgeTunnel signals tunnel demand and polls until the edge agent connects or the acquire timeout elapses.
func (m *EnvironmentMiddleware) waitForActiveEdgeTunnel(ctx context.Context, envID string) mo.Option[*edge.AgentTunnel] {
	edge.TouchTunnelDemand(envID, edge.DefaultTunnelDemandTTL)

	waitCtx, cancel := context.WithTimeout(ctx, edge.DefaultTunnelAcquireTimeout())
	defer cancel()

	ticker := time.NewTicker(edge.DefaultTunnelAcquirePollEvery)
	defer ticker.Stop()

	for {
		if tunnel := m.getActiveEdgeTunnel(envID); tunnel.IsPresent() {
			return tunnel
		}
		select {
		case <-waitCtx.Done():
			return mo.None[*edge.AgentTunnel]()
		case <-ticker.C:
		}
	}
}

func writeProxyError(c *echo.Context, status int, message string) error {
	return c.JSON(status, map[string]any{
		"success": false,
		"data":    map[string]any{"error": message},
	})
}
