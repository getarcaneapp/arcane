package event

import (
	"strings"
	"sync"
	"time"
)

const (
	dockerExpectationTTL         = 60 * time.Second
	dockerSuppressionWindowGrace = 10 * time.Second
)

type dockerExpectationInternal struct {
	resourceType, resourceID, resourceName string
}

type dockerWindowKeyInternal struct {
	kind, name string
	resource   dockerExpectationInternal
}

type dockerWindowInternal struct {
	active  int
	expires time.Time
}

type dockerCorrelationInternal struct {
	mu                 sync.Mutex
	expectations       map[dockerExpectationInternal]time.Time
	windows            map[dockerWindowKeyInternal]dockerWindowInternal
	now                func() time.Time
	updatingContainers func() []string
}

func (c *dockerCorrelationInternal) timeInternal() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *dockerCorrelationInternal) pruneInternal(now time.Time) {
	for key, expiry := range c.expectations {
		if !now.Before(expiry) {
			delete(c.expectations, key)
		}
	}
	for key, window := range c.windows {
		if window.active == 0 && !now.Before(window.expires) {
			delete(c.windows, key)
		}
	}
}

// MarkDockerExpectation correlates subsequent daemon observations with a local Arcane action.
func (s *EventService) MarkDockerExpectation(resourceType, resourceID, resourceName string) {
	if s == nil {
		return
	}
	switch resourceType {
	case "container", "image", "volume", "network":
	default:
		return
	}
	if resourceName == "name" {
		resourceName = ""
	}
	if resourceID == "" && resourceName == "" {
		return
	}
	c := &s.dockerCorrelation
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.timeInternal()
	c.pruneInternal(now)
	if c.expectations == nil {
		c.expectations = make(map[dockerExpectationInternal]time.Time)
	}
	c.expectations[dockerExpectationInternal{resourceType, resourceID, resourceName}] = now.Add(dockerExpectationTTL)
}

// BeginComposeSuppressionWindow covers daemon events labeled with the Compose project.
// Unlabeled resources require an explicit resource expectation to avoid hiding unrelated activity.
func (s *EventService) BeginComposeSuppressionWindow(composeProject string) func() {
	if composeProject == "" {
		return func() {}
	}
	return s.beginDockerWindowsInternal([]dockerWindowKeyInternal{{kind: "compose", name: composeProject}})
}

// BeginDockerResourceSuppressionWindow covers a mutation for its full duration and cleanup grace.
func (s *EventService) BeginDockerResourceSuppressionWindow(resourceType, resourceID, resourceName string) func() {
	if resourceName == "name" {
		resourceName = ""
	}
	if resourceID == "" && resourceName == "" {
		return func() {}
	}
	return s.beginDockerWindowsInternal([]dockerWindowKeyInternal{{kind: "resource", resource: dockerExpectationInternal{resourceType, resourceID, resourceName}}})
}

func (s *EventService) beginDockerWindowsInternal(keys []dockerWindowKeyInternal) func() {
	if s == nil {
		return func() {}
	}
	c := &s.dockerCorrelation
	c.mu.Lock()
	c.pruneInternal(c.timeInternal())
	if c.windows == nil {
		c.windows = make(map[dockerWindowKeyInternal]dockerWindowInternal)
	}
	for _, key := range keys {
		window := c.windows[key]
		window.active++
		c.windows[key] = window
	}
	c.mu.Unlock()
	return sync.OnceFunc(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, key := range keys {
			window := c.windows[key]
			window.active--
			window.expires = c.timeInternal().Add(dockerSuppressionWindowGrace)
			c.windows[key] = window
		}
	})
}

// SetDockerUpdatingContainers supplies the updater's live identities without coupling domains.
func (s *EventService) SetDockerUpdatingContainers(source func() []string) {
	if s == nil {
		return
	}
	s.dockerCorrelation.mu.Lock()
	defer s.dockerCorrelation.mu.Unlock()
	s.dockerCorrelation.updatingContainers = source
}

// ShouldSuppressDaemonEvent reports whether a daemon observation matches a recent local action.
func (s *EventService) ShouldSuppressDaemonEvent(resourceType, actorID, actorName, composeProject string) bool {
	if s == nil {
		return false
	}
	c := &s.dockerCorrelation
	c.mu.Lock()
	updating := c.updatingContainers
	c.mu.Unlock()
	// The updater records stop after Docker returns, so active updates also need correlation.
	if resourceType == "container" && updating != nil {
		for _, id := range updating() {
			if id != "" && (id == actorID || dockerIDMatchesInternal(resourceType, id, actorID)) {
				return true
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneInternal(c.timeInternal())

	for exp := range c.expectations {
		if exp.matchesInternal(resourceType, actorID, actorName) {
			return true
		}
	}
	for key := range c.windows {
		switch key.kind {
		case "resource":
			if key.resource.matchesInternal(resourceType, actorID, actorName) {
				return true
			}
		case "compose":
			if composeProject == key.name {
				return true
			}
		}
	}

	return false
}

func (exp dockerExpectationInternal) matchesInternal(resourceType, actorID, actorName string) bool {
	if exp.resourceType != resourceType {
		return false
	}
	if exp.resourceID != "" && (exp.resourceID == actorID || exp.resourceID == actorName || dockerIDMatchesInternal(resourceType, exp.resourceID, actorID)) {
		return true
	}
	return exp.resourceName != "" && (exp.resourceName == actorName || exp.resourceName == actorID)
}

func dockerIDMatchesInternal(resourceType, expected, actual string) bool {
	if resourceType == "volume" {
		return false
	}
	if resourceType == "image" {
		expected = strings.TrimPrefix(expected, "sha256:")
		actual = strings.TrimPrefix(actual, "sha256:")
	}
	if len(expected) < 12 || len(expected) > 64 || len(actual) != 64 {
		return false
	}
	for _, id := range []string{expected, actual} {
		for _, ch := range id {
			if ch < '0' || ch > '9' && ch < 'a' || ch > 'f' {
				return false
			}
		}
	}
	return strings.HasPrefix(actual, expected)
}

func (s *EventService) correlateCreatedEventInternal(req CreateEventRequest) {
	if req.ResourceType == nil || req.Metadata["source"] == "docker" {
		return
	}
	if req.EnvironmentID != nil && *req.EnvironmentID != "" && *req.EnvironmentID != "0" {
		return
	}
	// Failure and observational records do not identify a successful Docker mutation.
	//exhaustive:ignore
	switch req.Type {
	case EventTypeContainerStart, EventTypeContainerStop, EventTypeContainerRestart,
		EventTypeContainerDelete, EventTypeContainerCreate, EventTypeContainerUpdate,
		EventTypeContainerDeploy, EventTypeContainerKill, EventTypeContainerPause, EventTypeContainerUnpause,
		EventTypeImagePull, EventTypeImageLoad, EventTypeImageTag, EventTypeImageCommit, EventTypeImageDelete,
		EventTypeVolumeCreate, EventTypeVolumeDelete, EventTypeVolumeRename,
		EventTypeNetworkCreate, EventTypeNetworkDelete:
	default:
		return
	}
	var id, name string
	if req.ResourceID != nil {
		id = *req.ResourceID
	}
	if req.ResourceName != nil {
		name = *req.ResourceName
	}
	s.MarkDockerExpectation(*req.ResourceType, id, name)
}
