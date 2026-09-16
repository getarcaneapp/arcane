package version

import (
	"context"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/version"
	"go.getarcane.app/streams/agg"
)

const versionStreamPollInterval = 5 * time.Minute

// RunStreamProducer shares one version poll loop across every connected client and emits a snapshot whenever it changes.
func (s *VersionService) RunStreamProducer(ctx context.Context, events chan<- version.StreamEvent) {
	s.streamHub.Subscribe(ctx, "local",
		func(runCtx context.Context, publish func(version.StreamEvent)) {
			var last *version.Info
			poll := func() {
				info := s.GetAppVersionInfo(runCtx)
				if runCtx.Err() != nil {
					return
				}
				if last != nil {
					if last.UpdateAvailable == info.UpdateAvailable &&
						last.NewestVersion == info.NewestVersion &&
						last.NewestDigest == info.NewestDigest &&
						last.CurrentTag == info.CurrentTag &&
						last.CurrentDigest == info.CurrentDigest &&
						last.ReleaseURL == info.ReleaseURL &&
						last.ReleaseNotes == info.ReleaseNotes &&
						last.ReleasedAt == info.ReleasedAt {
						return
					}
				}
				last = info
				publish(version.StreamEvent{Type: "snapshot", Info: info, Timestamp: time.Now()})
			}

			poll()

			ticker := time.NewTicker(versionStreamPollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					poll()
				}
			}
		},
		func(event version.StreamEvent) bool {
			return agg.Send(ctx, events, event)
		})
}
