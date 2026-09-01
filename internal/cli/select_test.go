package cli

import (
	"testing"

	"github.com/kyleking/doneram/internal/engine"
)

func TestSelectSites(t *testing.T) {
	sites := []engine.Site{{Tool: "jq"}, {Tool: "hk"}, {Tool: "jq"}}

	all, err := selectSites(sites, nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("selectSites(nil) = %d sites, %v; want all 3", len(all), err)
	}

	kept, err := selectSites(sites, []string{"jq"})
	if err != nil {
		t.Fatalf("selectSites: %v", err)
	}
	if len(kept) != 2 {
		t.Errorf("kept %d sites, want both jq sites", len(kept))
	}

	if _, err := selectSites(sites, []string{"jq", "nope"}); err == nil {
		t.Error("err = nil, want an error naming the undeclared tool")
	}
}
