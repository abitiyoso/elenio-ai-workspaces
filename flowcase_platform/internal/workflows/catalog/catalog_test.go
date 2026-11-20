package catalog

import "testing"

func TestCatalogRejectsUnsupportedInputContract(t *testing.T) {
	for _, tc := range []struct {
		contract string
		valid    bool
	}{
		{"", true}, {"sim.v1", true}, {"flowcase.case.v1", true}, {"untrusted", false},
	} {
		c := NewStatic()
		err := c.Merge(Snapshot{Agents: []Agent{{Ref: "test.agent", InputContract: tc.contract}}})
		if (err == nil) != tc.valid {
			t.Fatalf("contract %q: %v", tc.contract, err)
		}
	}
}
