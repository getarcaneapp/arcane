package audit

import (
	"cmp"
	"container/list"
	"context"
	"encoding/json/v2"
	"strings"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

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

var csvHeader = []string{
	"id",
	"createdAt",
	"actorType",
	"actorUserId",
	"actorUsername",
	"apiKeyId",
	"action",
	"outcome",
	"targetType",
	"targetId",
	"targetName",
	"environmentId",
	"permission",
	"requestMethod",
	"requestPath",
	"remoteAddr",
	"userAgent",
	"before",
	"after",
	"metadata",
}

func newLog(ctx context.Context, entry types.AuditEntry, denial types.AuthzDenial) AuditLog {
	actor, _ := userctx.CurrentUserFromContext(ctx)
	apiKeyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	actorID := kit.FromPtr(actor).ID
	return AuditLog{
		ID:            uuid.New().String(),
		CreatedAt:     time.Now(),
		ActorType:     cmp.Or(entry.ActorType, actorType(actorID, apiKeyID)),
		ActorUserID:   kit.Ternary(actorID == "", nil, new(actorID)),
		ActorUsername: kit.FromPtr(actor).Username,
		ApiKeyID:      kit.Ternary(apiKeyID == "", nil, new(apiKeyID)),
		Action:        entry.Action,
		Outcome:       cmp.Or(entry.Outcome, types.AuditOutcomeSuccess),
		TargetType:    entry.TargetType,
		TargetID:      entry.TargetID,
		TargetName:    entry.TargetName,
		EnvironmentID: entry.EnvironmentID,
		Permission:    entry.Permission,
		RequestMethod: denial.Method,
		RequestPath:   denial.Path,
		RemoteAddr:    cmp.Or(denial.RemoteAddr, middleware.GetRemoteAddrFromContext(ctx)),
		UserAgent:     denial.UserAgent,
		Before:        entry.Before,
		After:         entry.After,
		Metadata:      entry.Metadata,
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

func applyFilter(query *gorm.DB, filter types.AuditFilter) *gorm.DB {
	for column, value := range map[string]string{
		"actor_user_id":  filter.ActorUserID,
		"action":         filter.Action,
		"outcome":        filter.Outcome,
		"target_type":    filter.TargetType,
		"environment_id": filter.EnvironmentID,
	} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if filter.From != nil {
		query = query.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("created_at < ?", *filter.To)
	}
	return query
}

func toDTO(row AuditLog) types.AuditLog {
	return types.AuditLog{
		ID:            row.ID,
		CreatedAt:     row.CreatedAt,
		ActorType:     row.ActorType,
		ActorUserID:   row.ActorUserID,
		ActorUsername: row.ActorUsername,
		ApiKeyID:      row.ApiKeyID,
		Action:        row.Action,
		Outcome:       row.Outcome,
		TargetType:    row.TargetType,
		TargetID:      row.TargetID,
		TargetName:    row.TargetName,
		EnvironmentID: row.EnvironmentID,
		Permission:    row.Permission,
		RequestMethod: row.RequestMethod,
		RequestPath:   row.RequestPath,
		RemoteAddr:    row.RemoteAddr,
		UserAgent:     row.UserAgent,
		Before:        row.Before,
		After:         row.After,
		Metadata:      row.Metadata,
	}
}

func csvRecord(row AuditLog) ([]string, error) {
	encoded := make([]string, 3)
	for i, value := range []any{row.Before, row.After, row.Metadata} {
		if value == nil {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		encoded[i] = string(raw)
	}
	record := []string{
		row.ID,
		row.CreatedAt.UTC().Format(time.RFC3339Nano),
		row.ActorType,
		kit.FromPtr(row.ActorUserID),
		row.ActorUsername,
		kit.FromPtr(row.ApiKeyID),
		row.Action,
		row.Outcome,
		row.TargetType,
		row.TargetID,
		row.TargetName,
		kit.FromPtr(row.EnvironmentID),
		row.Permission,
		row.RequestMethod,
		row.RequestPath,
		row.RemoteAddr,
		row.UserAgent,
		encoded[0],
		encoded[1],
		encoded[2],
	}
	for i, value := range record {
		record[i] = kit.Ternary(value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])), "'"+value, value)
	}
	return record, nil
}

func denialKey(row AuditLog) string {
	return strings.Join([]string{
		kit.FromPtr(row.ActorUserID),
		kit.FromPtr(row.ApiKeyID),
		kit.Ternary(row.ActorUserID == nil, row.RemoteAddr, ""),
		row.Permission,
		kit.FromPtr(row.EnvironmentID),
		row.RequestMethod,
		row.RequestPath,
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
