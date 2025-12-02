package cty

import (
	"fmt"
	"iter"
	"strings"

	"github.com/zclconf/go-cty/cty/ctymarks"
)

// marker is an internal wrapper type used to add special "marks" to values.
//
// A "mark" is an annotation that can be used to represent additional
// characteristics of values that propagate through operation methods to
// result values. However, a marked value cannot be used with integration
// methods normally associated with its type, in order to ensure that
// calling applications don't inadvertently drop marks as they round-trip
// values out of cty and back in again.
//
// Marked values are created only explicitly by the calling application, so
// an application that never marks a value does not need to worry about
// encountering marked values.
type marker struct {
	realV any
	marks ValueMarks
}

// ValueMarks is a map, representing a set, of "mark" values associated with
// a Value. See Value.Mark for more information on the usage of mark values.
type ValueMarks map[any]struct{}

// NewValueMarks constructs a new ValueMarks set with the given mark values.
//
// If any of the arguments are already ValueMarks values then they'll be merged
// into the result, rather than used directly as individual marks.
func NewValueMarks(marks ...any) ValueMarks {
	if len(marks) == 0 {
		return nil
	}
	ret := make(ValueMarks, len(marks))
	for _, v := range marks {
		if vm, ok := v.(ValueMarks); ok {
			// Constructing a new ValueMarks with an existing ValueMarks
			// implements a merge operation. (This can cause our result to
			// have a larger size than we expected, but that's okay.)
			for v := range vm {
				ret[v] = struct{}{}
			}
			continue
		}
		ret[v] = struct{}{}
	}
	if len(ret) == 0 {
		// If we were merging ValueMarks values together and they were all
		// empty then we'll avoid returning a zero-length map and return a
		// nil instead, as is conventional.
		return nil
	}
	return ret
}

// Equal returns true if the receiver and the given ValueMarks both contain
// the same marks.
func (m ValueMarks) Equal(o ValueMarks) bool {
	if len(m) != len(o) {
		return false
	}
	for v := range m {
		if _, ok := o[v]; !ok {
			return false
		}
	}
	return true
}

func (m ValueMarks) GoString() string {
	var s strings.Builder
	s.WriteString("cty.NewValueMarks(")
	i := 0
	for mv := range m {
		if i != 0 {
			s.WriteString(", ")
		}
		s.WriteString(fmt.Sprintf("%#v", mv))
		i++
	}
	s.WriteString(")")
	return s.String()
}

// PathValueMarks is a structure that enables tracking marks
// and the paths where they are located in one type
type PathValueMarks struct {
	Path  Path
	Marks ValueMarks
}

func (p PathValueMarks) Equal(o PathValueMarks) bool {
	if !p.Path.Equals(o.Path) {
		return false
	}
	if !p.Marks.Equal(o.Marks) {
		return false
	}
	return true
}

// IsMarked returns true if and only if the receiving value carries at least
// one mark. A marked value cannot be used directly with integration methods
// without explicitly unmarking it (and retrieving the markings) first.
func (val Value) IsMarked() bool {
	_, ok := val.v.(marker)
	return ok
}

// HasMark returns true if and only if the receiving value has the given mark.
func (val Value) HasMark(mark any) bool {
	if mr, ok := val.v.(marker); ok {
		_, ok := mr.marks[mark]
		return ok
	}
	return false
}

// HasMarkDeep is like [HasMark] but also searches any values nested inside
// the given value.
func (val Value) HasMarkDeep(mark any) bool {
	for _, v := range DeepValues(val) {
		if v.HasMark(mark) {
			return true
		}
	}
	return false
}

// ValueMarksOfType returns an iterable sequence of any marks directly
// associated with the given value that can be type-asserted to the given
// type.
func ValueMarksOfType[T any](v Value) iter.Seq[T] {
	return func(yield func(T) bool) {
		yieldValueMarksOfType(v, yield)
	}
}

// ValueMarksOfTypeDeep is like [ValueMarksOfType] but also visits any values
// nested inside the given value.
//
// The same value may be produced multiple times if multiple nested values are
// marked with it.
func ValueMarksOfTypeDeep[T any](v Value) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range DeepValues(v) {
			if !yieldValueMarksOfType(v, yield) {
				break
			}
		}
	}
}

