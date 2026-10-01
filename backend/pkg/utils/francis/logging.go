package francis

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// NameActor changes only the log display; durable actor IDs remain unchanged.
func (r *Runtime) NameActor(actorID, name string) {
	r.actorNames.Store(actorID, name)
}

type actorLogHandlerInternal struct {
	slog.Handler

	names *sync.Map
	steps []actorLogStepInternal
}

type actorLogStepInternal struct {
	attrs []slog.Attr
	group string
}

func (h *actorLogHandlerInternal) Handle(ctx context.Context, record slog.Record) error {
	handler := h.Handler
	// Resolve at emission, including loggers created before restored jobs are loaded.
	for _, step := range h.steps {
		if step.group != "" {
			handler = handler.WithGroup(step.group)
			continue
		}
		attrs := make([]slog.Attr, len(step.attrs))
		for i, attr := range step.attrs {
			attrs[i] = h.nameAttrInternal(attr)
		}
		handler = handler.WithAttrs(attrs)
	}
	named := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		named.AddAttrs(h.nameAttrInternal(attr))
		return true
	})
	return handler.Handle(ctx, named)
}

func (h *actorLogHandlerInternal) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.steps = append(slices.Clone(h.steps), actorLogStepInternal{attrs: slices.Clone(attrs)})
	return &clone
}

func (h *actorLogHandlerInternal) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.steps = append(slices.Clone(h.steps), actorLogStepInternal{group: name})
	return &clone
}

func (h *actorLogHandlerInternal) nameAttrInternal(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		attrs := slices.Clone(attr.Value.Group())
		for i := range attrs {
			attrs[i] = h.nameAttrInternal(attrs[i])
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
