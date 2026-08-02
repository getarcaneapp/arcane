package docker

import "github.com/moby/moby/api/types/events"

// ImageStateResyncAction identifies Arcane's synthetic image event emitted when Docker reconnects.
const ImageStateResyncAction events.Action = "arcane-resync"
