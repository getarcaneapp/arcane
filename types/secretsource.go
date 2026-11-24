package types

import (
	"time"
)

type (
	SecretSourceField struct {
		Key         string `json:"key" yaml:"key" doc:"Template key for the field"`
		Label       string `json:"label" yaml:"label" doc:"Form label"`
		Description string `json:"description,omitempty" yaml:"description" doc:"Help text shown under the field"`
		Placeholder string `json:"placeholder,omitempty" yaml:"placeholder" doc:"Input placeholder"`
		Default     string `json:"default,omitempty" yaml:"default" doc:"Value used when the field is left empty"`
		Required    bool   `json:"required,omitempty" yaml:"required" doc:"Whether the field must have a value"`
		Multiline   bool   `json:"multiline,omitempty" yaml:"multiline" doc:"Render the field as a textarea"`
	}

	SecretSourceHTTPStep struct {
		Query   map[string]string `json:"query,omitempty" yaml:"query" doc:"Templated query parameters; empty values are omitted"`
		Headers map[string]string `json:"headers,omitempty" yaml:"headers" doc:"Templated request headers"`
		JSON    map[string]any    `json:"json,omitempty" yaml:"json" doc:"Templated JSON body"`
		Form    map[string]string `json:"form,omitempty" yaml:"form" doc:"Templated form-encoded body"`
		Capture map[string]string `json:"capture,omitempty" yaml:"capture" doc:"Login only: dotted response paths captured into .login; expiresIn is the token lifetime in seconds"`
		Method  string            `json:"method" yaml:"method" doc:"HTTP method"`
		Path    string            `json:"path" yaml:"path" doc:"Templated path appended to inputs.baseUrl"`
	}

	SecretSourceListMapping struct {
		Key   string `json:"key" yaml:"key" doc:"Dotted path of the key inside each list item"`
		Value string `json:"value" yaml:"value" doc:"Dotted path of the value inside each list item"`
	}

	SecretSourceExtract struct {
		List   *SecretSourceListMapping `json:"list,omitempty" yaml:"list" doc:"Map an array of objects to key/value pairs"`
		From   string                   `json:"from" yaml:"from" doc:"Dotted path with * wildcards; . is the document root"`
		Object bool                     `json:"object,omitempty" yaml:"object" doc:"Take the scalar members of an object as key/value pairs"`
	}

	SecretSourceDefinition struct {
		APIVersion   string                `json:"apiVersion" yaml:"apiVersion" doc:"Definition schema version"`
		ID           string                `json:"id" yaml:"id" doc:"Definition identifier"`
		Name         string                `json:"name" yaml:"name" doc:"Display name"`
		Description  string                `json:"description,omitempty" yaml:"description" doc:"Short description"`
		Kind         string                `json:"kind" yaml:"kind" enum:"http,container" doc:"Engine that runs the definition"`
		Inputs       []SecretSourceField   `json:"inputs" yaml:"inputs" doc:"Non-secret fields, in form order"`
		Credentials  []SecretSourceField   `json:"credentials" yaml:"credentials" doc:"Secret fields, in form order"`
		Login        *SecretSourceHTTPStep `json:"login,omitempty" yaml:"login" doc:"Optional login request (http kind)"`
		Fetch        *SecretSourceHTTPStep `json:"fetch,omitempty" yaml:"fetch" doc:"Fetch request (http kind)"`
		Image        string                `json:"image,omitempty" yaml:"image" doc:"Templated image reference (container kind)"`
		Env          map[string]string     `json:"env,omitempty" yaml:"env" doc:"Templated container environment (container kind)"`
		Args         []string              `json:"args,omitempty" yaml:"args" doc:"Templated container command (container kind)"`
		Format       string                `json:"format,omitempty" yaml:"format" doc:"Templated stdout format: json (default) or dotenv (container kind)"`
		Extract      []SecretSourceExtract `json:"extract,omitempty" yaml:"extract" doc:"Extraction rules applied in order; later rules override earlier keys"`
		PathRequired bool                  `json:"pathRequired,omitempty" yaml:"pathRequired" doc:"Whether the source path must be set"`
	}

	SecretSourcePreset struct {
		Source     string                 `json:"source" doc:"Preset YAML, usable as a starting point for a custom definition"`
		Definition SecretSourceDefinition `json:"definition" doc:"Parsed preset definition"`
	}

	SecretSource struct {
		CreatedAt            time.Time              `json:"createdAt" readOnly:"true"`
		UpdatedAt            *time.Time             `json:"updatedAt,omitempty" readOnly:"true"`
		LastFetchedAt        *time.Time             `json:"lastFetchedAt,omitempty" readOnly:"true" doc:"Last successful fetch"`
		ID                   string                 `json:"id" readOnly:"true"`
		Name                 string                 `json:"name"`
		Kind                 string                 `json:"kind" readOnly:"true"`
		PresetID             string                 `json:"presetId,omitempty"`
		Inputs               map[string]string      `json:"inputs"`
		CredentialKeys       []string               `json:"credentialKeys" readOnly:"true" doc:"Credential keys that have a stored value"`
		Path                 string                 `json:"path,omitempty"`
		KeyPrefix            string                 `json:"keyPrefix,omitempty"`
		LastError            string                 `json:"lastError,omitempty" readOnly:"true"`
		EnvironmentIDs       []string               `json:"environmentIds"`
		Definition           SecretSourceDefinition `json:"definition" readOnly:"true"`
		LastKeyCount         int                    `json:"lastKeyCount" readOnly:"true"`
		LastSkippedKeyCount  int                    `json:"lastSkippedKeyCount" readOnly:"true" doc:"Keys skipped by the last fetch because they are not valid variable names"`
		AllEnvironments      bool                   `json:"allEnvironments"`
		InjectIntoContainers bool                   `json:"injectIntoContainers"`
		Enabled              bool                   `json:"enabled"`
	}

	SecretSourceInput struct {
		Name                 string            `json:"name" required:"false" maxLength:"255" doc:"Unique source name"`
		PresetID             string            `json:"presetId" required:"false" doc:"Preset to use when definition is empty"`
		Definition           string            `json:"definition" required:"false" doc:"Custom definition YAML; overrides presetId"`
		Inputs               map[string]string `json:"inputs" required:"false" doc:"Values for the definition's inputs"`
		Credentials          map[string]string `json:"credentials" required:"false" doc:"Values for the definition's credentials; on update, omitted keys are kept and an empty value clears the key"`
		Path                 string            `json:"path" required:"false" doc:"Optional path passed to the definition as .path"`
		KeyPrefix            string            `json:"keyPrefix" required:"false" doc:"Prefix prepended to every yielded key"`
		EnvironmentIDs       []string          `json:"environmentIds" required:"false"`
		AllEnvironments      bool              `json:"allEnvironments" required:"false"`
		InjectIntoContainers bool              `json:"injectIntoContainers" required:"false"`
		Enabled              bool              `json:"enabled" required:"false" doc:"Defaults to true on create"`
	}

	SecretSourceTestInput struct {
		ID string `json:"id" required:"false" doc:"Saved source to test; submitted fields override its stored values"`
		SecretSourceInput
	}

	SecretSourceTestResult struct {
		Error           string   `json:"error,omitempty"`
		Keys            []string `json:"keys" doc:"Key names the source yields, after the key prefix"`
		KeyCount        int      `json:"keyCount"`
		SkippedKeyCount int      `json:"skippedKeyCount"`
		Success         bool     `json:"success"`
	}

	SecretSourceValidateInput struct {
		Definition string `json:"definition" minLength:"1" doc:"Definition YAML"`
	}

	SecretSourceMutationResponse struct {
		Source      *SecretSource           `json:"source,omitempty"`
		SyncResults []EnvironmentSyncStatus `json:"syncResults,omitempty"`
	}
)
