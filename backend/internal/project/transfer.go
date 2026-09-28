package project

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	transferlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/transfer"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"github.com/moby/go-archive"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	kit "go.getarcane.app/kit/pkg"
	"go.yaml.in/yaml/v4"
)

// InspectForTransfer reports everything a transfer plan needs to know about the project.
func (s *ProjectService) InspectForTransfer(ctx context.Context, projectID string) (transfertypes.ProjectInspection, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return transfertypes.ProjectInspection{}, err
	}
	composeProject, composeFile, err := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if err != nil {
		return transfertypes.ProjectInspection{}, errors.WrapIf(err, "load compose project")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return transfertypes.ProjectInspection{}, err
	}
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		return transfertypes.ProjectInspection{}, errors.WrapIf(err, "list project containers")
	}
	// A nil mapper means Arcane and the Docker host share the same paths.
	hostProjectPath := proj.Path
	if pathMapper := s.projectPathMapperInternal(ctx); pathMapper != nil {
		hostProjectPath, _, _ = pathMapper.ContainerToHost(proj.Path)
	}
	inspection := transfertypes.ProjectInspection{
		ProjectID:     proj.ID,
		Name:          proj.Name,
		Path:          proj.Path,
		ComposeFile:   composeFile,
		Status:        string(proj.Status),
		Archived:      proj.IsArchived,
		GitOpsManaged: isGitOpsManagedProjectInternal(proj),
		Profiles:      composeProject.Profiles,
		Images:        kit.Unique(slices.Concat(projects.PullableImageRefs(composeProject), projects.BuildImageRefsFromComposeProject(composeProject))),
		OutsideFiles:  transferOutsideFilesInternal(composeProject, composeFile, proj.Path, hostProjectPath),
	}
	for _, c := range lookupProjectContainersInternal(*proj, groupComposeContainersByProjectInternal(containers)) {
		inspect, err := dockerClient.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			return transfertypes.ProjectInspection{}, errors.WrapIff(err, "inspect container %s", dockerutil.ContainerNameFromNames(c.Names))
		}
		inspection.Containers = append(inspection.Containers, dockerutil.TransferConsumerFromInspect(inspect.Container))
	}
	inspection.Services, inspection.Binds = transferServiceInspectionsInternal(composeProject, inspection.Containers, proj.Path, hostProjectPath)
	if inspection.Volumes, err = s.transferVolumeInspectionsInternal(ctx, dockerClient, composeProject); err != nil {
		return transfertypes.ProjectInspection{}, err
	}
	for _, key := range slices.Sorted(maps.Keys(composeProject.Networks)) {
		if network := composeProject.Networks[key]; bool(network.External) {
			inspection.ExternalNetworks = append(inspection.ExternalNetworks, cmp.Or(network.Name, key))
		}
	}
	err = acfs.Walk(ctx, proj.Path, "/", func(entry acfstypes.Entry) error {
		inspection.DirFileCount++
		if !entry.IsDirectory && !entry.IsSymlink {
			inspection.DirSizeBytes += entry.Size
		}
		return nil
	})
	if err != nil {
		return transfertypes.ProjectInspection{}, errors.WrapIf(err, "measure project directory")
	}
	if inspection.Hold, err = s.holds.Get(ctx, transfertypes.KindProject, proj.ID); err != nil {
		return transfertypes.ProjectInspection{}, err
	}
	return inspection, nil
}

// transferServiceInspectionsInternal describes each compose service, attaching
// its running (else first) container, and collects the services' bind mounts.
func transferServiceInspectionsInternal(composeProject *composetypes.Project, consumers []transfertypes.Consumer, projectPath, hostProjectPath string) ([]transfertypes.ProjectServiceInspection, []transfertypes.ProjectBindInspection) {
	names := slices.Sorted(maps.Keys(composeProject.Services))
	services := make([]transfertypes.ProjectServiceInspection, 0, len(names))
	var binds []transfertypes.ProjectBindInspection
	for _, name := range names {
		svc := composeProject.Services[name]
		services = append(services, transferServiceInspectionInternal(name, svc, consumers))
		for _, volume := range svc.Volumes {
			if volume.Type != composetypes.VolumeTypeBind || volume.Source == "" {
				continue
			}
			binds = append(binds, transfertypes.ProjectBindInspection{
				Service:       name,
				Source:        volume.Source,
				Target:        volume.Target,
				ReadOnly:      volume.ReadOnly,
				InsideProject: transferPathInsideProjectInternal(projectPath, hostProjectPath, volume.Source),
				DockerSocket:  strings.HasSuffix(volume.Source, "docker.sock"),
				SizeBytes:     -1,
				FileCount:     -1,
			})
		}
	}
	return services, binds
}

