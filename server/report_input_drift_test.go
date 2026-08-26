package server

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
)

var (
	tsFieldNameRE = regexp.MustCompile(`(?m)^\s*(\w+)\??:`)
	tsCommentRE   = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)
)

// stripNestedTypeLiterals removes the contents of any brace-delimited type
// (e.g. an inline `{ ddiTokens: number; ... }` field type) from a TS
// interface body. Without this, a nested object type reformatted across
// multiple lines reads as extra top-level fields, since each of its members
// would land at the start of its own line.
func stripNestedTypeLiterals(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '{':
			depth++
			continue
		case '}':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tsInterfaceFieldNames extracts field names from a named TypeScript
// interface block in src. It fails the test loudly (never returns silently
// empty) if the interface can't be found or the block yields no fields, so a
// reformatted source file can't make this guard pass vacuously.
func tsInterfaceFieldNames(t *testing.T, src []byte, interfaceName string) []string {
	t.Helper()
	block := regexp.MustCompile(`(?s)export interface ` + interfaceName + ` \{(.*?)\n\}`).FindSubmatch(src)
	if block == nil {
		t.Fatalf("could not find `export interface %s { ... }` in source", interfaceName)
	}
	body := tsCommentRE.ReplaceAllString(string(block[1]), "")
	body = stripNestedTypeLiterals(body)
	var names []string
	for _, m := range tsFieldNameRE.FindAllStringSubmatch(body, -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("found interface %s but extracted zero fields — regex is stale", interfaceName)
	}
	return names
}

// TestReportExportPayload_MatchesReportInput guards against drift between the
// hand-built TS export payload (frontend/src/app/components/api-client.ts)
// and the exporter.ReportInput struct the Go handler decodes it into. A field
// added to one side and not the other, or renamed on one side, produces no
// compile error in either language — it silently exports a zero value or
// silently stops arriving.
//
// The TS side is parsed straight from source rather than a fixture: a
// fixture here would be a third copy of the field list to keep in sync,
// which is the very drift this test exists to catch.
func TestReportExportPayload_MatchesReportInput(t *testing.T) {
	src, err := os.ReadFile("../frontend/src/app/components/api-client.ts")
	if err != nil {
		t.Fatalf("read api-client.ts: %v", err)
	}
	tsFields := tsInterfaceFieldNames(t, src, "ReportExportPayload")
	goFields := serverJSONFieldNames(exporter.ReportInput{})

	assertServerFieldSetsEqual(t, "exporter.ReportInput vs ReportExportPayload", goFields, tsFields)
}

// TestTsInterfaceFieldNames_MultilineNestedObjectAndComments is a regression
// test for a reformatted nested inline object type (as `totals` is, in the
// real interface) spanning multiple lines, plus a JSDoc block and a line
// comment between fields — none of which are additional top-level fields.
func TestTsInterfaceFieldNames_MultilineNestedObjectAndComments(t *testing.T) {
	src := []byte(`
export interface ReportExportPayload {
  generatedAt: string;
  /**
   * Rolled-up totals across categories.
   */
  totals: {
    ddiTokens: number;
    ipTokens: number;
    assetTokens: number;
    grandTotal: number;
  };
  // provider name -> token count
  providerTotals: Record<string, number>;
}
`)
	want := []string{"generatedAt", "totals", "providerTotals"}
	got := tsInterfaceFieldNames(t, src, "ReportExportPayload")

	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got fields %v, want %v", got, want)
	}
}
