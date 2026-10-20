package quadlets

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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

var quadletPagination = pagination.Config[podmantypes.QuadletDetail]{
	SearchAccessors: []pagination.SearchAccessor[podmantypes.QuadletDetail]{
		func(quadlet podmantypes.QuadletDetail) (string, error) {
			return quadlet.Name, nil
		},
		func(quadlet podmantypes.QuadletDetail) (string, error) {
			return quadlet.UnitName, nil
		},
		func(quadlet podmantypes.QuadletDetail) (string, error) {
			return string(quadlet.Type), nil
		},
		func(quadlet podmantypes.QuadletDetail) (string, error) {
			return quadlet.Status, nil
		},
		func(quadlet podmantypes.QuadletDetail) (string, error) {
			return quadlet.App, nil
		},
	},
	SortBindings: []pagination.SortBinding[podmantypes.QuadletDetail]{
		{Key: "name", Fn: func(a, b podmantypes.QuadletDetail) int {
			return strings.Compare(a.Name, b.Name)
		}},
		{Key: "type", Fn: func(a, b podmantypes.QuadletDetail) int {
			return strings.Compare(string(a.Type), string(b.Type))
		}},
		{Key: "status", Fn: func(a, b podmantypes.QuadletDetail) int {
			return strings.Compare(a.Status, b.Status)
		}},
		{Key: "unitName", Fn: func(a, b podmantypes.QuadletDetail) int {
			return strings.Compare(a.UnitName, b.UnitName)
		}},
		{Key: "app", Fn: func(a, b podmantypes.QuadletDetail) int {
			return strings.Compare(a.App, b.App)
		}},
	},
}

func (s *Service) ListPaginated(ctx context.Context, filters map[string][]string, params pagination.QueryParams) ([]podmantypes.QuadletDetail, pagination.Response, error) {
	items, err := s.List(ctx, filters)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	result := quadletPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) Aggregate(ctx context.Context, params pagination.QueryParams) (base.Paginated[podmantypes.QuadletDetail], error) {
	collection, err := httpx.CollectEnvironments[base.Paginated[podmantypes.QuadletDetail]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return base.Paginated[podmantypes.QuadletDetail]{}, err
	}

	pages := make([]pagination.EnvironmentPage[podmantypes.QuadletDetail], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[podmantypes.QuadletDetail]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(quadletPagination, pages, params)

	return base.Paginated[podmantypes.QuadletDetail]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
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
	report, err := rt.Client.QuadletRemove(ctx, name, podman.QuadletRemoveOptions{Force: true})
	if err != nil {
		return podmantypes.QuadletRemoveReport{}, err
	}
	if len(report.Errors) > 0 {
		failures := make([]string, 0, len(report.Errors))
		for _, quadlet := range slices.Sorted(maps.Keys(report.Errors)) {
			failures = append(failures, quadlet+": "+report.Errors[quadlet])
		}
		return report, common.Classify(common.ErrConflict, fmt.Errorf("remove quadlet %s: %s", name, strings.Join(failures, "; ")))
	}
	return report, nil
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

	index := slices.IndexFunc(entries, func(entry podmantypes.Quadlet) bool {
		return entry.Name == name
	})
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