func transferServiceInspectionInternal(name string, svc composetypes.ServiceConfig, consumers []transfertypes.Consumer) transfertypes.ProjectServiceInspection {
	entry := transfertypes.ProjectServiceInspection{
		Name:      name,
		Image:     svc.Image,
		BuildOnly: svc.Build != nil && strings.TrimSpace(svc.Image) == "",
		DependsOn: slices.Sorted(maps.Keys(svc.DependsOn)),
		Networks:  slices.Sorted(maps.Keys(svc.Networks)),
	}
	if svc.StopGracePeriod != nil {
		entry.StopTimeout = int(time.Duration(*svc.StopGracePeriod) / time.Second)
	}
	for _, port := range svc.Ports {
		if port.Published != "" {
			entry.Ports = append(entry.Ports, fmt.Sprintf("%s:%d/%s", port.Published, port.Target, cmp.Or(port.Protocol, "tcp")))
		}
	}
	for _, device := range svc.Devices {
		entry.Devices = append(entry.Devices, cmp.Or(kit.Ternary(device.Target != "" && device.Target != device.Source, device.Source+":"+device.Target, ""), device.Source))
	}
	for _, consumer := range consumers {
		if consumer.ComposeService != name || (entry.ContainerID != "" && !consumer.Running) {
			continue
		}
		entry.ContainerID, entry.ContainerName, entry.Running = consumer.ContainerID, consumer.Name, consumer.Running
		entry.State = kit.Ternary(consumer.Running, string(container.StateRunning), "exited")
	}
	return entry
}

func (s *ProjectService) transferVolumeInspectionsInternal(ctx context.Context, dockerClient *client.Client, composeProject *composetypes.Project) ([]transfertypes.ProjectVolumeInspection, error) {
	explicit, err := projects.ComposeVolumeKeysWithExplicitName(composeProject.ComposeFiles)
	if err != nil {
		return nil, errors.WrapIf(err, "parse compose volume names")
	}
	volumes := make([]transfertypes.ProjectVolumeInspection, 0, len(composeProject.Volumes))
	for _, key := range slices.Sorted(maps.Keys(composeProject.Volumes)) {
		cfg := composeProject.Volumes[key]
		_, explicitName := explicit[key]
		entry := transfertypes.ProjectVolumeInspection{
			Key:          key,
			Name:         cmp.Or(cfg.Name, composeProject.Name+"_"+key),
			External:     bool(cfg.External),
			ExplicitName: explicitName,
			Driver:       cfg.Driver,
			Options:      cfg.DriverOpts,
			SizeBytes:    -1,
			FileCount:    -1,
		}
		if _, inspectErr := dockerClient.VolumeInspect(ctx, entry.Name, client.VolumeInspectOptions{}); inspectErr == nil {
			entry.Exists = true
		} else if !cerrdefs.IsNotFound(inspectErr) {
			return nil, errors.WrapIff(inspectErr, "inspect volume %s", entry.Name)
		}
		if entry.Hold, err = s.holds.Get(ctx, transfertypes.KindVolume, entry.Name); err != nil {
			return nil, err
		}
		volumes = append(volumes, entry)
	}
	return volumes, nil
}

// transferPathInsideProjectInternal reports whether candidate lives in the
// project directory, seen either from Arcane or from the Docker host.
func transferPathInsideProjectInternal(projectPath, hostProjectPath, candidate string) bool {
	return projects.IsSafeSubdirectory(projectPath, candidate) || projects.IsSafeSubdirectory(hostProjectPath, candidate)
}

