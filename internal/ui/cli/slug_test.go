package cli_test

import (
	"encoding/json"
	"testing"
)

// The slug rule's output contract: dropped results are counted in data.dropped and announced in warnings[].
func TestSearchJSONReportsDroppedResults(t *testing.T) {
	p := page("Alpha")
	p.Dropped = 2
	r := exec(t, searchWith(p), nil, "search", "x", "--json")
	var env struct {
		Data struct {
			Dropped int `json:"dropped"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil {
		t.Fatalf("%v %s", err, r.out.String())
	}
	if env.Data.Dropped != 2 || len(env.Warnings) == 0 || env.Warnings[0] != "2 results were hidden because their slug has an unexpected form." {
		t.Fatalf("dropped %d, warnings %q", env.Data.Dropped, env.Warnings)
	}
	if clean := exec(t, searchWith(page("Alpha")), nil, "search", "x", "--json"); clean.code != 0 {
		t.Fatalf("exit %d", clean.code)
	}
}
