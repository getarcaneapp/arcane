package cty

import (
	"errors"
	"fmt"
	"iter"
	"maps"

	"github.com/zclconf/go-cty/cty/ctymarks"
)

// WrangleMarksDeep is a specialized variant of [Transform] that is focused
// on interrogating and modifying any marks present throughout a data structure,
// without modifying anything else about the value.
//
// Refer to the [WrangleFunc] documentation for more information. Each of
// the provided functions is called in turn for each mark at each distinct path,
// and the first function that returns a non-nil [ctymarks.WrangleAction] "wins"
// and prevents any later ones from running for a particular mark/path pair.
//
// The implementation makes a best effort to avoid constructing new values
// unless marks have actually changed, to keep this operation relatively cheap
// in the presumed-common case where no marks are present at all.
func (v Value) WrangleMarksDeep(wranglers ...WrangleFunc) (Value, error) {
	// This function is implemented in this package, rather than in the
	// separate "ctymarks", so that it can intrude into the unexported
	// internal details of [Value] to minimize overhead when no marks
	// are present at all.
	var path Path
	if v.IsKnown() && !v.Type().IsPrimitiveType() && !v.IsNull() {
		// If we have a known, non-null, non-primitive-typed value then we
		// can assume there will be at least a little nesting we need to
		// represent using our path, and so we'll preallocate some capacity
		// which we'll be able to share across all calls that are shallower
		// than this level of nesting.
		path = make(Path, 0, 4)
	}
	topMarks := make(ValueMarks)
	var errs []error
	new := wrangleMarksDeep(v, wranglers, path, topMarks, &errs)
	if new == NilVal {
		new = v // completely unchanged
	}
	var err error
	switch len(errs) {
	case 0:
		// nil err is fine, then
	case 1:
		err = errs[0]
	default:
		err = errors.Join(errs...)
	}
	return new.WithMarks(topMarks), err
}

// wrangleMarksDeep is the main implementation of [WrangleMarksDeep], which
// calls itself recursively to handle nested data structures.
//
// If the returned value is [NilVal] then that means that no changes were
// needed to anything at or beneath that nesting level and so the caller should
// just keep the original value exactly.
//
// Modifies topMarks and errs during traversal to collect (respectively) any
// marks that caused [ctymarks.WrangleExpand] and and errors returned by
// wrangle functions.
func wrangleMarksDeep(v Value, wranglers []WrangleFunc, path Path, topMarks ValueMarks, errs *[]error) Value {
	var givenMarks, newMarks ValueMarks
	makeNewValue := false
	// The following is the same idea as [Value.Unmark], but implemented inline
	// here so that we can skip copying any existing ValueMarks that might
	// already be present, since we know we're not going to try to mutate it.
	if marked, ok := v.v.(marker); ok {
		v = Value{
			ty: v.ty,
			v:  marked.realV,
		}
		givenMarks = marked.marks
	}

	// We call this whenever we know we're returning a new value, to perform
	// a one-time copy of the given marks into a new marks map we can modify
	// and to set a flag to force us to construct a newly-marked value when
	// we return below.
	needNewValue := func() {
		if newMarks == nil && len(givenMarks) != 0 {
			newMarks = make(ValueMarks, len(givenMarks))
			maps.Copy(newMarks, givenMarks)
		}
		makeNewValue = true
	}

	for mark := range givenMarks {
	Wranglers:
		for _, wrangler := range wranglers {
			action, err := wrangler(mark, path)
			if err != nil {
				if len(path) != 0 {
					err = path.NewError(err)
				}
				*errs = append(*errs, err)
			}
			switch action {
			case nil:
				continue Wranglers
			case ctymarks.WrangleKeep:
				break Wranglers
			case ctymarks.WrangleExpand:
				topMarks[mark] = struct{}{}
				break Wranglers
			case ctymarks.WrangleDrop:
				needNewValue()
				delete(newMarks, mark)
				break Wranglers
			default:
				newMark := ctymarks.WrangleReplaceMark(action)
				if newMark == nil {
					// Should not get here because these cases should be
					// exhaustive for all possible WrangleAction values.
					panic(fmt.Sprintf("unhandled WrangleAction %#v", action))
				}
				needNewValue()
				delete(newMarks, mark)
				newMarks[newMark] = struct{}{}
				break Wranglers
			}
		}
	}

	// We're not going to make any further changes to our _direct_ marks
	// after this, so if we didn't already make a copy of the given marks
	// we can now safely alias our original set to reuse when we return.
	// (We might still construct a new value though, if we recurse into
	// a nested value that needs its own changes.)
	if newMarks == nil {
		newMarks = givenMarks // might still be nil if we didn't have any marks on entry
	}

	// Now we'll visit nested values recursively, if any.
	// The cases below intentionally don't cover primitive types, set types,
	// or capsule types, because none of them can possibly have nested marks
	// inside. (For set types in particular, any marks on inner values get
	// aggregated on the top-level set itself during construction.)
	ty := v.Type()
	switch {
	case v.IsNull() || !v.IsKnown():
		// Can't recurse into null or unknown values, regardless of type,
		// so nothing to do here.

	case ty.IsListType() || ty.IsTupleType():
		// These types both have the same internal representation, and we
		// know we're not going to change anything about the type, so we
		// can share the same implementation for both.
		l := v.LengthInt()
		if l == 0 {
			break // nothing to do for an empty container
		}

		// We'll avoid allocating a new slice until we know we're going
		// to make a change.
		var newElems []any // as would appear in Value.v for all three of these types
		for i, innerV := range replaceKWithIdx(v.Elements()) {
			path := append(path, IndexStep{Key: NumberIntVal(int64(i))})
			newInnerV := wrangleMarksDeep(innerV, wranglers, path, topMarks, errs)
			if newInnerV != NilVal {
				needNewValue()
				if newElems == nil {
					// If this is the first change we've found then we need to
					// allocate our new elems array and retroactively copy
					// anything we previously skipped because it was unchanged.
					newElems = make([]any, i, l)
					copy(newElems, v.v.([]any))
				}
				newElems = append(newElems, newInnerV.v)
			} else if newElems != nil {
				// Once we've started building a new value we need to append
				// everything to it whether it's changed or not, but we can
				// reuse the unchanged element's internal value.
				newElems = append(newElems, innerV.v)
			}
		}
		if newElems != nil {
			// if we built a new array of elements then it should replace
			// the one from our input value.
			v.v = newElems
		}

	case ty.IsMapType() || ty.IsObjectType():
		// These types both have the same internal representation, and we
		// know we're not going to change anything about the type, so we
		// can share the same implementation for both.
		l := v.LengthInt()
		if l == 0 {
			break // nothing to do for an empty container
		}

		// We'll avoid allocating a new map until we know we're going to
		// make a change.
		var newElems map[string]any
		for keyV, innerV := range v.Elements() {
			var pathStep PathStep
			if ty.IsObjectType() {
				pathStep = GetAttrStep{Name: keyV.AsString()}
			} else {
				pathStep = IndexStep{Key: keyV}
			}
			path := append(path, pathStep)
			newInnerV := wrangleMarksDeep(innerV, wranglers, path, topMarks, errs)
			if newInnerV != NilVal {
				needNewValue()
				if newElems == nil {
					// If this is the first change we've found then we need to
					// allocate our new elems map and retroactively copy
					// everything from the original map before we overwrite
					// the elements that need to change.
					newElems = make(map[string]any, l)
					maps.Copy(newElems, v.v.(map[string]any))
				}
				newElems[keyV.AsString()] = newInnerV.v
			}
		}
		if newElems != nil {
			// if we built a new map of elements then it should replace
			// the one from our input value.
			v.v = newElems
		}
	}

	if !makeNewValue {
		// We didn't make any changes to the marks, so we don't need to
		// construct a new value.
		return NilVal
	}
	return v.WithMarks(newMarks)
}

