package transfer

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	kit "go.getarcane.app/kit/pkg"
)

var (
	anonymousVolumeNameInternal = regexp.MustCompile(`^[0-9a-f]{64}$`)
	volumeNameInternal          = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// buildPlanInternal runs the whole preflight: environments, capabilities,
// source and destination inspection, blockers, review items, and the hash.
func (s *Service) buildPlanInternal(ctx context.Context, sourceEnvID string, request transfertypes.Request, permissions *authz.PermissionSet) (transfertypes.Plan, error) {
	request.DestinationEnvironmentID = strings.TrimSpace(request.DestinationEnvironmentID)
	request.DestinationName = strings.TrimSpace(request.DestinationName)
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.VolumeName = strings.TrimSpace(request.VolumeName)
	if request.Kind != transfertypes.KindProject && request.Kind != transfertypes.KindVolume {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("kind must be project or volume"))
	}
	if request.Mode != transfertypes.ModeCopy && request.Mode != transfertypes.ModeMove {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("mode must be copy or move"))
	}
	if request.DestinationName == "" {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("destination name is required"))
	}
	if request.DestinationEnvironmentID == "" || request.DestinationEnvironmentID == sourceEnvID {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("destination must be a different environment"))
	}
	if request.Kind == transfertypes.KindProject && request.ProjectID == "" {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("project ID is required"))
	}
	if request.Kind == transfertypes.KindVolume && request.VolumeName == "" {
		return transfertypes.Plan{}, common.Classify(common.ErrValidation, errors.New("volume name is required"))
	}
	if _, err := s.environments.GetEnvironmentByID(ctx, request.DestinationEnvironmentID); err != nil {
		return transfertypes.Plan{}, common.Classify(common.ErrNotFound, errors.New("destination environment not found"))
	}

	plan := transfertypes.Plan{
		Request:                    request,
		SourceEnvironmentID:        sourceEnvID,
		SourceEnvironmentName:      s.environments.ResolveEnvironmentName(ctx, sourceEnvID),
		DestinationEnvironmentName: s.environments.ResolveEnvironmentName(ctx, request.DestinationEnvironmentID),
		Resources:                  []transfertypes.PlannedResource{},
		Consumers:                  []transfertypes.Consumer{},
		Blockers:                   []transfertypes.Blocker{},
		Reviews:                    []transfertypes.ReviewItem{},
		RequiredAcknowledgements:   []string{},
	}
	if sourceEnvID != environment.LocalEnvironmentID {
		if _, err := s.environments.GetEnvironmentByID(ctx, sourceEnvID); err != nil {
			return transfertypes.Plan{}, common.Classify(common.ErrNotFound, errors.New("source environment not found"))
		}
	}
	source := sourceEnvID
	destination := request.DestinationEnvironmentID
	if !s.checkCapabilitiesInternal(ctx, &plan, source, destination) {
		finalizePlanInternal(&plan, permissions)
		return plan, nil
	}
	switch request.Kind {
	case transfertypes.KindVolume:
		if err := s.planVolumeInternal(ctx, &plan, source, destination); err != nil {
			return transfertypes.Plan{}, err
		}
	case transfertypes.KindProject:
		if err := s.planProjectInternal(ctx, &plan, source, destination); err != nil {
			return transfertypes.Plan{}, err
		}
	}
	finalizePlanInternal(&plan, permissions)
	return plan, nil
}

