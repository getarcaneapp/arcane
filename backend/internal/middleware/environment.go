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
	"net"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ws"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

const (
	apiEnvironmentsPrefix  = "/api/environments/"
	environmentsPathMarker = "/environments/"

	maxProxiedWorkspaceManifestBytes = 1024 * 1024
	maxProxiedWorkspacePrefixBytes   = 16 * 1024 * 1024
	maxProxiedGitOpsSyncBodyBytes    = 1024 * 1024
)

var errProxiedBodyTooLarge = errors.New("proxied request body is too large")

// managementEndpointSet contains paths handled locally and never proxied to remote environments.
var managementEndpointSet = map[string]struct{}{
	"/test":       {},
	"/sync":       {},
	"/deployment": {},
	"/version":    {},
	"/settings":   {},
	"/jobs":       {},
}

// EnvResolver resolves an environment ID to its connection details.
// Returns: accessToken, enabled, error
type EnvResolver func(ctx context.Context, id string) (*string, bool, error)

// AuthValidator authenticates a request and returns the caller's permission set and user (nil for non-user callers).
// Sudo permission sets (internal agent proxies) bypass authorization.
type AuthValidator func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool)

// EnvironmentMiddleware proxies requests for remote environments to their respective agents.
type EnvironmentMiddleware struct {
	localID       string
	paramName     string
	resolver      EnvResolver
	authValidator AuthValidator
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

	m := &EnvironmentMiddleware{
		localID:       localID,
		paramName:     paramName,
		resolver:      resolver,
		authValidator: authValidator,
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
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		return next(c)
	}

	_, rest, hasMarker := strings.Cut(req.URL.Path, environmentsPathMarker)
	pathEnvID, _, _ := strings.Cut(rest, "/")
	envID := cmp.Or(c.Param(m.paramName), pathEnvID)
	if !hasMarker || envID == "" || envID == m.localID {
		return next(c)
	}

	suffix, ok := strings.CutPrefix(req.URL.Path, apiEnvironmentsPrefix+envID)
	if !ok || len(suffix) <= 1 || suffix[0] != '/' || IsManagerLocalPath(req.Method, suffix) {
		return next(c)
	}

	// SECURITY: Validate authentication BEFORE proxying to remote environments.
	var perms *authz.PermissionSet
	var user *types.Actor
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

	accessToken, enabled, err := m.resolver(ctx, envID)
	if err != nil {
		return writeProxyError(c, http.StatusNotFound, "Environment not found")
	}
	if !enabled {
		return writeProxyError(c, http.StatusBadRequest, "Environment is disabled")
	}

	// SECURITY: Remote agents trust the manager as sudo, so per-environment authorization happens here.
	status := m.proxyPermissionStatus(c, perms, envID, suffix)
	if replay, isReplay := req.Body.(*proxiedReplayBody); isReplay {
		defer func() { _ = replay.Close() }()
	}
	switch status {
	case http.StatusOK:
	case http.StatusForbidden:
		return writeProxyError(c, status, "You don't have permission to perform this action on this environment")
	case http.StatusRequestEntityTooLarge:
		return writeProxyError(c, status, "Request body is too large")
	default:
		return writeProxyError(c, status, "Invalid request body")
	}

	tunnel, unavailable := m.activeTunnel(ctx, envID)
	if unavailable {
		return writeProxyError(c, http.StatusServiceUnavailable, "Edge agent is not connected")
	}

	slog.DebugContext(ctx, "Routing request through edge tunnel", "environmentId", envID, "path", req.URL.Path)
	client := tunnel.HTTPClient()
	target := edge.MuxAgentBaseURL + path.Join(apiEnvironmentsPrefix, m.localID) + suffix
	if req.URL.RawQuery != "" {
		target += "?" + req.URL.RawQuery
	}
	if httpx.IsWebSocketUpgradeRequest(req) {
		if proxyErr := ws.ProxyHTTP(c.Response(), req, edge.HTTPToWebSocketURL(target), edge.BuildWebSocketHeaders(c, accessToken), m.checkOrigin, client); proxyErr != nil {
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

	resp, err := client.Do(proxyReq)
	if err != nil {
		netErr, isNetErr := errors.AsType[net.Error](err)
		return writeProxyError(c, kit.Ternary(isNetErr && netErr.Timeout(), http.StatusGatewayTimeout, http.StatusBadGateway), "Proxy request failed: "+err.Error())
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
		if copyErr := edge.CopyBodyWithFlush(w, resp.Body); copyErr != nil {
			slog.WarnContext(ctx, "Proxied response ended early", "environmentId", envID, "error", copyErr)
			// Headers are already sent, so abort the connection instead of ending a truncated response as complete.
			panic(http.ErrAbortHandler)
		}
	}
	return nil
}

// activeTunnel returns the environment's live tunnel, if any. Edge environments refresh poll demand on every
// request and wait briefly only for an agent with a fresh poll check-in; unavailable reports an offline edge agent.
func (m *EnvironmentMiddleware) activeTunnel(ctx context.Context, envID string) (tunnel *edge.AgentTunnel, unavailable bool) {
	edge.TouchTunnelDemand(envID, edge.DefaultTunnelDemandTTL)
	if active, ok := m.registry.Active(envID).Get(); ok {
		return active, false
	}
	if edge.GetPollRuntimeRegistry().Get(envID, time.Now()).IsAbsent() {
		slog.WarnContext(ctx, "No active edge tunnel or recent poll check-in for environment", "environmentId", envID)
		return nil, true
	}
	tunnel, ok := m.registry.WaitForActive(ctx, envID, edge.DefaultTunnelAcquireTimeout()).Get()
	if !ok {
		slog.WarnContext(ctx, "No active edge tunnel for environment", "environmentId", envID)
		return nil, true
	}
	slog.InfoContext(ctx, "Recovered edge tunnel during request", "environmentId", envID)
	return tunnel, false
}

// proxyPermissionStatus returns http.StatusOK when the caller may perform the proxied request, mirroring local
// RequirePermission checks. Sudo bypasses it; routes without a permission mapping are denied by default.
func (m *EnvironmentMiddleware) proxyPermissionStatus(c *echo.Context, ps *authz.PermissionSet, envID, suffix string) int {
	if m.matcher == nil || (ps != nil && ps.Sudo) {
		return http.StatusOK
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
			return kit.Ternary(denied, http.StatusForbidden, http.StatusOK)
		}
	}

	perm, ok := m.matcher.Lookup(method, suffix, req.URL.Query()).Get()
	if !ok {
		slog.WarnContext(ctx, "Denying proxied request with no known permission mapping", "method", method, "path", suffix, "environmentId", envID)
		return http.StatusForbidden
	}
	if perm == "" {
		// Explicitly public route: allowed for any authenticated caller.
		return http.StatusOK
	}
	if !ps.Allows(perm, kit.Ternary(authz.IsEnvScoped(perm), envID, "")) {
		slog.DebugContext(ctx, "Denying proxied request: permission denied", "method", method, "path", suffix, "permission", perm, "environmentId", envID)
		return http.StatusForbidden
	}

	var required []string
	var err error
	if method == http.MethodPatch && len(segments) == 3 && segments[0] == "volumes" && segments[1] != "" && segments[2] == "workspace" {
		required, err = proxiedVolumeWorkspacePermissions(req)
	} else {
		required, err = proxiedGitOpsSyncPermissions(req, method, segments)
	}
	if err != nil {
		slog.DebugContext(ctx, "Rejecting proxied request with invalid body", "path", suffix, "environmentId", envID, "error", err)
		return kit.Ternary(errors.Is(err, errProxiedBodyTooLarge), http.StatusRequestEntityTooLarge, http.StatusBadRequest)
	}
	// Resource-sorted container lists collect per-container stats, so they also need containers:read.
	if method == http.MethodGet && len(segments) == 1 && segments[0] == "containers" && types.IsContainerResourceSort(req.URL.Query().Get("sort")) {
		required = append(required, authz.PermContainersRead)
	}
	for _, operationPermission := range required {
		if !ps.Allows(operationPermission, envID) {
			slog.DebugContext(ctx, "Denying proxied request: body-derived permission denied",
				"path", suffix, "permission", operationPermission, "environmentId", envID)
			return http.StatusForbidden
		}
	}
	return http.StatusOK
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

	prefix := &io.LimitedReader{R: originalBody, N: maxProxiedWorkspacePrefixBytes}
	reader := multipart.NewReader(io.TeeReader(prefix, captured), params["boundary"])
	for {
		part, nextErr := reader.NextPart()
		if nextErr != nil && prefix.N == 0 {
			return nil, fmt.Errorf("find workspace manifest part: %w", errProxiedBodyTooLarge)
		}
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
		if readErr != nil && prefix.N == 0 {
			return nil, fmt.Errorf("read workspace manifest: %w", errProxiedBodyTooLarge)
		}
		if readErr != nil {
			return nil, fmt.Errorf("read workspace manifest: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close workspace manifest part: %w", closeErr)
		}
		if len(manifestJSON) > maxProxiedWorkspaceManifestBytes {
			return nil, fmt.Errorf("workspace manifest: %w", errProxiedBodyTooLarge)
		}
		var manifest types.VolumeWorkspaceUpdateManifest
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
	isCreate := len(segments) == 2 && segments[0] == "gitops" && segments[1] == "syncs" && method == http.MethodPost
	isImport := len(segments) == 3 && segments[0] == "gitops" && segments[1] == "syncs" && segments[2] == "import" && method == http.MethodPost
	isUpdate := len(segments) == 3 && segments[0] == "gitops" && segments[1] == "syncs" && segments[2] != "" && method == http.MethodPatch
	if !isCreate && !isImport && !isUpdate || request.Body == nil {
		return nil, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(request.Body, maxProxiedGitOpsSyncBodyBytes+1))
	closeErr := request.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read gitops sync body: %w", err)
	}
	if len(body) > maxProxiedGitOpsSyncBodyBytes {
		return nil, fmt.Errorf("gitops sync body: %w", errProxiedBodyTooLarge)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))

	type syncBody struct {
		types.GitOpsPreDeployConfigRequest

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
	if isCreate && slices.ContainsFunc(items, func(item syncBody) bool { return item.Mode == types.GitOpsSyncModeBackup }) {
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

// IsManagerLocalPath reports whether an environment-scoped path suffix is served by the manager instead of being proxied.
func IsManagerLocalPath(method, suffix string) bool {
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
	case "GET /swarm/join/candidates", "GET /swarm/nodes", "POST /swarm/join/environments", "POST /swarm/nodes/agents/reconcile":
		return true
	}

	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 4 && parts[0] == "volumes" && parts[1] != "" && parts[2] == "backups" && parts[3] == "policy" {
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

func writeProxyError(c *echo.Context, status int, message string) error {
	return c.JSON(status, map[string]any{
		"success": false,
		"data":    map[string]any{"error": message},
	})
}
