package database

import "time"

// SQLiteConnectionOptions supplies pool-specific pragma defaults.
type SQLiteConnectionOptions struct {
	IgnoreForeignKeys bool
	JournalMode       string
	BusyTimeout       time.Duration
}
