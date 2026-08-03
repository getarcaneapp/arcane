package swarm

import (
	"math"
	"os"
	"sort"
	"strings"

	"emperror.dev/errors"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
)

func convertServiceMountsInternal(
	volumes []composegotypes.ServiceVolumeConfig,
	ns namespace,
	projectVolumes composegotypes.Volumes,
	stackLabels map[string]string,
) ([]mount.Mount, error) {
	if len(volumes) == 0 {
		return nil, nil
	}
	result := make([]mount.Mount, 0, len(volumes))
	for _, vol := range volumes {
		mountType := mapVolumeTypeInternal(vol.Type)
		source, projectVolume, err := resolveServiceMountSourceInternal(vol.Source, mountType, ns, projectVolumes)
		if err != nil {
			return nil, err
		}
		entry := mount.Mount{
			Type:        mountType,
			Source:      source,
			Target:      vol.Target,
			ReadOnly:    vol.ReadOnly,
			Consistency: mount.Consistency(vol.Consistency),
		}
		if vol.Bind != nil {
			entry.BindOptions = &mount.BindOptions{
				Propagation:      mount.Propagation(vol.Bind.Propagation),
				CreateMountpoint: bool(vol.Bind.CreateHostPath),
			}
		}
		if vol.Volume != nil {
			entry.VolumeOptions = &mount.VolumeOptions{
				NoCopy:  vol.Volume.NoCopy,
				Labels:  vol.Volume.Labels,
				Subpath: vol.Volume.Subpath,
			}
		}
		if mountType == mount.TypeVolume && vol.Source != "" {
			if entry.VolumeOptions == nil {
				entry.VolumeOptions = &mount.VolumeOptions{}
			}
			if !projectVolume.External {
				entry.VolumeOptions.Labels = mergeLabelsInternal(entry.VolumeOptions.Labels, mergeLabelsInternal(projectVolume.Labels, stackLabels))
				entry.VolumeOptions.DriverConfig = &mount.Driver{
					Name:    projectVolume.Driver,
					Options: projectVolume.DriverOpts,
				}
			}
		}
		if vol.Tmpfs != nil {
			entry.TmpfsOptions = &mount.TmpfsOptions{
				SizeBytes: int64(vol.Tmpfs.Size),
				Mode:      os.FileMode(vol.Tmpfs.Mode),
			}
		}
		if vol.Image != nil {
			entry.ImageOptions = &mount.ImageOptions{Subpath: vol.Image.SubPath}
		}
		result = append(result, entry)
	}
	return result, nil
}

func resolveServiceMountSourceInternal(
	source string,
	mountType mount.Type,
	ns namespace,
	projectVolumes composegotypes.Volumes,
) (string, composegotypes.VolumeConfig, error) {
	if source == "" {
		return "", composegotypes.VolumeConfig{}, nil
	}
	if mountType != mount.TypeVolume && (mountType != mount.TypeCluster || strings.HasPrefix(source, "group:")) {
		return source, composegotypes.VolumeConfig{}, nil
	}

	projectVolume, ok := projectVolumes[source]
	if !ok {
		return "", composegotypes.VolumeConfig{}, errors.Errorf("undefined volume %q", source)
	}
	return ns.Resolve(source, projectVolume.Name), projectVolume, nil
}

type resolvedFileReferenceInternal struct {
	meta   resourceMeta
	target string
	uid    string
	gid    string
	mode   os.FileMode
}

func resolveServiceFileReferencesInternal(
	references []composegotypes.FileReferenceConfig,
	metaByKey map[string]resourceMeta,
	resourceType string,
) ([]resolvedFileReferenceInternal, error) {
	resolved := make([]resolvedFileReferenceInternal, 0, len(references))
	for _, reference := range references {
		meta, ok := metaByKey[reference.Source]
		if !ok {
			return nil, errors.Errorf("undefined %s %q", resourceType, reference.Source)
		}
		target := reference.Target
		if target == "" {
			target = reference.Source
		}
		mode, err := fileModeOrDefaultInternal(reference.Mode)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid %s %q mode", resourceType, reference.Source)
		}
		resolved = append(resolved, resolvedFileReferenceInternal{
			meta:   meta,
			target: target,
			uid:    utils.StringOrDefault(reference.UID, "0"),
			gid:    utils.StringOrDefault(reference.GID, "0"),
			mode:   mode,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].meta.Name < resolved[j].meta.Name
	})
	return resolved, nil
}

func convertServiceSecretRefsInternal(secrets []composegotypes.ServiceSecretConfig, secretMetaByKey map[string]resourceMeta) ([]*swarm.SecretReference, error) {
	references := make([]composegotypes.FileReferenceConfig, len(secrets))
	for i, secret := range secrets {
		references[i] = composegotypes.FileReferenceConfig(secret)
	}
	resolved, err := resolveServiceFileReferencesInternal(references, secretMetaByKey, "secret")
	if err != nil {
		return nil, err
	}

	result := make([]*swarm.SecretReference, 0, len(resolved))
	for _, reference := range resolved {
		result = append(result, &swarm.SecretReference{
			SecretID:   reference.meta.ID,
			SecretName: reference.meta.Name,
			File: &swarm.SecretReferenceFileTarget{
				Name: reference.target,
				UID:  reference.uid,
				GID:  reference.gid,
				Mode: reference.mode,
			},
		})
	}
	return result, nil
}

func convertServiceConfigRefsInternal(configs []composegotypes.ServiceConfigObjConfig, configMetaByKey map[string]resourceMeta) ([]*swarm.ConfigReference, error) {
	references := make([]composegotypes.FileReferenceConfig, len(configs))
	for i, config := range configs {
		references[i] = composegotypes.FileReferenceConfig(config)
	}
	resolved, err := resolveServiceFileReferencesInternal(references, configMetaByKey, "config")
	if err != nil {
		return nil, err
	}

	result := make([]*swarm.ConfigReference, 0, len(resolved))
	for _, reference := range resolved {
		result = append(result, &swarm.ConfigReference{
			ConfigID:   reference.meta.ID,
			ConfigName: reference.meta.Name,
			File: &swarm.ConfigReferenceFileTarget{
				Name: reference.target,
				UID:  reference.uid,
				GID:  reference.gid,
				Mode: reference.mode,
			},
		})
	}
	return result, nil
}

func resolveFileObjectContentInternal(
	fileConfig composegotypes.FileObjectConfig,
	workingDir string,
	environment composegotypes.Mapping,
) ([]byte, error) {
	if fileConfig.Content != "" {
		return []byte(fileConfig.Content), nil
	}
	if fileConfig.Environment != "" {
		value, ok := environment.Resolve(fileConfig.Environment)
		if !ok {
			return nil, errors.Errorf("environment variable %s not set", fileConfig.Environment)
		}
		return []byte(value), nil
	}
	if fileConfig.File != "" {
		path, err := projects.ResolvePathWithinDir(workingDir, fileConfig.File)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
	return nil, errors.New("config or secret content is required")
}

func fileModeOrDefaultInternal(mode *composegotypes.FileMode) (os.FileMode, error) {
	if mode == nil {
		return 0444, nil
	}
	if *mode < 0 || *mode > composegotypes.FileMode(math.MaxUint32) {
		return 0, errors.Errorf("file mode %d is outside the uint32 range", *mode)
	}
	return os.FileMode(uint32(*mode)), nil
}

func mapVolumeTypeInternal(value string) mount.Type {
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
