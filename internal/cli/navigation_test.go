package cli

import (
	"testing"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

func TestAppSectionsHaveCompleteNavigation(t *testing.T) {
	client, err := dsm.New(dsm.Options{Host: "demo-nas.local", DisplayHost: defaultDemoHostLabel})
	if err != nil {
		t.Fatal(err)
	}
	sections := appSections(tui.ViewContext{
		Client: client,
		Theme:  tui.DefaultTheme(),
		Keys:   tui.DefaultKeys(),
	})

	wantSections := []string{"Overview", "Storage", "Apps", "Backup", "Services", "Security", "System", "Settings", "Tools"}
	if len(sections) != len(wantSections) {
		t.Fatalf("section count = %d, want %d", len(sections), len(wantSections))
	}
	for i, want := range wantSections {
		if got := sections[i].Name; got != want {
			t.Fatalf("section[%d] = %q, want %q", i, got, want)
		}
		if len(sections[i].Views) == 0 {
			t.Fatalf("section %q has no views", want)
		}
		for _, view := range sections[i].Views {
			if view == nil {
				t.Fatalf("section %q has nil view", want)
			}
			if view.Name() == "" || view.Title() == "" || view.Icon() == "" {
				t.Fatalf("section %q has incomplete view metadata: name=%q title=%q icon=%q", want, view.Name(), view.Title(), view.Icon())
			}
		}
	}
}
