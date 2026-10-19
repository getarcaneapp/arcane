package quadlets

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

var installableTypes = []podmantypes.QuadletType{
	podmantypes.QuadletTypeContainer,
	podmantypes.QuadletTypePod,
	podmantypes.QuadletTypeKube,
	podmantypes.QuadletTypeNetwork,
	podmantypes.QuadletTypeVolume,
	podmantypes.QuadletTypeImage,
	podmantypes.QuadletTypeBuild,
	podmantypes.QuadletTypeArtifact,
}

type Runtime struct {
	Client        *podman.Client
	Systemd       podman.Systemd
	AppsSupported bool
}

type Service struct {
	runtime func(context.Context) (Runtime, error)
}

func NewService(runtime func(context.Context) (Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) List(ctx context.Context, filters map[string][]string) ([]podmantypes.QuadletDetail, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := rt.Client.QuadletList(ctx, filters)
	if err != nil {
		return nil, err
	}

	out := make([]podmantypes.QuadletDetail, 0, len(entries))
	for _, entry := range entries {
		out = append(out, describe(entry))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, name string) (podmantypes.QuadletDetail, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.QuadletDetail{}, err
	}

	entry, err := find(ctx, rt.Client, name)
	if err != nil {
		return podmantypes.QuadletDetail{}, err
	}

	detail := describe(entry)
	containers, err := rt.Client.ContainerList(ctx, []string{podmantypes.SystemdUnitLabel + "=" + detail.UnitName}, nil)
	if err != nil {
		return podmantypes.QuadletDetail{}, err
	}
	unitStatus, unitErr := rt.Systemd.UnitStatus(ctx, detail.UnitName)
	if unitErr != nil {
		slog.DebugContext(ctx, "Quadlet unit status unavailable", "quadlet", name, "unit", detail.UnitName, "error", unitErr)
	}

	return podmantypes.QuadletDetail{
		Name:       detail.Name,
		UnitName:   detail.UnitName,
		Path:       detail.Path,
		Status:     detail.Status,
		App:        detail.App,
		Type:       detail.Type,
		Template:   detail.Template,
		UnitStatus: kit.Ternary(unitErr == nil, &unitStatus, nil),
		Containers: containers,
	}, nil
}

func (s *Service) File(ctx context.Context, name string) (podmantypes.QuadletFile, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.QuadletFile{}, err
	}

	content, err := rt.Client.QuadletFile(ctx, name)
	if err != nil {
		return podmantypes.QuadletFile{}, err
	}
	return podmantypes.QuadletFile{Name: name, Content: content}, nil
}

func (s *Service) Install(ctx context.Context, req podmantypes.QuadletInstallRequest) (podmantypes.QuadletInstallReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.QuadletInstallReport{}, err
	}

	name := strings.TrimSpace(req.Name)
	quadletType := podmantypes.QuadletType(strings.TrimPrefix(path.Ext(name), "."))
	if strings.TrimSuffix(name, path.Ext(name)) == "" || !slices.Contains(installableTypes, quadletType) {
		return podmantypes.QuadletInstallReport{}, common.Classify(common.ErrBadRequest, fmt.Errorf("unsupported quadlet file %q", name))
	}
	if req.App != "" && !rt.AppsSupported {
		return podmantypes.QuadletInstallReport{}, common.Classify(common.ErrBadRequest, errors.New("quadlet applications require Podman 6.1 or later"))
	}

	return rt.Client.QuadletInstall(ctx, podman.QuadletInstallOptions{
		File: podmantypes.QuadletFile{
			Name:    name,
			Content: req.Content,
		},
		ExtraFiles:    req.ExtraFiles,
		Application:   req.App,
		AppsSupported: rt.AppsSupported,
	})
}

func (s *Service) Update(ctx context.Context, name string, req podmantypes.QuadletUpdateRequest) (podmantypes.QuadletInstallReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.QuadletInstallReport{}, err
	}

	entry, err := find(ctx, rt.Client, name)
	if err != nil {
		return podmantypes.QuadletInstallReport{}, err
	}

	return rt.Client.QuadletInstall(ctx, podman.QuadletInstallOptions{
		File: podmantypes.QuadletFile{
			Name:    entry.Name,
			Content: req.Content,
		},
		ExtraFiles:    req.ExtraFiles,
		Replace:       true,
		Application:   entry.App,
		AppsSupported: rt.AppsSupported,
	})
}

func (s *Service) Delete(ctx context.Context, name string) (podmantypes.QuadletRemoveReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.QuadletRemoveReport{}, err
	}
	return rt.Client.QuadletRemove(ctx, name, podman.QuadletRemoveOptions{Force: true})
}

func (s *Service) Lifecycle(ctx context.Context, name string, action func(podman.Systemd, context.Context, string) error) (podmantypes.UnitStatus, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return podmantypes.UnitStatus{}, err
	}

	entry, err := find(ctx, rt.Client, name)
	if err != nil {
		return podmantypes.UnitStatus{}, err
	}

	detail := describe(entry)
	if detail.Template {
		return podmantypes.UnitStatus{}, common.Classify(common.ErrConflict, fmt.Errorf("quadlet %s is a template and has no lifecycle actions", name))
	}
	if actionErr := action(rt.Systemd, ctx, detail.UnitName); actionErr != nil {
		return podmantypes.UnitStatus{}, actionErr
	}
	return rt.Systemd.UnitStatus(ctx, detail.UnitName)
}

func find(ctx context.Context, client *podman.Client, name string) (podmantypes.Quadlet, error) {
	entries, err := client.QuadletList(ctx, map[string][]string{"name": {name}})
	if err != nil {
		return podmantypes.Quadlet{}, err
	}

	index := slices.IndexFunc(entries, func(entry podmantypes.Quadlet) bool { return entry.Name == name })
	if index < 0 {
		return podmantypes.Quadlet{}, common.Classify(common.ErrNotFound, fmt.Errorf("quadlet %s not found", name))
	}
	return entries[index], nil
}

func describe(entry podmantypes.Quadlet) podmantypes.QuadletDetail {
	ext := path.Ext(entry.Name)
	stem := strings.TrimSuffix(entry.Name, ext)
	quadletType := podmantypes.QuadletType(strings.TrimPrefix(ext, "."))

	return podmantypes.QuadletDetail{
		Name: entry.Name,
		UnitName: cmp.Or(entry.UnitName, func() string {
			switch quadletType {
			case podmantypes.QuadletTypeContainer, podmantypes.QuadletTypeKube:
				return stem + ".service"
			case podmantypes.QuadletTypePod:
				return stem + "-pod.service"
			default:
				return stem + "-" + string(quadletType) + ".service"
			}
		}()),
		Path:     entry.Path,
		Status:   entry.Status,
		App:      entry.App,
		Type:     quadletType,
		Template: strings.HasSuffix(stem, "@") || strings.EqualFold(strings.TrimSpace(entry.Status), "loaded template"),
	}
}