// WrangleFunc is the signature of a callback function used to visit a
// particular mark associated with a particular path within a value.
//
// [Path] values passed to successive calls to a [WrangleFunc] may share a
// backing array, and so if the function wishes to retain a particular path
// after it returns it must use [Path.Copy] to produce a copy in an unaliased
// backing array.
//
// A function of this type must decide what change to make, if any, to the
// presence of this mark at this location. Returning nil means to take no
// action at all and to potentially allow other later functions of this
// type to decide what to do instead.
//
// If the function returns an error then [Value.WrangleMarksDeep] collects it
// and any other errors returned during traversal, automatically wraps in a
// [cty.PathError] if not at the root, and returns an [`errors.Join`] of all
// of the errors if there are more than one. The indicated action is still taken
// and continued "wrangling" occurs as normal to visit other marks and other
// paths.
//
// [cty.Value.WrangleMarksDeep], and this callback signature used with it,
// are together designed with some assumptions that don't always hold but
// have been common enough to make it seem worth supporting with a first-class
// feature:
//
//   - All of the different marks on any specific value are orthogonal to one
//     another, and so it's possible to decide an action for each one in
//     isolation.
//   - Marks within a data structure are orthogonal to the specific values they
//     are associated with, and so it's possible to decide an action without
//     knowning the value it's associated with. (Though it's possible in
//     principle to use the given path to retrieve more information when needed,
//     at the expense of some additional traversal overhead.)
//   - Most values have no marks at all and when marks are present there are
//     relatively few of them, and so it's worth making some extra effort to
//     handle the no-marks case cheaply even if it makes the marks-present case
//     a little more expensive.
//
// If any of these assumptions don't apply to your situation then this may not
// be an appropriate solution. [cty.Transform] or [cty.TransformWithTransformer]
// might serve as a more general alternative if you need more control.
type WrangleFunc func(mark any, path Path) (ctymarks.WrangleAction, error)

func replaceKWithIdx[K any, V any](in iter.Seq2[K, V]) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		i := 0
		for _, v := range in {
			if !yield(i, v) {
				break
			}
			i++
		}
	}
}