func (s *Service) checkCapabilitiesInternal(ctx context.Context, plan *transfertypes.Plan, source, destination string) bool {
	ok := true
	for _, side := range []struct {
		label  string
		envID  string
		target *transfertypes.Capabilities
	}{{"source", source, &plan.SourceCapabilities}, {"destination", destination, &plan.DestinationCapabilities}} {
		capabilities, err := callInternal[transfertypes.Capabilities](ctx, s, side.envID, http.MethodGet, "/api/environments/0/transfer/capabilities", nil, callTimeoutInternal, func() (transfertypes.Capabilities, error) { return s.Capabilities(ctx) })
		if errors.Is(err, common.ErrNotFound) {
			err = ErrEndpointUnsupported
		}
		if err != nil {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "capabilities", Message: fmt.Sprintf("%s environment: %s", side.label, err.Error())})
			ok = false
			continue
		}
		if capabilities.Protocol != transfertypes.ProtocolVersion {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "protocol", Message: fmt.Sprintf("%s environment speaks transfer protocol %d; this manager needs %d", side.label, capabilities.Protocol, transfertypes.ProtocolVersion)})
			ok = false
		}
		if !strings.EqualFold(capabilities.OS, "linux") {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "platform", Message: fmt.Sprintf("%s environment runs %s; transfers support Linux Docker hosts", side.label, capabilities.OS)})
			ok = false
		}
		*side.target = capabilities
	}
	if ok && plan.Request.Kind == transfertypes.KindVolume && plan.SourceCapabilities.DaemonID != "" && plan.SourceCapabilities.DaemonID == plan.DestinationCapabilities.DaemonID {
		requireAckInternal(plan, transfertypes.AckSameDaemon, "Both environments use the same Docker daemon; the copy is created next to the source under its new name.")
	}
	if ok && plan.SourceCapabilities.Arch != "" && plan.DestinationCapabilities.Arch != "" && plan.SourceCapabilities.Arch != plan.DestinationCapabilities.Arch {
		plan.Reviews = append(plan.Reviews, transfertypes.ReviewItem{Code: "arch", Message: fmt.Sprintf("the destination is %s while the source is %s; images must be available for the destination platform", plan.DestinationCapabilities.Arch, plan.SourceCapabilities.Arch)})
	}
	return ok
}

func (s *Service) planVolumeInternal(ctx context.Context, plan *transfertypes.Plan, source, destination string) error {
	request := plan.Request
	if !volumeNameInternal.MatchString(request.DestinationName) {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "destination_name", Message: "destination volume name may contain only letters, digits, dots, hyphens and underscores", Resource: request.DestinationName})
	}
	inspection, err := callInternal[transfertypes.VolumeInspection](ctx, s, source, http.MethodGet, "/api/environments/0/transfer/volumes/"+url.PathEscape(request.VolumeName), nil, longTimeoutInternal, func() (transfertypes.VolumeInspection, error) {
		return s.volumes.InspectForTransfer(ctx, request.VolumeName)
	})
	if err != nil {
		return err
	}
	if !inspection.Exists {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "missing", Message: "the source volume does not exist", Resource: request.VolumeName})
		return nil
	}
	plan.Blockers = append(plan.Blockers, volumeBlockersInternal(inspection)...)
	if inspection.Hold != nil {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "held", Message: "the volume is reserved by transfer " + inspection.Hold.TransferID, Resource: request.VolumeName})
	}
	target, err := callInternal[transfertypes.VolumeInspection](ctx, s, destination, http.MethodGet, "/api/environments/0/transfer/volumes/"+url.PathEscape(request.DestinationName), nil, longTimeoutInternal, func() (transfertypes.VolumeInspection, error) {
		return s.volumes.InspectForTransfer(ctx, request.DestinationName)
	})
	if err != nil {
		return err
	}
	if target.Exists {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "destination_exists", Message: "a volume with the destination name already exists", Resource: request.DestinationName})
	}
	if target.Hold != nil {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "held", Message: "the destination name is reserved by transfer " + target.Hold.TransferID, Resource: request.DestinationName})
	}
	plan.Resources = append(plan.Resources, transfertypes.PlannedResource{
		Key:             resourceKeyInternal(transfertypes.ResourceVolume, request.VolumeName),
		Kind:            transfertypes.ResourceVolume,
		SourceName:      request.VolumeName,
		DestinationName: request.DestinationName,
		EstimatedBytes:  max(inspection.SizeBytes, 0),
		EstimatedFiles:  max(inspection.FileCount, 0),
	})
	plan.Consumers = append(plan.Consumers, inspection.Consumers...)
	if request.Mode == transfertypes.ModeMove {
		requireAckInternal(plan, transfertypes.AckVolumeMoveNoAttach, "Moving a volume copies its data only: no container is migrated and nothing is attached to the destination volume.")
		requireAckInternal(plan, transfertypes.AckMoveLeavesSource, "After the move the source consumers stay stopped and the source volume stays reserved until you roll back, clean up, or resume it.")
	}
	return nil
}