// transferOutsideFilesInternal lists env_file, config, secret, and include
// paths that resolve outside the project directory.
func transferOutsideFilesInternal(composeProject *composetypes.Project, composeFile, projectPath, hostProjectPath string) []string {
	var candidates []string
	for _, svc := range composeProject.Services {
		for _, envFile := range svc.EnvFiles {
			candidates = append(candidates, envFile.Path)
		}
	}
	for _, config := range composeProject.Configs {
		candidates = append(candidates, config.File)
	}
	for _, secret := range composeProject.Secrets {
		candidates = append(candidates, secret.File)
	}
	if includes, err := projects.ParseIncludes(composeFile, projects.EnvMap(composeProject.Environment), false); err == nil {
		for _, include := range includes {
			candidates = append(candidates, include.Path)
		}
	}
	var outside []string
	for _, candidate := range kit.Unique(kit.TrimNonEmpty(candidates)) {
		if !transferPathInsideProjectInternal(projectPath, hostProjectPath, candidate) {
			outside = append(outside, candidate)
		}
	}
	slices.Sort(outside)
	return outside
}

// OpenTransferArchive streams the project directory as a tar archive.
func (s *ProjectService) OpenTransferArchive(ctx context.Context, projectID string) (io.ReadCloser, func(), error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	reader, err := archive.TarWithOptions(proj.Path, &archive.TarOptions{})
	if err != nil {
		return nil, nil, errors.WrapIf(err, "archive project directory")
	}
	return reader, func() { _ = reader.Close() }, nil
}

// ImportTransferArchive creates <projectsDir>/<dirName> and extracts source
// into it; the directory is removed on failure.
func (s *ProjectService) ImportTransferArchive(ctx context.Context, dirName string, source io.Reader) (string, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return "", err
	}
	projectPath, err := transferProjectPathInternal(projectsDirectory, dirName)
	if err != nil {
		return "", err
	}
	if _, _, err := projects.CreateExactDir(ctx, projectsDirectory, projectPath, dirName, utils.DirPerm); err != nil {
		return "", errors.WrapIf(err, "create project directory")
	}
	if err := archive.Untar(source, projectPath, &archive.TarOptions{NoLchown: true, BestEffortXattrs: true}); err != nil {
		if removeErr := acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, "/"+dirName); removeErr != nil {
			slog.WarnContext(ctx, "failed to remove partially imported project directory", "path", projectPath, "error", removeErr)
		}
		return "", errors.WrapIf(err, "import project archive")
	}
	return projectPath, nil
}

// transferProjectPathInternal validates dirName as one safe path segment and
// returns its absolute path under the projects directory.
func transferProjectPathInternal(projectsDirectory, dirName string) (string, error) {
	if dirName == "" || projects.SanitizeProjectName(dirName) != dirName || strings.Trim(dirName, "_") == "" || filepath.Base(dirName) != dirName {
		return "", common.Classify(common.ErrBadRequest, errors.Errorf("invalid project directory name %q", dirName))
	}
	projectPath := filepath.Join(projectsDirectory, dirName)
	if !projects.IsSafeSubdirectory(projectsDirectory, projectPath) || filepath.Clean(projectsDirectory) == projectPath {
		return "", common.Classify(common.ErrBadRequest, errors.Errorf("project directory %q escapes the projects directory", dirName))
	}
	return projectPath, nil
}

// CheckTransferDestination reports the collisions a project would hit on this node.
func (s *ProjectService) CheckTransferDestination(ctx context.Context, request transfertypes.DestinationCheckRequest) (transfertypes.DestinationCheckResponse, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return transfertypes.DestinationCheckResponse{}, err
	}
	response := transfertypes.DestinationCheckResponse{FreeBytes: directoryFreeBytesInternal(projectsDirectory)}
	if response.ProjectNameInUse, err = s.projectNameInUseInternal(ctx, request.ProjectName); err != nil {
		return transfertypes.DestinationCheckResponse{}, err
	}
	_, statErr := os.Lstat(filepath.Join(projectsDirectory, projects.SanitizeProjectName(request.ProjectName)))
	response.DirectoryInUse = statErr == nil
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return transfertypes.DestinationCheckResponse{}, err
	}
	for _, name := range request.VolumeNames {
		if _, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); err == nil {
			response.VolumesInUse = append(response.VolumesInUse, name)
		}
	}
	for _, name := range request.Networks {
		if _, err := dockerClient.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err != nil {
			response.MissingNetworks = append(response.MissingNetworks, name)
		}
	}
	if len(request.ContainerNames) == 0 && len(request.Ports) == 0 {
		return response, nil
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return transfertypes.DestinationCheckResponse{}, errors.WrapIf(err, "list containers")
	}
	response.ContainerNamesUsed, response.PortsInUse = transferContainerCollisionsInternal(listed.Items, request)
	return response, nil
}

