package pagination

import (
	"cmp"

	"github.com/getarcaneapp/arcane/types/v2/base"
	kit "go.getarcane.app/kit/pkg"
)

type Environmental interface {
	Environment() base.EnvironmentRef
}

type EnvironmentPage[T any] struct {
	Environment    base.EnvironmentRef
	Items          []T
	TotalAvailable int64
}

func WithEnvironment[T Environmental](c Config[T]) Config[T] {
	byEnvironment := func(a, b T) int {
		left, right := a.Environment(), b.Environment()
		return cmp.Or(cmp.Compare(left.Name, right.Name), cmp.Compare(left.ID, right.ID))
	}
	if len(c.SortBindings) == 0 {
		c.SortBindings = []SortBinding[T]{{Key: "environment", Fn: byEnvironment, DescFn: reverSortFn(byEnvironment)}}
		return c
	}

	first := c.SortBindings[0]
	c.SortBindings = append(append([]SortBinding[T]{{
		Key:    first.Key,
		Fn:     chainSortFn(first.Fn, byEnvironment),
		DescFn: kit.Ternary[SortOption[T]](first.DescFn != nil, chainSortFn(first.DescFn, byEnvironment), nil),
	}}, c.SortBindings[1:]...), SortBinding[T]{
		Key:    "environment",
		Fn:     chainSortFn(byEnvironment, first.Fn),
		DescFn: chainSortFn(reverSortFn(byEnvironment), first.Fn),
	})
	return c
}

func MergeEnvironmentPages[T Environmental, PT interface {
	*T
	SetEnvironment(base.EnvironmentRef)
}](config Config[T], pages []EnvironmentPage[T], params QueryParams) ([]T, Response) {
	all := make([]T, 0)
	var totalAvailable int64
	for _, page := range pages {
		for _, item := range page.Items {
			PT(&item).SetEnvironment(page.Environment)
			all = append(all, item)
		}
		totalAvailable += page.TotalAvailable
	}
	if params.Limit > 0 && params.Start >= len(all) {
		params.Start = max(len(all)-1, 0) / params.Limit * params.Limit
	}
	return WithEnvironment(config).OrderAndPaginate(all, params), BuildResponse(int64(len(all)), totalAvailable, params)
}
