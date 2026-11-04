package enrollment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.uber.org/fx"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/types/v3"
)

type (
	EnrollmentService struct {
		environmentService *environment.EnvironmentService
		swarmService       *swarm.SwarmService
		eventService       *event.EventService
		mtls               *agent.MTLS
	}

	EnrollmentServiceParams struct {
		fx.In

		EnvironmentService *environment.EnvironmentService
		SwarmService       *swarm.SwarmService
		EventService       *event.EventService
		MTLS               *agent.MTLS
	}

	EnrollParams struct {
		EnrollmentToken string
		AgentToken      string
		Request         types.AgentEnrollRequest
	}

	EnrollMTLSParams struct {
		AgentToken string
		RemoteAddr string
	}
)

const lostEnrollmentReplyWindow = 10 * time.Minute

var (
	errMTLSEnrollUnavailable = common.Classify(common.ErrNotFound, agent.ErrMTLSEnrollUnavailable)
	errAgentTokenRequired    = common.Classify(common.ErrUnauthorized, errors.New("agent token required"))
	errInvalidAgentToken     = common.Classify(common.ErrUnauthorized, errors.New("invalid agent token"))
	errMTLSEnrollCooldown    = common.NewAPIError(agent.ErrMTLSEnrollCooldown.Error(), common.APIErrorCodeTooManyRequests, http.StatusTooManyRequests)
	errMTLSEnrollFailed      = errors.New("failed to generate agent mTLS assets")
)

func NewEnrollmentService(p EnrollmentServiceParams) *EnrollmentService {
	return &EnrollmentService{
		environmentService: p.EnvironmentService,
		swarmService:       p.SwarmService,
		eventService:       p.EventService,
		mtls:               p.MTLS,
	}
}

func (s *EnrollmentService) Enroll(ctx context.Context, p EnrollParams) (enrolled *types.AgentEnrollResponse, err error) {
	token, err := s.environmentService.ResolveEnrollmentToken(ctx, p.EnrollmentToken)
	if err != nil {
		return nil, err
	}

	current, err := s.environmentService.ResolveEnvironmentByAccessToken(ctx, p.AgentToken)
	if err != nil && !errors.Is(err, environment.ErrEnvironmentAccessTokenRequired) && !errors.Is(err, environment.ErrInvalidEnvironmentAccessToken) {
		return nil, err
	}
	if kit.FromPtr(current).Enabled && (kit.FromPtr(token.EnvironmentID) == current.ID ||
		token.EnvironmentID == nil && slices.Contains([]string{"", p.Request.SwarmNodeID}, strings.TrimSpace(kit.FromPtr(current.SwarmNodeID)))) {
		return &types.AgentEnrollResponse{
			EnvironmentID: current.ID,
			Token:         kit.FromPtr(current.AccessToken),
		}, nil
	}

	if token.EnvironmentID != nil && token.MaxUses != nil && token.UseCount >= *token.MaxUses &&
		time.Since(kit.FromPtr(token.LastUsedAt)) < lostEnrollmentReplyWindow {
		bound, getErr := s.environmentService.GetEnvironmentByID(ctx, *token.EnvironmentID)
		if getErr != nil {
			return nil, getErr
		}
		if bound.Status == string(environment.EnvironmentStatusPending) && kit.FromPtr(bound.AccessToken) != "" {
			return &types.AgentEnrollResponse{
				EnvironmentID: bound.ID,
				Token:         kit.FromPtr(bound.AccessToken),
			}, nil
		}
	}

	if consumeErr := s.environmentService.ConsumeEnrollmentToken(ctx, token.ID); consumeErr != nil {
		return nil, consumeErr
	}
	defer func() {
		if err == nil {
			return
		}
		if refundErr := s.environmentService.RefundEnrollmentToken(context.WithoutCancel(ctx), token.ID); refundErr != nil {
			slog.WarnContext(ctx, "Failed to give back the enrollment token use of a failed enrollment", "tokenId", token.ID, "error", refundErr)
		}
	}()

	if token.EnvironmentID != nil {
		env, getErr := s.environmentService.GetEnvironmentByID(ctx, *token.EnvironmentID)
		if getErr != nil {
			return nil, getErr
		}
		apiKey, rotateErr := s.environmentService.RegenerateEnvironmentApiKey(ctx, env, types.SystemUser.ID, types.SystemUser.Username)
		if rotateErr != nil {
			return nil, rotateErr
		}
		return &types.AgentEnrollResponse{
			EnvironmentID: env.ID,
			Token:         apiKey,
		}, nil
	}

	if p.Request.SwarmNodeID != "" && p.Request.SwarmClusterID != "" {
		parent, findErr := s.swarmService.FindClusterManagerEnvironment(ctx, p.Request.SwarmClusterID)
		if findErr != nil {
			return nil, findErr
		}
		if parentEnvironmentID, ok := parent.Get(); ok {
			return s.swarmService.EnrollNodeAgent(ctx, parentEnvironmentID, p.Request.SwarmNodeID)
		}
	}
	return s.environmentService.EnrollAgentEnvironment(ctx, p.Request.Hostname)
}

