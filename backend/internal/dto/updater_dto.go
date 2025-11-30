package dto

type UpdaterRunRequest struct {
	DryRun bool `json:"dryRun"`
}

type UpdaterItem struct {
	ResourceID    string            `json:"resourceId"`
	ResourceType  string            `json:"resourceType"` // "image" | "container" | "project"
	ResourceName  string            `json:"resourceName,omitempty"`
	Status        string            `json:"status"` // "checked" | "updated" | "skipped" | "failed" | "up_to_date" | "update_available"
	Error         string            `json:"error,omitempty"`
	OldImages     map[string]string `json:"oldImages,omitempty"`
	NewImages     map[string]string `json:"newImages,omitempty"`
	UpdateApplied bool              `json:"updateApplied,omitempty"`
}

type UpdaterRunResult struct {
	Checked  int           `json:"checked"`
	Updated  int           `json:"updated"`
	Skipped  int           `json:"skipped"`
	Failed   int           `json:"failed"`
	Items    []UpdaterItem `json:"items"`
	Duration string        `json:"duration"`
}

type AutoUpdateCheckDto struct {
	Type        string   `json:"type,omitempty"`
	ResourceIds []string `json:"resourceIds,omitempty"`
	ForceUpdate bool     `json:"forceUpdate,omitempty"`
	DryRun      bool     `json:"dryRun,omitempty"`
}

type AutoUpdateResultDto struct {
	Success   bool                       `json:"success"`
	Checked   int                        `json:"checked"`
	Updated   int                        `json:"updated"`
	Skipped   int                        `json:"skipped"`
	Failed    int                        `json:"failed"`
	StartTime string                     `json:"startTime"`
	EndTime   string                     `json:"endTime"`
	Duration  string                     `json:"duration"`
	Results   []AutoUpdateResourceResult `json:"results"`
}

type AutoUpdateResourceResult struct {
	ResourceID      string                 `json:"resourceId"`
	ResourceName    string                 `json:"resourceName"`
	ResourceType    string                 `json:"resourceType"`
	Status          string                 `json:"status"`
	UpdateAvailable bool                   `json:"updateAvailable"`
	UpdateApplied   bool                   `json:"updateApplied"`
	OldImages       map[string]string      `json:"oldImages,omitempty"`
	NewImages       map[string]string      `json:"newImages,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Details         map[string]interface{} `json:"details,omitempty"`
}

type UpdaterStatusDto struct {
	Running     bool   `json:"running"`
	LastRun     string `json:"lastRun,omitempty"`
	NextRun     string `json:"nextRun,omitempty"`
	LastResult  string `json:"lastResult,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	TotalRuns   int    `json:"totalRuns"`
	SuccessRuns int    `json:"successRuns"`
	FailedRuns  int    `json:"failedRuns"`
}

type UpdaterHistoryEntryDto struct {
	ID        string           `json:"id"`
	StartTime string           `json:"startTime"`
	EndTime   string           `json:"endTime"`
	Duration  string           `json:"duration"`
	Status    string           `json:"status"`
	Checked   int              `json:"checked"`
	Updated   int              `json:"updated"`
	Skipped   int              `json:"skipped"`
	Failed    int              `json:"failed"`
	Error     string           `json:"error,omitempty"`
	Result    UpdaterRunResult `json:"result,omitempty"`
}
