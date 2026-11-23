package swarm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"

	"kit.getarcane.app/acfs"

	"getarcane.dev/arcane/v3/pkg/projects"
)

type (
	resolvedFileReference struct {
		meta   resourceMeta
		target string
		uid    string
		gid    string
		mode   os.FileMode
	}
)

func convertServiceMounts(
	volumes []composeTypes.ServiceVolumeConfig,
	ns namespace,
	projectVolumes composeTypes.Volumes,
	stackLabels map[string]string,
) ([]mount.Mount, error) {
	if len(volumes) == 0 {
		return nil, nil
	}
	result := make([]mount.Mount, 0, len(volumes))
	for _, vol := range volumes {
		mountType := mapVolumeType(vol.Type)
		source, projectVolume, err := resolveServiceMountSource(vol.Source, mountType, ns, projectVolumes)
		if err != nil {
			return nil, err
		}
		var bindOptions *mount.BindOptions
		if vol.Bind != nil {
			bindOptions = &mount.BindOptions{
				Propagation:      mount.Propagation(vol.Bind.Propagation),
				CreateMountpoint: bool(vol.Bind.CreateHostPath),
			}
		}
		var volumeOptions *mount.VolumeOptions
		if vol.Volume != nil || (mountType == mount.TypeVolume && vol.Source != "") {
			var noCopy bool
			var labels map[string]string
			var subpath string
			if vol.Volume != nil {
				noCopy, labels, subpath = vol.Volume.NoCopy, vol.Volume.Labels, vol.Volume.Subpath
			}
			var driverConfig *mount.Driver
			if mountType == mount.TypeVolume && vol.Source != "" && !projectVolume.External {
				labels = mergeLabels(labels, mergeLabels(projectVolume.Labels, stackLabels))
				driverConfig = &mount.Driver{
					Name:    projectVolume.Driver,
					Options: projectVolume.DriverOpts,
				}
			}
			volumeOptions = &mount.VolumeOptions{
				NoCopy:       noCopy,
				Labels:       labels,
				Subpath:      subpath,
				DriverConfig: driverConfig,
			}
		}
		var tmpfsOptions *mount.TmpfsOptions
		if vol.Tmpfs != nil {
			tmpfsOptions = &mount.TmpfsOptions{
				SizeBytes: int64(vol.Tmpfs.Size),
				Mode:      os.FileMode(vol.Tmpfs.Mode),
			}
		}
		var imageOptions *mount.ImageOptions
		if vol.Image != nil {
			imageOptions = &mount.ImageOptions{Subpath: vol.Image.SubPath}
		}
		result = append(result, mount.Mount{
			Type:          mountType,
			Source:        source,
			Target:        vol.Target,
			ReadOnly:      vol.ReadOnly,
			Consistency:   mount.Consistency(vol.Consistency),
			BindOptions:   bindOptions,
			VolumeOptions: volumeOptions,
			TmpfsOptions:  tmpfsOptions,
			ImageOptions:  imageOptions,
		})
	}
	return result, nil
}

func resolveServiceMountSource(
	source string,
	mountType mount.Type,
	ns namespace,
	projectVolumes composeTypes.Volumes,
) (string, composeTypes.VolumeConfig, error) {
	if source == "" {
		return "", composeTypes.VolumeConfig{}, nil
	}
	if mountType != mount.TypeVolume && (mountType != mount.TypeCluster || strings.HasPrefix(source, "group:")) {
		return source, composeTypes.VolumeConfig{}, nil
	}

	projectVolume, ok := projectVolumes[source]
	if !ok {
		return "", composeTypes.VolumeConfig{}, fmt.Errorf("undefined volume %q", source)
	}
	return ns.Resolve(source, projectVolume.Name), projectVolume, nil
}

