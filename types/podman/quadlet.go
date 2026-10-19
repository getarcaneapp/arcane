package podman

type QuadletType string

const (
	QuadletTypeContainer QuadletType = "container"
	QuadletTypePod       QuadletType = "pod"
	QuadletTypeKube      QuadletType = "kube"
	QuadletTypeNetwork   QuadletType = "network"
	QuadletTypeVolume    QuadletType = "volume"
	QuadletTypeImage     QuadletType = "image"
	QuadletTypeBuild     QuadletType = "build"
	QuadletTypeArtifact  QuadletType = "artifact"
)

type Quadlet struct {
	Name     string `json:"name"`
	UnitName string `json:"unitName,omitempty"`
	Path     string `json:"path"`
	Status   string `json:"status"`
	App      string `json:"app,omitempty"`
}

type QuadletFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type QuadletInstallRequest struct {
	Name       string        `json:"name" minLength:"1" doc:"Quadlet file name including its extension"`
	Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
	ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
	App        string        `json:"app,omitempty" doc:"Application the quadlet belongs to"`
}

type QuadletUpdateRequest struct {
	Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
	ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
}

type QuadletDetail struct {
	Name       string      `json:"name"`
	UnitName   string      `json:"unitName"`
	Path       string      `json:"path"`
	Status     string      `json:"status"`
	App        string      `json:"app,omitempty"`
	Type       QuadletType `json:"type"`
	Template   bool        `json:"template"`
	UnitStatus *UnitStatus `json:"unitStatus,omitempty"`
	Containers []Container `json:"containers,omitempty"`
}

type QuadletInstallReport struct {
	InstalledQuadlets map[string]string `json:"installedQuadlets"`
	QuadletErrors     map[string]string `json:"quadletErrors"`
}

type QuadletRemoveReport struct {
	Removed []string          `json:"removed"`
	Errors  map[string]string `json:"errors"`
}