func volumeBlockersInternal(inspection transfertypes.VolumeInspection) []transfertypes.Blocker {
	var blockers []transfertypes.Blocker
	if inspection.Anonymous || anonymousVolumeNameInternal.MatchString(inspection.Name) {
		blockers = append(blockers, transfertypes.Blocker{Code: "anonymous", Message: "anonymous volumes cannot be transferred", Resource: inspection.Name})
	}
	if inspection.Internal {
		blockers = append(blockers, transfertypes.Blocker{Code: "internal", Message: "Arcane internal volumes cannot be transferred", Resource: inspection.Name})
	}
	if inspection.Driver != "" && inspection.Driver != "local" {
		blockers = append(blockers, transfertypes.Blocker{Code: "driver", Message: fmt.Sprintf("volume driver %q is not supported; only local volumes without driver options can be transferred", inspection.Driver), Resource: inspection.Name})
	}
	if len(inspection.Options) > 0 {
		blockers = append(blockers, transfertypes.Blocker{Code: "driver_options", Message: "volumes with driver options cannot be transferred", Resource: inspection.Name})
	}
	return blockers
}

func (s *Service) planProjectInternal(ctx context.Context, plan *transfertypes.Plan, source, destination string) error {
	request := plan.Request
	if projects.SanitizeProjectName(request.DestinationName) != request.DestinationName || strings.Trim(request.DestinationName, "_") == "" {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "destination_name", Message: "destination project name may contain only letters, digits, hyphens and underscores", Resource: request.DestinationName})
	}
	inspection, err := callInternal[transfertypes.ProjectInspection](ctx, s, source, http.MethodGet, "/api/environments/0/transfer/projects/"+url.PathEscape(request.ProjectID), nil, longTimeoutInternal, func() (transfertypes.ProjectInspection, error) {
		return s.projects.InspectForTransfer(ctx, request.ProjectID)
	})
	if err != nil {
		return err
	}
	plan.Blockers = append(plan.Blockers, projectBlockersInternal(inspection)...)
	containerNames, ports := planProjectServicesInternal(plan, inspection.Services)
	plan.DestinationServices = serviceNamesInternal(inspection.Services)
	plan.ServiceDependencies = map[string][]string{}
	for _, service := range inspection.Services {
		if len(service.DependsOn) > 0 {
			plan.ServiceDependencies[service.Name] = service.DependsOn
		}
	}
	plan.Consumers = append(plan.Consumers, inspection.Containers...)
	plan.Resources = append(plan.Resources, transfertypes.PlannedResource{
		Key:             resourceKeyInternal(transfertypes.ResourceProjectDir, request.ProjectID),
		Kind:            transfertypes.ResourceProjectDir,
		SourceName:      inspection.Name,
		DestinationName: request.DestinationName,
		EstimatedBytes:  max(inspection.DirSizeBytes, 0),
		EstimatedFiles:  max(inspection.DirFileCount, 0),
	})
	volumeNames, err := s.planProjectVolumesInternal(ctx, plan, source, inspection.Volumes)
	if err != nil {
		return err
	}
	planProjectBindsInternal(plan, inspection.Binds)
	check, err := callInternal[transfertypes.DestinationCheckResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/projects/check", transfertypes.DestinationCheckRequest{
		ProjectName:    request.DestinationName,
		VolumeNames:    volumeNames,
		ContainerNames: containerNames,
		Ports:          ports,
		Networks:       inspection.ExternalNetworks,
		Images:         inspection.Images,
	}, callTimeoutInternal, func() (transfertypes.DestinationCheckResponse, error) {
		return s.projects.CheckTransferDestination(ctx, transfertypes.DestinationCheckRequest{
			ProjectName:    request.DestinationName,
			VolumeNames:    volumeNames,
			ContainerNames: containerNames,
			Ports:          ports,
			Networks:       inspection.ExternalNetworks,
			Images:         inspection.Images,
		})
	})
	if err != nil {
		return err
	}
	plan.Blockers = append(plan.Blockers, destinationBlockersInternal(request.DestinationName, check)...)
	planProjectReviewsInternal(plan, ports)
	var total int64
	for _, resource := range plan.Resources {
		total += resource.EstimatedBytes
	}
	capacityReviewInternal(plan, total, check.FreeBytes)
	return nil
}