// transferContainerCollisionsInternal returns the requested container names
// already taken and the requested published ports bound by running containers.
func transferContainerCollisionsInternal(containers []container.Summary, request transfertypes.DestinationCheckRequest) ([]string, []string) {
	var usedNames, boundPorts []string
	for _, c := range containers {
		for _, name := range c.Names {
			usedNames = append(usedNames, strings.TrimPrefix(name, "/"))
		}
		if c.State != container.StateRunning {
			continue
		}
		for _, port := range c.Ports {
			if port.PublicPort > 0 {
				boundPorts = append(boundPorts, transferPortKeyInternal(fmt.Sprintf("%d/%s", port.PublicPort, port.Type)))
			}
		}
	}
	var namesUsed, portsInUse []string
	for _, name := range request.ContainerNames {
		if slices.Contains(usedNames, name) {
			namesUsed = append(namesUsed, name)
		}
	}
	for _, port := range request.Ports {
		if slices.Contains(boundPorts, transferPortKeyInternal(port)) {
			portsInUse = append(portsInUse, port)
		}
	}
	return namesUsed, portsInUse
}

func (s *ProjectService) projectNameInUseInternal(ctx context.Context, name string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&Project{}).Where("name = ? OR name = ?", name, projects.NormalizeProjectName(name)).Count(&count).Error
	if err != nil {
		return false, errors.WrapIf(err, "check project name")
	}
	return count > 0, nil
}

// transferPortKeyInternal normalizes "8080", "8080/tcp", or "0.0.0.0:8080/tcp" to "8080/tcp".
func transferPortKeyInternal(entry string) string {
	spec, protocol, _ := strings.Cut(strings.TrimSpace(entry), "/")
	if index := strings.LastIndex(spec, ":"); index >= 0 {
		spec = spec[index+1:]
	}
	return spec + "/" + strings.ToLower(cmp.Or(protocol, "tcp"))
}

// RegisterTransferredProject turns an imported directory into a managed,
// stopped project after rewriting its identity.
func (s *ProjectService) RegisterTransferredProject(ctx context.Context, request transfertypes.RegisterProjectRequest) (transfertypes.RegisterProjectResponse, error) {
	ctx, user := transferlib.WithHoldOwner(ctx, request.TransferID), common.SystemUser
	name := strings.TrimSpace(request.Rewrite.Name)
	if name == "" {
		return transfertypes.RegisterProjectResponse{}, common.Classify(common.ErrBadRequest, errors.New("project name is required"))
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return transfertypes.RegisterProjectResponse{}, err
	}
	projectPath, err := transferProjectPathInternal(projectsDirectory, request.ProjectDir)
	if err != nil {
		return transfertypes.RegisterProjectResponse{}, err
	}
	if isDir, statErr := projects.IsProjectDirectoryPath(projectPath, false); statErr != nil || !isDir {
		return transfertypes.RegisterProjectResponse{}, common.Classify(common.ErrNotFound, errors.Errorf("imported project directory %s not found", request.ProjectDir))
	}
	if inUse, err := s.projectNameInUseInternal(ctx, name); err != nil {
		return transfertypes.RegisterProjectResponse{}, err
	} else if inUse {
		return transfertypes.RegisterProjectResponse{}, common.Classify(common.ErrConflict, errors.Errorf("project %s already exists", name))
	}
	composeContent, composeFile, err := rewriteTransferredProjectFilesInternal(ctx, projectsDirectory, projectPath, request.Rewrite)
	if err != nil {
		return transfertypes.RegisterProjectResponse{}, err
	}
	if err := projects.ValidateComposeContentForUpdate(ctx, projectsDirectory, projectPath, name, composeContent, nil, nil, "", false); err != nil {
		return transfertypes.RegisterProjectResponse{}, errors.WrapIf(err, "invalid compose file")
	}
	dirName := request.ProjectDir
	proj := &Project{Name: name, DirName: &dirName, Path: projectPath, Status: ProjectStatusStopped}
	if err := s.registerProjectDirectoryInternal(ctx, projectsDirectory, composeFile, proj, nil, nil); err != nil {
		return transfertypes.RegisterProjectResponse{}, err
	}
	metadata := database.JSON{"action": "create", "projectID": proj.ID, "projectName": proj.Name, "path": projectPath, "transferID": request.TransferID}
	s.logProjectEventInternal(ctx, event.EventTypeProjectCreate, proj.ID, proj.Name, user, metadata, "could not log transferred project creation")
	return transfertypes.RegisterProjectResponse{ProjectID: proj.ID, Name: proj.Name}, nil
}

