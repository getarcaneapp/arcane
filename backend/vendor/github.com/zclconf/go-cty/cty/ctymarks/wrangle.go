package ctymarks

// WrangleAction values each describe an action to be taken for a particular
// mark at a particular path that has been visited by a [WrangleFunc].
//
// A nil value of this type represents taking no action at all, and thereby
// potentially allowing other later functions to decide an action instead.
type WrangleAction interface {
	wrangleAction()
}

type simpleWrangleAction rune

func (w simpleWrangleAction) wrangleAction() {}

// WrangleKeep is a [WrangleAction] that indicates that the given mark
// should be retained at the given path and that no subsequent wrangle functions
// in the same operation should be given an opportunity to visit the same mark.
const WrangleKeep = simpleWrangleAction('k')

// WrangleDrop is a [WrangleAction] that indicates that the given mark
// should be dropped at the given path, and that no subsequent wrangle functions
// in the same operation should be given an opportunity to visit the same mark.
const WrangleDrop = simpleWrangleAction('d')

// WrangleExpand is a [WrangleAction] that indicates that the given mark
// should be transferred to the top-level value that the current mark wrangling
// process is operating on.
//
// This effectively means that the mark then applies to all parts of the
// original top-level value, rather than to only a small part of the nested
// data structure within it.
const WrangleExpand = simpleWrangleAction('e')

// WrangleReplace returns a [WrangleAction] that indicates that the given
// mark should be removed and the given mark inserted in its place.
//
// This could be useful when values must cross between systems that use
// different marks to represent similar concepts, for example.
func WrangleReplace(newMark any) WrangleAction {
	if newMark == nil {
		panic("ctymarks.WrangleReplace with nil mark")
	}
	return wrangleReplaceAction{newMark}
}

// WrangleReplaceMark returns the new mark for the given action if it was
// created using [WrangleReplace], or nil if it is any other kind of action.
func WrangleReplaceMark(action WrangleAction) any {
	replace, _ := action.(wrangleReplaceAction)
	return replace.newMark
}

type wrangleReplaceAction struct {
	newMark any
}

func (w wrangleReplaceAction) wrangleAction() {}
