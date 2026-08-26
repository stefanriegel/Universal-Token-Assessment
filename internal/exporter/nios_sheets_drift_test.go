package exporter

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// jsonFieldNames extracts the json-tag names of a struct value, stripping any
// ,omitempty (or other) options and skipping "" / "-" tags.
func jsonFieldNames(v interface{}) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func loadFixtureFieldNames(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		t.Fatalf("failed to parse fixture %s: %v", path, err)
	}
	if len(names) == 0 {
		t.Fatalf("fixture %s is empty", path)
	}
	return names
}

func assertFieldSetsEqual(t *testing.T, structName string, got, want []string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}

	var missing, extra []string
	for n := range wantSet {
		if !gotSet[n] {
			missing = append(missing, n)
		}
	}
	for n := range gotSet {
		if !wantSet[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s field drift: missing from struct=%v, extra in struct=%v", structName, missing, extra)
	}
}

// TestNiosServerMetricFieldDrift fails if nios.NiosServerMetric or
// NiosServerMetricFull gain, lose, or rename a json-tag field without the
// shared fixture testdata/nios-metric-fields.json being updated (NIOS-03).
func TestNiosServerMetricFieldDrift(t *testing.T) {
	want := loadFixtureFieldNames(t, "../../testdata/nios-metric-fields.json")

	assertFieldSetsEqual(t, "nios.NiosServerMetric", jsonFieldNames(nios.NiosServerMetric{}), want)
	assertFieldSetsEqual(t, "exporter.NiosServerMetricFull", jsonFieldNames(NiosServerMetricFull{}), want)
}