// rewriteTransferredProjectFilesInternal applies the identity rewrite to the
// compose file and freezes global env values; it returns the compose content.
func rewriteTransferredProjectFilesInternal(ctx context.Context, projectsDirectory, projectPath string, rewrite transfertypes.ProjectRewrite) (string, string, error) {
	composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, projectPath)
	if err != nil {
		return "", "", err
	}
	composeLogical, err := acfs.LogicalPath(projectPath, composeFile)
	if err != nil {
		return "", "", errors.WrapIf(err, "compose file must live inside the project directory")
	}
	source, err := acfs.ReadFile(ctx, projectPath, composeLogical)
	if err != nil {
		return "", "", errors.WrapIf(err, "read compose file")
	}
	updated, err := rewriteTransferredComposeInternal(source, rewrite, projectPath)
	if err != nil {
		return "", "", err
	}
	if !bytes.Equal(source, updated) {
		entry, err := acfs.Stat(ctx, projectPath, composeLogical, false)
		if err != nil {
			return "", "", errors.WrapIf(err, "inspect compose file")
		}
		if err := acfs.Write(ctx, projectPath, composeLogical, updated, acfs.WriteOptions{Mode: fs.FileMode(entry.UnixMode).Perm(), InPlace: true}); err != nil {
			return "", "", errors.WrapIf(err, "write rewritten compose file")
		}
	}
	return string(updated), composeFile, nil
}

// composeRewriteInternal collects byte-span edits against one compose source
// so operator formatting survives the rewrite.
type composeRewriteInternal struct {
	source      []byte
	rewrite     transfertypes.ProjectRewrite
	projectPath string
	edits       []composeImageEditInternal
}

// rewriteTransferredComposeInternal edits the top-level name, mapped volume
// names, and mapped bind sources in place.
func rewriteTransferredComposeInternal(source []byte, rewrite transfertypes.ProjectRewrite, projectPath string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, errors.WrapIf(err, "parse compose file")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("compose file must contain a mapping")
	}
	root := document.Content[0]
	rewriter := &composeRewriteInternal{source: source, rewrite: rewrite, projectPath: projectPath}
	if nameNode := composeImageFieldInternal(root, "name"); nameNode != nil && nameNode.Kind == yaml.ScalarNode {
		if err := rewriter.replaceInternal(nameNode, rewrite.Name); err != nil {
			return nil, errors.WrapIf(err, "rewrite project name")
		}
	}
	if volumes := composeImageFieldInternal(root, "volumes"); volumes != nil && volumes.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(volumes.Content); i += 2 {
			if err := rewriter.rewriteVolumeInternal(volumes.Content[i].Value, volumes.Content[i+1], rewrite.VolumeMappings); err != nil {
				return nil, errors.WrapIff(err, "rewrite volume %s", volumes.Content[i].Value)
			}
		}
	}
	if services := composeImageFieldInternal(root, "services"); services != nil && services.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(services.Content); i += 2 {
			serviceVolumes := composeImageFieldInternal(services.Content[i+1], "volumes")
			if serviceVolumes == nil || serviceVolumes.Kind != yaml.SequenceNode {
				continue
			}
			for _, item := range serviceVolumes.Content {
				if err := rewriter.rewriteBindSourceInternal(item); err != nil {
					return nil, errors.WrapIff(err, "rewrite bind mount for service %s", services.Content[i].Value)
				}
			}
		}
	}
	// Splice from the end so earlier spans keep their offsets.
	slices.SortFunc(rewriter.edits, func(a, b composeImageEditInternal) int { return b.start - a.start })
	updated := slices.Clone(source)
	for _, edit := range rewriter.edits {
		updated = slices.Concat(updated[:edit.start], edit.text, updated[edit.end:])
	}
	return updated, nil
}

// replaceInternal queues a scalar replacement when the value differs.
func (r *composeRewriteInternal) replaceInternal(node *yaml.Node, value string) error {
	if node.Value == value {
		return nil
	}
	edit, err := composeImageSourceEditInternal(r.source, node, value)
	if err != nil {
		return err
	}
	r.edits = append(r.edits, edit)
	return nil
}

