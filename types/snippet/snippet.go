package snippet

import "time"

// ParameterType enumerates the supported snippet parameter kinds.
type ParameterType string

const (
	ParameterTypeString  ParameterType = "string"
	ParameterTypeNumber  ParameterType = "number"
	ParameterTypeBoolean ParameterType = "boolean"
	ParameterTypeSelect  ParameterType = "select"
)

// Target values for Snippet.Target / CreateSnippetRequest.Target. Plain
// strings (not a distinct Go type) so they round-trip through the
// copier-based model<->API mapping the same way TriggerSource/Status already
// do.
const (
	TargetHost      = "host"
	TargetContainer = "container"
)

// ParameterDef declares one snippet parameter. At run time the resolved value
// is exposed to the script as a process environment variable named Name —
// never spliced into the script text — so there is no quoting or injection
// surface beyond the script body itself.
type ParameterDef struct {
	// Name of the parameter; becomes the env var name. Must match
	// ^[A-Za-z_][A-Za-z0-9_]{0,63}$ and must not be a deny-listed name.
	//
	// Required: true
	Name string `json:"name"`

	// Type of the parameter: "string", "number", "boolean", or "select".
	//
	// Required: true
	Type ParameterType `json:"type"`

	// Label is an optional human-readable label shown in the run form.
	//
	// Required: false
	Label string `json:"label,omitempty"`

	// Required indicates the parameter must resolve to a non-empty value.
	//
	// Required: false
	Required bool `json:"required,omitempty"`

	// Default is the value used when the parameter is not supplied at run
	// time. Stored and resolved as a string regardless of Type.
	//
	// Required: false
	Default string `json:"default,omitempty"`

	// Options is the allowed value set for a "select" parameter. Must be
	// non-empty iff Type is "select".
	//
	// Required: false
	Options []string `json:"options,omitempty"`

	// Pattern is an optional regular expression a "string" parameter's value
	// must match. Only valid when Type is "string".
	//
	// Required: false
	Pattern string `json:"pattern,omitempty"`
}

// Snippet represents a user-authored host-shell command with typed
// parameters, runnable manually or on a cron schedule.
type Snippet struct {
	// ID of the snippet.
	//
	// Required: true
	ID string `json:"id"`

	// CreatedAt is the date and time at which the snippet was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the snippet was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// EnvironmentID is the ID of the environment this snippet belongs to.
	//
	// Required: true
	EnvironmentID string `json:"environmentId"`

	// Name of the snippet. Unique within its environment.
	//
	// Required: true
	Name string `json:"name"`

	// Description of what the snippet does.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Script is the shell script body, executed via the host shell or, when
	// Target is "container", inside ContainerID. Declared parameters are
	// referenced as quoted env vars, e.g. "$NAME".
	//
	// Required: true
	Script string `json:"script"`

	// Target is where the script executes: "host" (default) or "container".
	//
	// Required: true
	Target string `json:"target"`

	// ContainerID is the target container's ID, required when Target is
	// "container" and otherwise empty.
	//
	// Required: false
	ContainerID *string `json:"containerId,omitempty"`

	// Parameters declares the typed inputs the script accepts. Order is
	// preserved and used as the run-form field order.
	//
	// Required: false
	Parameters []ParameterDef `json:"parameters,omitempty"`

	// WorkingDir is the directory the script runs in on the host.
	//
	// Required: false
	WorkingDir *string `json:"workingDir,omitempty"`

	// TimeoutSec bounds script execution. Clamped to a server-side maximum.
	//
	// Required: true
	TimeoutSec int `json:"timeoutSec"`

	// Schedule is a 6-field cron expression (with seconds). Empty when the
	// snippet has no schedule configured.
	//
	// Required: false
	Schedule *string `json:"schedule,omitempty"`

	// ScheduleEnabled indicates whether the cron schedule is currently active.
	//
	// Required: true
	ScheduleEnabled bool `json:"scheduleEnabled"`

	// ScheduleParameters supplies parameter values used for scheduled runs,
	// resolved through the same rules as a manual run.
	//
	// Required: false
	ScheduleParameters map[string]string `json:"scheduleParameters,omitempty"`

	// LastRunAt is the start time of the most recent run.
	//
	// Required: false
	LastRunAt *time.Time `json:"lastRunAt,omitempty"`

	// LastRunStatus is the status of the most recent run.
	//
	// Required: false
	LastRunStatus *string `json:"lastRunStatus,omitempty"`

	// CreatedByUserID is the ID of the user who created the snippet.
	//
	// Required: false
	CreatedByUserID *string `json:"createdByUserId,omitempty"`
}