func yieldValueMarksOfType[T any](v Value, yield func(T) bool) bool {
	mr, ok := v.v.(marker)
	if !ok {
		return true
	}
	for mark := range mr.marks {
		if v, ok := mark.(T); ok {
			if !yield(v) {
				return false
			}
		}
	}
	return true
}

// ContainsMarked returns true if the receiving value or any value within it
// is marked.
//
// This operation is relatively expensive. If you only need a shallow result,
// use IsMarked instead.
func (val Value) ContainsMarked() bool {
	for _, v := range DeepValues(val) {
		if v.IsMarked() {
			return true
		}
	}
	return false
}

func (val Value) assertUnmarked() {
	if val.IsMarked() {
		panic("value is marked, so must be unmarked first")
	}
}

// Marks returns a map (representing a set) of all of the mark values
// associated with the receiving value, without changing the marks. Returns nil
// if the value is not marked at all.
func (val Value) Marks() ValueMarks {
	if mr, ok := val.v.(marker); ok {
		// copy so that the caller can't mutate our internals
		ret := make(ValueMarks, len(mr.marks))
		for k, v := range mr.marks {
			ret[k] = v
		}
		return ret
	}
	return nil
}

// HasSameMarks returns true if an only if the receiver and the given other
// value have identical marks.
func (val Value) HasSameMarks(other Value) bool {
	vm, vmOK := val.v.(marker)
	om, omOK := other.v.(marker)
	if vmOK != omOK {
		return false
	}
	if vmOK {
		return vm.marks.Equal(om.marks)
	}
	return true
}

// Mark returns a new value that as the same type and underlying value as
// the receiver but that also carries the given value as a "mark".
//
// Marks are used to carry additional application-specific characteristics
// associated with values. A marked value can be used with operation methods,
// in which case the marks are propagated to the operation results. A marked
// value _cannot_ be used with integration methods, so callers of those
// must derive an unmarked value using Unmark (and thus explicitly handle
// the markings) before calling the integration methods.
//
// The mark value can be any value that would be valid to use as a map key.
// The mark value should be of a named type in order to use the type itself
// as a namespace for markings. That type can be unexported if desired, in
// order to ensure that the mark can only be handled through the defining
// package's own functions.
//
// An application that never calls this method does not need to worry about
// handling marked values.
func (val Value) Mark(mark any) Value {
	if _, ok := mark.(ValueMarks); ok {
		panic("cannot call Value.Mark with a ValueMarks value (use WithMarks instead)")
	}
	var newMarker marker
	newMarker.realV = val.v
	if mr, ok := val.v.(marker); ok {
		// It's already a marker, so we'll retain existing marks.
		newMarker.marks = make(ValueMarks, len(mr.marks)+1)
		for k, v := range mr.marks {
			newMarker.marks[k] = v
		}
		// unwrap the inner marked value, so we don't get multiple layers
		// of marking.
		newMarker.realV = mr.realV
	} else {
		// It's not a marker yet, so we're creating the first mark.
		newMarker.marks = make(ValueMarks, 1)
	}
	newMarker.marks[mark] = struct{}{}
	return Value{
		ty: val.ty,
		v:  newMarker,
	}
}

type applyPathValueMarksTransformer struct {
	pvm []PathValueMarks
}

func (t *applyPathValueMarksTransformer) Enter(p Path, v Value) (Value, error) {
	return v, nil
}

func (t *applyPathValueMarksTransformer) Exit(p Path, v Value) (Value, error) {
	for _, path := range t.pvm {
		if p.Equals(path.Path) {
			return v.WithMarks(path.Marks), nil
		}
	}
	return v, nil
}

// MarkWithPaths accepts a slice of PathValueMarks to apply
// markers to particular paths and returns the marked
// Value.
func (val Value) MarkWithPaths(pvm []PathValueMarks) Value {
	if len(pvm) == 0 {
		// If we have no marks to apply then there's nothing to do, so we'll
		// just return the same value rather than wastefully rebuilding it.
		return val
	}
	ret, _ := TransformWithTransformer(val, &applyPathValueMarksTransformer{pvm})
	return ret
}