// rewriteVolumeInternal renames a volume with an explicit name: and gives an
// external volume named by its key a name: entry.
func (r *composeRewriteInternal) rewriteVolumeInternal(key string, value *yaml.Node, mappings map[string]string) error {
	if nameNode := composeImageFieldInternal(value, "name"); nameNode != nil && nameNode.Kind == yaml.ScalarNode {
		if target, ok := mappings[nameNode.Value]; ok {
			return r.replaceInternal(nameNode, target)
		}
		return nil
	}
	target, ok := mappings[key]
	external := composeImageFieldInternal(value, "external")
	if !ok || target == key || external == nil || external.Kind != yaml.ScalarNode {
		return nil
	}
	if isExternal, _ := strconv.ParseBool(external.Value); !isExternal {
		return nil
	}
	if value.Style&yaml.FlowStyle != 0 || len(value.Content) == 0 {
		return errors.New("only block-style mappings can receive a name")
	}
	// Insert "name: <target>" before the first key, indented like it; volume names never need quoting.
	firstKey := value.Content[0]
	anchor, err := composeImageSourceEditInternal(r.source, firstKey, firstKey.Value)
	if err != nil {
		return err
	}
	text := "name: " + target + "\n" + strings.Repeat(" ", max(firstKey.Column-1, 0))
	r.edits = append(r.edits, composeImageEditInternal{start: anchor.start, end: anchor.start, text: []byte(text)})
	return nil
}

// rewriteBindSourceInternal handles a short "src:target[:opts]" scalar or a
// long-syntax source: scalar.
func (r *composeRewriteInternal) rewriteBindSourceInternal(item *yaml.Node) error {
	if item.Kind == yaml.ScalarNode {
		sourcePath, rest, found := strings.Cut(item.Value, ":")
		if target, ok := r.bindTargetInternal(sourcePath); found && ok {
			return r.replaceInternal(item, target+":"+rest)
		}
		return nil
	}
	sourceNode := composeImageFieldInternal(item, "source")
	if sourceNode == nil || sourceNode.Kind != yaml.ScalarNode {
		return nil
	}
	if target, ok := r.bindTargetInternal(sourceNode.Value); ok {
		return r.replaceInternal(sourceNode, target)
	}
	return nil
}

// bindTargetInternal relocates absolute bind sources under the source project
// directory to the destination directory so the copy never mounts the
// original's files.
func (r *composeRewriteInternal) bindTargetInternal(source string) (string, bool) {
	if r.rewrite.SourcePath == "" || r.projectPath == "" || !filepath.IsAbs(source) {
		return "", false
	}
	relative, err := filepath.Rel(filepath.Clean(r.rewrite.SourcePath), filepath.Clean(source))
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", false
	}
	return filepath.Join(r.projectPath, relative), true
}

// PrepareTransferredProject pulls the project's images and creates its
// containers without starting them.
func (s *ProjectService) PrepareTransferredProject(ctx context.Context, projectID string, request transfertypes.ProjectActionRequest) error {
	ctx, user := transferlib.WithHoldOwner(ctx, request.TransferID), common.SystemUser
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}
	credentials, err := s.ResolveRegistryCredentials(ctx)
	if err != nil {
		return err
	}
	if err := s.EnsureProjectImagesPresent(ctx, projectID, io.Discard, user, credentials); err != nil {
		return err
	}
	composeProject, _, err := s.loadComposeProjectForProjectInternal(ctx, proj, prepareProjectBindDirectoriesInternal(proj.Path))
	if err != nil {
		return errors.WrapIf(err, "load compose project")
	}
	defer s.eventService.BeginComposeSuppressionWindow(composeProject.Name)()
	return errors.WrapIf(projects.ComposeCreate(ctx, composeProject, nil, s.composeRegistryAuthConfigsInternal(ctx)), "create project containers")
}

// DeployTransferredProject deploys the destination project for a Move cutover.
func (s *ProjectService) DeployTransferredProject(ctx context.Context, projectID string, request transfertypes.ProjectActionRequest) error {
	return s.DeployProject(transferlib.WithHoldOwner(ctx, request.TransferID), projectID, common.SystemUser, nil)
}

// RemoveTransferProject destroys a project under the transfer's hold.
func (s *ProjectService) RemoveTransferProject(ctx context.Context, projectID string, request transfertypes.RemoveRequest) error {
	return s.DestroyProject(transferlib.WithHoldOwner(ctx, request.TransferID), projectID, request.RemoveFiles, false, common.SystemUser)
}