// SnippetRun represents one execution of a snippet, manual or scheduled.
type SnippetRun struct {
	// ID of the run.
	//
	// Required: true
	ID string `json:"id"`

	// CreatedAt is the date and time at which the run row was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the run row was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// SnippetID is the ID of the snippet this run belongs to.
	//
	// Required: true
	SnippetID string `json:"snippetId"`

	// EnvironmentID is the ID of the environment the snippet ran on.
	//
	// Required: true
	EnvironmentID string `json:"environmentId"`

	// TriggerSource indicates what started the run: "manual" or "schedule".
	//
	// Required: true
	TriggerSource string `json:"triggerSource"`

	// Status is the run outcome: "success", "failed", or "timeout".
	//
	// Required: true
	Status string `json:"status"`

	// ExitCode is the script's process exit code, when the script ran.
	//
	// Required: false
	ExitCode *int64 `json:"exitCode,omitempty"`

	// Parameters holds the resolved parameter values used for this run.
	//
	// Required: false
	Parameters map[string]string `json:"parameters,omitempty"`

	// Output is the truncated combined stdout+stderr of the run.
	//
	// Required: false
	Output *string `json:"output,omitempty"`

	// Error contains a failure message when the run could not complete.
	//
	// Required: false
	Error *string `json:"error,omitempty"`

	// StartedAt is when the run began.
	//
	// Required: true
	StartedAt time.Time `json:"startedAt"`

	// DurationMs is how long the run took, in milliseconds.
	//
	// Required: true
	DurationMs int64 `json:"durationMs"`

	// StartedByUserID is the ID of the user who triggered the run, when
	// triggered by a user.
	//
	// Required: false
	StartedByUserID *string `json:"startedByUserId,omitempty"`

	// StartedByUsername is the username of the user who triggered the run.
	//
	// Required: false
	StartedByUsername *string `json:"startedByUsername,omitempty"`
}

// CreateSnippetRequest represents the request to create a snippet.
type CreateSnippetRequest struct {
	// Name of the snippet. Unique within its environment.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// Description of what the snippet does.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Script is the shell script body.
	//
	// Required: true
	Script string `json:"script" binding:"required"`

	// Target is where the script executes: "host" (default) or "container".
	//
	// Required: false
	Target *string `json:"target,omitempty"`

	// ContainerID is the target container's ID, required when Target is
	// "container".
	//
	// Required: false
	ContainerID *string `json:"containerId,omitempty"`

	// Parameters declares the typed inputs the script accepts.
	//
	// Required: false
	Parameters []ParameterDef `json:"parameters,omitempty"`

	// WorkingDir is the directory the script runs in on the host.
	//
	// Required: false
	WorkingDir *string `json:"workingDir,omitempty"`

	// TimeoutSec bounds script execution. Defaults to 60 when omitted.
	//
	// Required: false
	TimeoutSec *int `json:"timeoutSec,omitempty"`

	// Schedule is a 6-field cron expression (with seconds).
	//
	// Required: false
	Schedule *string `json:"schedule,omitempty"`

	// ScheduleEnabled indicates whether the cron schedule should be active.
	//
	// Required: false
	ScheduleEnabled *bool `json:"scheduleEnabled,omitempty"`

	// ScheduleParameters supplies parameter values used for scheduled runs.
	//
	// Required: false
	ScheduleParameters map[string]string `json:"scheduleParameters,omitempty"`
}

// UpdateSnippetRequest represents the request to update a snippet.
type UpdateSnippetRequest struct {
	// Name of the snippet.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// Description of what the snippet does. Set to an empty string to clear.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Script is the shell script body.
	//
	// Required: false
	Script *string `json:"script,omitempty"`

	// Target is where the script executes: "host" or "container".
	//
	// Required: false
	Target *string `json:"target,omitempty"`

	// ContainerID is the target container's ID, required when Target is
	// "container". Set to an empty string to clear.
	//
	// Required: false
	ContainerID *string `json:"containerId,omitempty"`

	// Parameters declares the typed inputs the script accepts. Replaces the
	// full parameter set when present.
	//
	// Required: false
	Parameters []ParameterDef `json:"parameters,omitempty"`

	// WorkingDir is the directory the script runs in on the host.
	//
	// Required: false
	WorkingDir *string `json:"workingDir,omitempty"`

	// TimeoutSec bounds script execution.
	//
	// Required: false
	TimeoutSec *int `json:"timeoutSec,omitempty"`

	// Schedule is a 6-field cron expression (with seconds). Set to an empty
	// string to clear.
	//
	// Required: false
	Schedule *string `json:"schedule,omitempty"`

	// ScheduleEnabled indicates whether the cron schedule should be active.
	//
	// Required: false
	ScheduleEnabled *bool `json:"scheduleEnabled,omitempty"`

	// ScheduleParameters supplies parameter values used for scheduled runs.
	// Replaces the full map when present.
	//
	// Required: false
	ScheduleParameters map[string]string `json:"scheduleParameters,omitempty"`
}

// RunSnippetRequest represents the request to manually run a snippet.
type RunSnippetRequest struct {
	// Parameters supplies parameter values for this run. Missing declared
	// parameters fall back to their default, then to empty.
	//
	// Required: false
	Parameters map[string]string `json:"parameters,omitempty"`
}
