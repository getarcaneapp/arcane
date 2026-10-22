package types

type SelectorKind string

const (
	SelectorKindLabels   SelectorKind = "labels"
	SelectorKindProjects SelectorKind = "projects"
	SelectorKindOwner    SelectorKind = "owner"
	SelectorKindNames    SelectorKind = "names"
)

type ResourceSelector struct {
	Labels    map[string]string `json:"labels,omitempty" doc:"Labels the resource must carry; every pair must match"`
	Projects  []string          `json:"projects,omitempty" doc:"Compose project names; the resource must belong to one of them"`
	OwnerOnly bool              `json:"ownerOnly,omitempty" doc:"Restrict to resources owned by the bound subject"`
	Names     []string          `json:"names,omitempty" doc:"Resource name globs (path.Match syntax); any may match"`
}