func resolveServiceFileReferences(
	references []composeTypes.FileReferenceConfig,
	metaByKey map[string]resourceMeta,
	resourceType string,
) ([]resolvedFileReference, error) {
	resolved := make([]resolvedFileReference, 0, len(references))
	for _, reference := range references {
		meta, ok := metaByKey[reference.Source]
		if !ok {
			return nil, fmt.Errorf("undefined %s %q", resourceType, reference.Source)
		}
		target := cmp.Or(reference.Target, reference.Source)
		mode, err := fileModeOrDefault(reference.Mode)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q mode: %w", resourceType, reference.Source, err)
		}
		resolved = append(resolved, resolvedFileReference{
			meta:   meta,
			target: target,
			uid:    cmp.Or(strings.TrimSpace(reference.UID), "0"),
			gid:    cmp.Or(strings.TrimSpace(reference.GID), "0"),
			mode:   mode,
		})
	}
	slices.SortFunc(resolved, func(a, b resolvedFileReference) int {
		return strings.Compare(a.meta.Name, b.meta.Name)
	})
	return resolved, nil
}

func convertServiceFileRefs[S composeTypes.ServiceSecretConfig | composeTypes.ServiceConfigObjConfig, R any](
	items []S,
	metaByKey map[string]resourceMeta,
	kind string,
	build func(resolvedFileReference) *R,
) ([]*R, error) {
	references := make([]composeTypes.FileReferenceConfig, len(items))
	for i, item := range items {
		references[i] = composeTypes.FileReferenceConfig(item)
	}
	resolved, err := resolveServiceFileReferences(references, metaByKey, kind)
	if err != nil {
		return nil, err
	}

	result := make([]*R, 0, len(resolved))
	for _, reference := range resolved {
		result = append(result, build(reference))
	}
	return result, nil
}

func convertServiceSecretRefs(secrets []composeTypes.ServiceSecretConfig, secretMetaByKey map[string]resourceMeta) ([]*swarm.SecretReference, error) {
	return convertServiceFileRefs(secrets, secretMetaByKey, "secret", func(reference resolvedFileReference) *swarm.SecretReference {
		return &swarm.SecretReference{
			SecretID:   reference.meta.ID,
			SecretName: reference.meta.Name,
			File: &swarm.SecretReferenceFileTarget{
				Name: reference.target,
				UID:  reference.uid,
				GID:  reference.gid,
				Mode: reference.mode,
			},
		}
	})
}

func convertServiceConfigRefs(configs []composeTypes.ServiceConfigObjConfig, configMetaByKey map[string]resourceMeta) ([]*swarm.ConfigReference, error) {
	return convertServiceFileRefs(configs, configMetaByKey, "config", func(reference resolvedFileReference) *swarm.ConfigReference {
		return &swarm.ConfigReference{
			ConfigID:   reference.meta.ID,
			ConfigName: reference.meta.Name,
			File: &swarm.ConfigReferenceFileTarget{
				Name: reference.target,
				UID:  reference.uid,
				GID:  reference.gid,
				Mode: reference.mode,
			},
		}
	})
}

func resolveFileObjectContent(
	ctx context.Context,
	fileConfig composeTypes.FileObjectConfig,
	workingDir string,
	environment composeTypes.Mapping,
) ([]byte, error) {
	if fileConfig.Content != "" {
		return []byte(fileConfig.Content), nil
	}
	if fileConfig.Environment != "" {
		value, ok := environment.Resolve(fileConfig.Environment)
		if !ok {
			return nil, fmt.Errorf("environment variable %s not set", fileConfig.Environment)
		}
		return []byte(value), nil
	}
	if fileConfig.File != "" {
		path, err := projects.ResolvePathWithinDir(workingDir, fileConfig.File)
		if err != nil {
			return nil, err
		}
		return acfs.Host{}.ReadFile(ctx, path)
	}
	return nil, errors.New("config or secret content is required")
}

func fileModeOrDefault(mode *composeTypes.FileMode) (os.FileMode, error) {
	if mode == nil {
		return 0o444, nil
	}
	if *mode < 0 || *mode > composeTypes.FileMode(math.MaxUint32) {
		return 0, fmt.Errorf("file mode %d is outside the uint32 range", *mode)
	}
	return os.FileMode(uint32(*mode)), nil
}

func mapVolumeType(value string) mount.Type {
	switch strings.ToLower(value) {
	case "bind":
		return mount.TypeBind
	case "tmpfs":
		return mount.TypeTmpfs
	case "npipe":
		return mount.TypeNamedPipe
	case "cluster":
		return mount.TypeCluster
	case "image":
		return mount.TypeImage
	case "volume", "":
		return mount.TypeVolume
	default:
		return mount.TypeVolume
	}
}
