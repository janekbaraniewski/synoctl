package tui

// Inspector is the optional interface a View can implement to populate the
// right-hand inspector pane.
//
// When a view implements this interface and the terminal is wide enough,
// the shell carves the body into two columns:
//
//	main pane (View.Render(mainW, h))   │   inspector (View.Inspect(inspW, h))
//
// Returning the empty string leaves the inspector blank (the shell still
// draws the separator and pane background). Views without an inspector
// (the dashboard, for example) simply don't implement this interface and
// get the full body width.
//
// Note: the inspector pane is hidden automatically on narrow terminals
// (width < InspectorMinTotalWidth). Views must therefore continue to
// render meaningfully without an inspector — typically by switching to a
// full-screen detail overlay when the user drills in, which is what the
// existing list views already do.
type Inspector interface {
	Inspect(width, height int) string
}

// InspectorMinTotalWidth is the terminal width below which the inspector
// pane is hidden entirely.
const InspectorMinTotalWidth = 120

// InspectorWidth picks an inspector pane width given the total terminal
// width. Returns 0 to indicate "don't render the inspector at all".
func InspectorWidth(total int) int {
	switch {
	case total < InspectorMinTotalWidth:
		return 0
	case total < 160:
		return 38
	case total < 200:
		return 46
	default:
		return 52
	}
}
