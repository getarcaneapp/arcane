package user

// TableFilterPreference stores a table filter as its column ID and value.
type TableFilterPreference [2]any

// TableSortPreference stores a table sort column and direction.
type TableSortPreference [2]string

// TablePreference contains the persisted presentation state for one table.
type TablePreference struct {
	GlobalSearch     *string                 `json:"g,omitempty" doc:"Global table search"`
	Sort             *TableSortPreference    `json:"s,omitempty" doc:"Sort column and direction"`
	PageSize         *int                    `json:"l,omitempty" doc:"Rows displayed per page"`
	HiddenColumns    []string                `json:"v,omitempty" doc:"Hidden table column IDs"`
	Filters          []TableFilterPreference `json:"f,omitempty" doc:"Active table filters"`
	MobileVisibility []string                `json:"m,omitempty" doc:"Encoded mobile table field visibility"`
	CustomSettings   map[string]any          `json:"c,omitempty" doc:"Table-specific settings"`
}

// TablePreferences maps table persistence keys to their state. A nil value in
// a PATCH request deletes that table's saved preference.
type TablePreferences map[string]*TablePreference

// Preferences contains the current user's persisted application preferences.
type Preferences struct {
	Tables TablePreferences `json:"tables" doc:"Table preferences keyed by persistence key"`
}