func projectBlockersInternal(inspection transfertypes.ProjectInspection) []transfertypes.Blocker {
	var blockers []transfertypes.Blocker
	if inspection.Archived {
		blockers = append(blockers, transfertypes.Blocker{Code: "archived", Message: "archived projects cannot be transferred"})
	}
	if inspection.GitOpsManaged {
		blockers = append(blockers, transfertypes.Blocker{Code: "gitops", Message: "the project is managed by GitOps; transfer the repository binding instead"})
	}
	if inspection.Hold != nil {
		blockers = append(blockers, transfertypes.Blocker{Code: "held", Message: "the project is reserved by transfer " + inspection.Hold.TransferID})
	}
	for _, file := range inspection.OutsideFiles {
		blockers = append(blockers, transfertypes.Blocker{Code: "outside_file", Message: "the project references a file outside its directory", Resource: file})
	}
	return blockers
}

// planProjectServicesInternal blocks unsupported services and collects the
// explicit container names and published ports the destination must be free of.
func planProjectServicesInternal(plan *transfertypes.Plan, services []transfertypes.ProjectServiceInspection) ([]string, []string) {
	var containerNames, ports []string
	for _, service := range services {
		if service.BuildOnly {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "build_only", Message: "service builds its image locally; build-only services cannot be transferred yet", Resource: service.Name})
		}
		if len(service.Devices) > 0 {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "devices", Message: "service maps host devices", Resource: service.Name})
		}
		if service.ContainerName != "" {
			containerNames = append(containerNames, service.ContainerName)
		}
		ports = append(ports, service.Ports...)
	}
	return containerNames, ports
}

// planProjectVolumesInternal adds one resource per existing compose volume
// and returns the destination names to check for collisions.
func (s *Service) planProjectVolumesInternal(ctx context.Context, plan *transfertypes.Plan, source string, declared []transfertypes.ProjectVolumeInspection) ([]string, error) {
	destinationVolumes := map[string]string{}
	var volumeNames []string
	for _, volume := range declared {
		if !volume.Exists {
			continue
		}
		if volume.Driver != "" && volume.Driver != "local" || len(volume.Options) > 0 {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "driver", Message: "only local volumes without driver options can be transferred", Resource: volume.Name})
		}
		if volume.Hold != nil {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "held", Message: "the volume is reserved by transfer " + volume.Hold.TransferID, Resource: volume.Name})
		}
		targetName := destinationVolumeNameInternal(volume, plan.Request)
		if previous, duplicate := destinationVolumes[targetName]; duplicate && previous != volume.Name {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "overlap", Message: fmt.Sprintf("volumes %s and %s both map to %s", previous, volume.Name, targetName), Resource: targetName})
		}
		destinationVolumes[targetName] = volume.Name
		volumeNames = append(volumeNames, targetName)
		inspection, err := callInternal[transfertypes.VolumeInspection](ctx, s, source, http.MethodGet, "/api/environments/0/transfer/volumes/"+url.PathEscape(volume.Name), nil, longTimeoutInternal, func() (transfertypes.VolumeInspection, error) { return s.volumes.InspectForTransfer(ctx, volume.Name) })
		if err != nil {
			return nil, err
		}
		plan.Blockers = append(plan.Blockers, volumeBlockersInternal(inspection)...)
		plan.Resources = append(plan.Resources, transfertypes.PlannedResource{
			Key:             resourceKeyInternal(transfertypes.ResourceVolume, volume.Name),
			Kind:            transfertypes.ResourceVolume,
			SourceName:      volume.Name,
			DestinationName: targetName,
			EstimatedBytes:  max(inspection.SizeBytes, 0),
			EstimatedFiles:  max(inspection.FileCount, 0),
		})
	}
	return volumeNames, nil
}

// planProjectBindsInternal blocks Docker-socket binds and bind mounts outside
// the project directory; binds inside it travel with the directory.
func planProjectBindsInternal(plan *transfertypes.Plan, binds []transfertypes.ProjectBindInspection) {
	for _, bind := range binds {
		switch {
		case bind.DockerSocket:
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "docker_socket", Message: "service mounts the Docker socket", Resource: bind.Service})
		case !bind.InsideProject:
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "bind_outside_project", Message: "bind mount outside the project directory is not transferred; move the data under the project or use a volume", Resource: bind.Source})
		}
	}
}

