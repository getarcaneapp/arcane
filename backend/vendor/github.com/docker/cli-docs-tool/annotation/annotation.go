// Copyright 2021 cli-docs-tool authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package annotation handles annotations for CLI commands.
package annotation

const (
	// ExternalURL specifies an external link annotation
	ExternalURL = "docs.external.url"
	// CodeDelimiter specifies the char that will be converted as code backtick.
	// Can be used on cmd for inheritance or a specific flag.
	CodeDelimiter = "docs.code-delimiter"
	// DefaultValue specifies the default value for a flag.
	DefaultValue = "docs.default-value"
)