// Unmark separates the marks of the receiving value from the value itself,
// removing a new unmarked value and a map (representing a set) of the marks.
//
// If the receiver isn't marked, Unmark returns it verbatim along with a nil
// map of marks.
func (val Value) Unmark() (Value, ValueMarks) {
	if !val.IsMarked() {
		return val, nil
	}
	mr := val.v.(marker)
	marks := val.Marks() // copy so that the caller can't mutate our internals
	return Value{
		ty: val.ty,
		v:  mr.realV,
	}, marks
}

// UnmarkDeep is similar to Unmark, but it works with an entire nested structure
// rather than just the given value directly.
//
// The result is guaranteed to contain no nested values that are marked, and
// the returned marks set includes the superset of all of the marks encountered
// during the operation.
func (val Value) UnmarkDeep() (Value, ValueMarks) {
	retMarks := make(ValueMarks)
	retVal, _ := val.WrangleMarksDeep(func(mark any, path Path) (ctymarks.WrangleAction, error) {
		retMarks[mark] = struct{}{}
		return ctymarks.WrangleDrop, nil
	})
	return retVal, retMarks
}

// UnmarkDeepWithPaths is like UnmarkDeep, except it returns a slice
// of PathValueMarks rather than a superset of all marks. This allows
// a caller to know which marks are associated with which paths
// in the Value.
func (val Value) UnmarkDeepWithPaths() (Value, []PathValueMarks) {
	var pvm []PathValueMarks
	retVal, _ := val.WrangleMarksDeep(func(mark any, path Path) (ctymarks.WrangleAction, error) {
		if len(pvm) != 0 {
			// We'll try to modify the most recent item instead of adding
			// a new one, if the path hasn't changed.
			latest := &pvm[len(pvm)-1]
			if latest.Path.Equals(path) {
				latest.Marks[mark] = struct{}{}
				return ctymarks.WrangleDrop, nil
			}
		}
		pvm = append(pvm, PathValueMarks{
			Path:  path.Copy(),
			Marks: NewValueMarks(mark),
		})
		return ctymarks.WrangleDrop, nil
	})
	return retVal, pvm
}

func (val Value) unmarkForce() Value {
	unw, _ := val.Unmark()
	return unw
}

// WithMarks returns a new value that has the same type and underlying value
// as the receiver and also has the marks from the given maps (representing
// sets).
func (val Value) WithMarks(marks ...ValueMarks) Value {
	if len(marks) == 0 {
		return val
	}
	ownMarks := val.Marks()
	markCount := len(ownMarks)
	for _, s := range marks {
		markCount += len(s)
	}
	if markCount == 0 {
		return val
	}
	newMarks := make(ValueMarks, markCount)
	for m := range ownMarks {
		newMarks[m] = struct{}{}
	}
	for _, s := range marks {
		for m := range s {
			newMarks[m] = struct{}{}
		}
	}
	v := val.v
	if mr, ok := v.(marker); ok {
		v = mr.realV
	}
	return Value{
		ty: val.ty,
		v: marker{
			realV: v,
			marks: newMarks,
		},
	}
}

// WithSameMarks returns a new value that has the same type and underlying
// value as the receiver and also has the marks from the given source values.
//
// Use this if you are implementing your own higher-level operations against
// cty using the integration methods, to re-introduce the marks from the
// source values of the operation.
func (val Value) WithSameMarks(srcs ...Value) Value {
	if len(srcs) == 0 {
		return val
	}
	ownMarks := val.Marks()
	markCount := len(ownMarks)
	for _, sv := range srcs {
		if mr, ok := sv.v.(marker); ok {
			markCount += len(mr.marks)
		}
	}
	if markCount == 0 {
		return val
	}
	newMarks := make(ValueMarks, markCount)
	for m := range ownMarks {
		newMarks[m] = struct{}{}
	}
	for _, sv := range srcs {
		if mr, ok := sv.v.(marker); ok {
			for m := range mr.marks {
				newMarks[m] = struct{}{}
			}
		}
	}
	v := val.v
	if mr, ok := v.(marker); ok {
		v = mr.realV
	}
	return Value{
		ty: val.ty,
		v: marker{
			realV: v,
			marks: newMarks,
		},
	}
}
