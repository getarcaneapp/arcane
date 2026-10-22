package audit

import (
	"cmp"
	"container/list"
	"context"
	"fmt"
	"maps"
	"strings"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

const (
	denialDedupeWindow   = time.Minute
	denialDedupeCapacity = 4096
	denialQueueCapacity  = 1024
	agentActorID         = "agent"
	environmentActorID   = "environment:"
)

var actionLabels = map[string]string{
	types.AuditActionRoleCreate:         "Role created",
	types.AuditActionRoleUpdate:         "Role updated",
	types.AuditActionRoleDelete:         "Role deleted",
	types.AuditActionBindingCreate:      "Role binding created",
	types.AuditActionBindingDelete:      "Role binding deleted",
	types.AuditActionGroupCreate:        "Group created",
	types.AuditActionGroupUpdate:        "Group updated",
	types.AuditActionGroupDelete:        "Group deleted",
	types.AuditActionGroupMemberAdd:     "Group member added",
	types.AuditActionGroupMemberRemove:  "Group member removed",
	types.AuditActionGroupOidcAdminKept: "OIDC sync kept admin membership",
	types.AuditActionOidcMappingCreate:  "OIDC group mapping created",
	types.AuditActionOidcMappingUpdate:  "OIDC group mapping updated",
	types.AuditActionOidcMappingDelete:  "OIDC group mapping deleted",
	types.AuditActionApiKeyCreate:       "API key created",
	types.AuditActionApiKeyUpdate:       "API key updated",
	types.AuditActionApiKeyDelete:       "API key deleted",
	types.AuditActionUserCreate:         "User created",
	types.AuditActionUserUpdate:         "User updated",
	types.AuditActionUserDelete:         "User deleted",
	types.AuditActionUserDisable:        "User disabled",
	types.AuditActionUserEnable:         "User enabled",
	types.AuditActionUserPasswordReset:  "User password reset",
	types.AuditActionFederatedCreate:    "Federated credential created",
	types.AuditActionFederatedUpdate:    "Federated credential updated",
	types.AuditActionFederatedDelete:    "Federated credential deleted",
	types.AuditActionSettingsUpdate:     "Setting updated",
	types.AuditActionAuthzDenied:        "Permission denied",
}

// newEvent maps an audit entry and its request context to an audit event row.
func newEvent(ctx context.Context, entry types.AuditEntry, denial types.AuthzDenial) event.Event {
	actor, _ := userctx.CurrentUserFromContext(ctx)
	apiKeyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	actorID := kit.FromPtr(actor).ID
	username := kit.FromPtr(actor).Username
	outcome := cmp.Or(entry.Outcome, types.AuditOutcomeSuccess)
	title := auditTitle(entry, outcome)
	now := time.Now()
	return event.Event{
		BaseModel:     database.BaseModel{ID: uuid.New().String(), CreatedAt: now},
		Type:          event.EventType(entry.Action),
		Severity:      outcomeSeverity(outcome),
		Title:         title,
		Description:   auditDescription(entry, denial, title, cmp.Or(username, actorID, "System")),
		ResourceType:  optional(entry.TargetType),
		ResourceID:    optional(entry.TargetID),
		ResourceName:  optional(entry.TargetName),
		UserID:        new(cmp.Or(actorID, username, "system")),
		Username:      new(cmp.Or(username, actorID, "System")),
		EnvironmentID: entry.EnvironmentID,
		Metadata:      auditMetadata(ctx, entry, denial, cmp.Or(entry.ActorType, actorType(actorID, apiKeyID)), apiKeyID, outcome),
		Audit:         true,
		Timestamp:     now,
	}
}

func auditMetadata(ctx context.Context, entry types.AuditEntry, denial types.AuthzDenial, actor, apiKeyID, outcome string) database.JSON {
	metadata := database.JSON{}
	maps.Copy(metadata, entry.Metadata)
	metadata["actorType"] = actor
	metadata["outcome"] = outcome
	setIfPresent(metadata, "apiKeyId", apiKeyID)
	setIfPresent(metadata, "permission", entry.Permission)
	request := map[string]any{}
	setIfPresent(request, "method", denial.Method)
	setIfPresent(request, "path", denial.Path)
	setIfPresent(request, "remoteAddr", cmp.Or(denial.RemoteAddr, middleware.GetRemoteAddrFromContext(ctx)))
	setIfPresent(request, "userAgent", denial.UserAgent)
	if len(request) > 0 {
		metadata["request"] = request
	}
	if entry.Before != nil {
		metadata["before"] = entry.Before
	}
	if entry.After != nil {
		metadata["after"] = entry.After
	}
	return metadata
}

func auditTitle(entry types.AuditEntry, outcome string) string {
	title := cmp.Or(actionLabels[entry.Action], "Audit event "+entry.Action)
	if outcome == types.AuditOutcomeError {
		title += " (failed)"
	}
	if subject := cmp.Or(entry.TargetName, entry.TargetID, entry.Permission); subject != "" {
		title += ": " + subject
	}
	return title
}

func auditDescription(entry types.AuditEntry, denial types.AuthzDenial, title, actorName string) string {
	if entry.Action != types.AuditActionAuthzDenied {
		return fmt.Sprintf("%s by %s", title, actorName)
	}
	description := fmt.Sprintf("%s was denied permission '%s'", actorName, entry.Permission)
	if denial.Method != "" || denial.Path != "" {
		description += " on " + strings.TrimSpace(denial.Method+" "+denial.Path)
	}
	return description
}

func outcomeSeverity(outcome string) event.EventSeverity {
	switch outcome {
	case types.AuditOutcomeDenied:
		return event.EventSeverityWarning
	case types.AuditOutcomeError:
		return event.EventSeverityError
	default:
		return event.EventSeverityInfo
	}
}

func optional(value string) *string {
	return kit.Ternary(value == "", nil, new(value))
}

func setIfPresent(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func actorType(actorID, apiKeyID string) string {
	switch {
	case actorID == "":
		return types.AuditActorSystem
	case actorID == agentActorID, strings.HasPrefix(actorID, environmentActorID):
		return types.AuditActorAgent
	case apiKeyID != "":
		return types.AuditActorApiKey
	default:
		return types.AuditActorUser
	}
}

// denialKey identifies repeated denials for the same caller, permission, and request.
func denialKey(ctx context.Context, denial types.AuthzDenial) string {
	actor, _ := userctx.CurrentUserFromContext(ctx)
	apiKeyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	actorID := kit.FromPtr(actor).ID
	return strings.Join([]string{
		actorID,
		apiKeyID,
		kit.Ternary(actorID == "", cmp.Or(denial.RemoteAddr, middleware.GetRemoteAddrFromContext(ctx)), ""),
		denial.Permission,
		denial.EnvironmentID,
		denial.Method,
		denial.Path,
	}, "\x00")
}

type denialCache struct {
	order   *list.List
	entries map[string]*list.Element
}

type denialCacheEntry struct {
	key  string
	seen time.Time
}

func newDenialCache() *denialCache {
	return &denialCache{
		order:   list.New(),
		entries: map[string]*list.Element{},
	}
}

func (c *denialCache) seenRecently(key string, now time.Time) bool {
	if element, ok := c.entries[key]; ok {
		entry := element.Value.(*denialCacheEntry)
		c.order.MoveToFront(element)
		if now.Sub(entry.seen) < denialDedupeWindow {
			return true
		}
		entry.seen = now
		return false
	}
	c.entries[key] = c.order.PushFront(&denialCacheEntry{
		key:  key,
		seen: now,
	})
	if c.order.Len() > denialDedupeCapacity {
		oldest := c.order.Remove(c.order.Back()).(*denialCacheEntry)
		delete(c.entries, oldest.key)
	}
	return false
}