func (s *EnrollmentService) EnrollMTLS(ctx context.Context, p EnrollMTLSParams) ([]agent.GeneratedMTLSFile, error) {
	if !s.mtls.UsesGeneratedCA() {
		return nil, errMTLSEnrollUnavailable
	}
	if p.AgentToken == "" {
		return nil, errAgentTokenRequired
	}
	envID, err := s.environmentService.ResolveAgentEnvironmentByToken(ctx, p.AgentToken)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token for mTLS enrollment", "error", err)
		return nil, errInvalidAgentToken
	}

	env, getErr := s.environmentService.GetEnvironmentByID(ctx, envID)
	if getErr != nil {
		slog.WarnContext(ctx, "Failed to resolve environment name for agent mTLS enrollment", "environmentId", envID, "error", getErr)
	}
	envName := kit.FromPtr(env).Name

	enrollment, err := s.mtls.IssueEnrollment(ctx, agent.IssueClientOptions{
		EnvironmentID:   envID,
		EnvironmentName: envName,
	})
	switch {
	case errors.Is(err, agent.ErrMTLSEnrollCooldown):
		slog.WarnContext(ctx, "Rejected repeated agent mTLS enrollment during cooldown", "environmentId", envID, "remoteAddr", p.RemoteAddr, "error", err)
		return nil, errMTLSEnrollCooldown
	case errors.Is(err, agent.ErrMTLSEnrollUnavailable):
		return nil, errMTLSEnrollUnavailable
	case err != nil:
		slog.ErrorContext(ctx, "Failed to enroll agent mTLS assets", "environmentId", envID, "error", err)
		return nil, errMTLSEnrollFailed
	case enrollment.Cached:
		slog.InfoContext(ctx, "Served cached agent mTLS enrollment during cooldown", "environmentId", envID, "remoteAddr", p.RemoteAddr)
		return enrollment.Assets.Files, nil
	case enrollment.Reenrolled:
		slog.WarnContext(ctx, "Agent mTLS certificate assets re-enrolled", "environmentId", envID, "remoteAddr", p.RemoteAddr, "certIssued", enrollment.Assets.CertIssued)
	default:
		slog.InfoContext(ctx, "Agent mTLS certificate assets enrolled", "environmentId", envID, "remoteAddr", p.RemoteAddr, "certIssued", enrollment.Assets.CertIssued)
	}

	events := []event.CreateEventRequest{{
		Type:          event.EventTypeEnvironmentMTLSEnroll,
		Severity:      kit.Ternary(enrollment.Reenrolled, event.EventSeverityWarning, event.EventSeverityInfo),
		Title:         "Agent mTLS enrollment",
		Description:   "Agent completed mTLS enrollment from " + p.RemoteAddr,
		ResourceType:  new("environment"),
		ResourceID:    &envID,
		ResourceName:  &envName,
		EnvironmentID: &envID,
		Metadata:      database.JSON{"remoteAddr": p.RemoteAddr, "reenrollment": enrollment.Reenrolled},
	}}
	if enrollment.Assets.CAGenerated {
		events = append(events, event.CreateEventRequest{
			Type:        event.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    event.EventSeverityInfo,
			Title:       "Agent mTLS CA generated",
			Description: "Arcane generated a new agent mTLS certificate authority",
			Metadata:    database.JSON{"remoteAddr": p.RemoteAddr, "kind": "ca"},
		})
	}
	if enrollment.Assets.CertIssued {
		events = append(events, event.CreateEventRequest{
			Type:          event.EventTypeEnvironmentMTLSCertIssued,
			Severity:      kit.Ternary(enrollment.Reenrolled, event.EventSeverityWarning, event.EventSeverityInfo),
			Title:         "Agent mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an agent mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    &envID,
			ResourceName:  &envName,
			EnvironmentID: &envID,
			Metadata:      database.JSON{"remoteAddr": p.RemoteAddr, "kind": "client", "reenrollment": enrollment.Reenrolled},
		})
	}
	for _, req := range events {
		_, _ = s.eventService.CreateEvent(context.WithoutCancel(ctx), req)
	}
	return enrollment.Assets.Files, nil
}