func destinationBlockersInternal(destinationName string, check transfertypes.DestinationCheckResponse) []transfertypes.Blocker {
	var blockers []transfertypes.Blocker
	if check.ProjectNameInUse {
		blockers = append(blockers, transfertypes.Blocker{Code: "destination_exists", Message: "a project with the destination name already exists", Resource: destinationName})
	}
	if check.DirectoryInUse {
		blockers = append(blockers, transfertypes.Blocker{Code: "destination_exists", Message: "a project directory with the destination name already exists", Resource: destinationName})
	}
	for _, name := range check.VolumesInUse {
		blockers = append(blockers, transfertypes.Blocker{Code: "destination_exists", Message: "a volume with this name already exists on the destination", Resource: name})
	}
	for _, name := range check.ContainerNamesUsed {
		blockers = append(blockers, transfertypes.Blocker{Code: "container_name", Message: "a container with this explicit name already exists on the destination", Resource: name})
	}
	for _, port := range check.PortsInUse {
		blockers = append(blockers, transfertypes.Blocker{Code: "port", Message: "a published port is already in use on the destination", Resource: port})
	}
	for _, network := range check.MissingNetworks {
		blockers = append(blockers, transfertypes.Blocker{Code: "network", Message: "external network is missing on the destination", Resource: network})
	}
	return blockers
}

func planProjectReviewsInternal(plan *transfertypes.Plan, ports []string) {
	if plan.Request.Mode != transfertypes.ModeMove {
		return
	}
	requireAckInternal(plan, transfertypes.AckMoveLeavesSource, "After the move the source project stays stopped and reserved until you roll back, clean up, or resume it.")
	if len(ports) > 0 {
		requireAckInternal(plan, transfertypes.AckExternalCutover, "Published ports move with the project; DNS, reverse proxies, and firewalls must be repointed at the destination host by you.")
	}
}

func destinationVolumeNameInternal(declared transfertypes.ProjectVolumeInspection, request transfertypes.Request) string {
	if mapped := strings.TrimSpace(request.VolumeMappings[declared.Name]); mapped != "" {
		return mapped
	}
	if declared.ExplicitName || declared.External {
		return declared.Name
	}
	return projects.NormalizeProjectName(request.DestinationName) + "_" + declared.Key
}

func serviceNamesInternal(services []transfertypes.ProjectServiceInspection) []string {
	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, service.Name)
	}
	sort.Strings(names)
	return names
}

func resourceKeyInternal(kind transfertypes.ResourceKind, name string) string {
	return string(kind) + ":" + name
}

func requireAckInternal(plan *transfertypes.Plan, code, message string) {
	plan.Reviews = append(plan.Reviews, transfertypes.ReviewItem{Code: code, Message: message, Required: true})
	plan.RequiredAcknowledgements = append(plan.RequiredAcknowledgements, code)
}

func capacityReviewInternal(plan *transfertypes.Plan, needed, free int64) {
	if needed <= 0 || free < 0 {
		return
	}
	if free < needed {
		plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "capacity", Message: fmt.Sprintf("the destination has %d bytes free but the transfer needs about %d", free, needed)})
	}
}

// finalizePlanInternal adds the review items every transfer carries, the
// downtime flag, permission blockers, totals, and the hash.
func finalizePlanInternal(plan *transfertypes.Plan, permissions *authz.PermissionSet) {
	requireAckInternal(plan, transfertypes.AckMetadataLimits, "File contents, numeric ownership, permission bits, modification times, symlinks, and hard links are preserved. ACLs and extended attributes are not.")
	running := 0
	for _, consumer := range plan.Consumers {
		if consumer.Running {
			running++
		}
	}
	plan.RequiresDowntime = running > 0
	if plan.RequiresDowntime {
		requireAckInternal(plan, transfertypes.AckConsumersStop, fmt.Sprintf("%d running container(s) will be stopped gracefully for the duration of the copy. Arcane cannot prevent external processes from writing while data is copied; detected changes fail verification.", running))
	}
	for _, resource := range plan.Resources {
		if resource.EstimatedBytes > 0 {
			plan.EstimatedBytes += resource.EstimatedBytes
		}
		if resource.EstimatedFiles > 0 {
			plan.EstimatedFiles += resource.EstimatedFiles
		}
	}
	authorizePlanInternal(permissions, plan)
	plan.PlanHash = ""
	plan.PlanHash = planHashInternal(*plan)
}

func planHashInternal(plan transfertypes.Plan) string {
	plan.PlanHash = ""
	encoded, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return kit.SHA256Hex(encoded)
}
