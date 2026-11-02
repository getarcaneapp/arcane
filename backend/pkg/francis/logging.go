package francis

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

type (
	actorLogHandler struct {
		slog.Handler

		names *sync.Map
		steps []actorLogStep
	}

	actorLogStep struct {
		group string
		attrs []slog.Attr
	}
)

// NameActor changes only the log display; durable actor IDs remain unchanged.
func (r *Runtime) NameActor(actorID, name string) {
	r.actorNames.Store(actorID, name)
}

// ActorName returns the display name set by NameActor, or an empty string.
func (r *Runtime) ActorName(actorID string) string {
	name, _ := r.actorNames.Load(actorID)
	displayName, _ := name.(string)
	return displayName
}

func (h *actorLogHandler) Handle(ctx context.Context, record slog.Record) error {
	handler := h.Handler
	// Resolve at emission, including loggers created before restored jobs are loaded.
	for _, step := range h.steps {
		if step.group != "" {
			handler = handler.WithGroup(step.group)
			continue
		}
		attrs := make([]slog.Attr, len(step.attrs))
		for i, attr := range step.attrs {
			attrs[i] = h.nameAttr(attr)
		}
		handler = handler.WithAttrs(attrs)
	}
	named := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		named.AddAttrs(h.nameAttr(attr))
		return true
	})
	return handler.Handle(ctx, named)
}

func (h *actorLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.steps = append(slices.Clone(h.steps), actorLogStep{attrs: slices.Clone(attrs)})
	return &clone
}

func (h *actorLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.steps = append(slices.Clone(h.steps), actorLogStep{group: name})
	return &clone
}

func (h *actorLogHandler) nameAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		attrs := slices.Clone(attr.Value.Group())
		for i := range attrs {
			attrs[i] = h.nameAttr(attrs[i])
		}
		attr.Value = slog.GroupValue(attrs...)
		return attr
	}
	if (attr.Key != "id" && attr.Key != "actorRef") || attr.Value.Kind() != slog.KindString {
		return attr
	}
	actorType, rest, ok := strings.Cut(attr.Value.String(), "/")
	if !ok {
		return attr
	}
	actorID, suffix, hasSuffix := strings.Cut(rest, "/")
	name, found := h.names.Load(actorID)
	if !found {
		return attr
	}
	displayName, ok := name.(string)
	if !ok {
		return attr
	}
	value := actorType + "/" + displayName
	if hasSuffix {
		value += "/" + suffix
	}
	attr.Value = slog.StringValue(value)
	return attr
}
