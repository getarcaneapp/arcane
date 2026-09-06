package jobschedule

import st "github.com/getarcaneapp/arcane/types/v2/scheduler"

type JobInput struct {
	ID    string `path:"id"`
	JobID string `path:"jobId" minLength:"1"`
}
type ListRunsInput struct {
	ID    string `path:"id"`
	JobID string `path:"jobId" minLength:"1"`
	Page  int    `query:"page" default:"1" minimum:"1"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
}
type RunInput struct {
	ID    string `path:"id"`
	JobID string `path:"jobId" minLength:"1"`
	RunID string `path:"runId" format:"uuid"`
}
type RunOutput struct{ Body st.Run }
type ListRunsOutput struct{ Body st.RunList }
