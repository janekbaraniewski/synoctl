package tui

// NavSection is a labelled group of views in the left sidebar. The label
// is rendered as a small-caps header above its items. Empty sections are
// not rendered, so it is safe to conditionally drop items at registration
// (e.g. omit Surveillance Station entries on a device that doesn't have it
// installed).
type NavSection struct {
	Name  string // section header — e.g. "Storage", "Apps", "System"
	Views []View
}

// navCursor identifies one item across the flattened sidebar. Sections are
// purely visual; selection is one-dimensional.
type navCursor struct {
	flat int // index into App.flat
}

// flatItem is a navigable entry in the flattened sidebar — either a real
// view, or a section header (which is skipped when the cursor moves).
type flatItem struct {
	isHeader bool
	header   string
	view     View
	section  string // for non-headers — the section name they belong to
}

// flatten builds the navigable list. Headers and the views that follow them
// are emitted in the order the caller registered.
func flattenSections(sections []NavSection) []flatItem {
	var out []flatItem
	for _, s := range sections {
		if len(s.Views) == 0 {
			continue
		}
		out = append(out, flatItem{isHeader: true, header: s.Name})
		for _, v := range s.Views {
			out = append(out, flatItem{view: v, section: s.Name})
		}
	}
	return out
}

// firstViewIndex returns the index of the first non-header item, or -1.
func firstViewIndex(items []flatItem) int {
	for i, it := range items {
		if !it.isHeader {
			return i
		}
	}
	return -1
}

// stepView advances the cursor by delta, skipping header rows. Returns the
// new index (or the input if no view exists in that direction).
func stepView(items []flatItem, from, delta int) int {
	n := len(items)
	if n == 0 {
		return from
	}
	i := from
	for steps := 0; steps < n; steps++ {
		i += delta
		if i < 0 {
			i = n - 1
		}
		if i >= n {
			i = 0
		}
		if !items[i].isHeader {
			return i
		}
	}
	return from
}
