package server

import (
	"os"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// TestMicrosoftAllocation_WireShapeParity pins the three independently
// maintained copies of the Microsoft allocation wire shape — server.go,
// internal/exporter, and the TypeScript client — to one field set, anchored
// on the internal/scanner/nios domain types as the source of truth.
//
// These three copies are connected only by matching JSON names: a rename on
// one side produces no compile error in any language, and silently zeroes
// the field on the other two. That silent desync is exactly the failure
// MSPAR-01 exists to prevent.
func TestMicrosoftAllocation_WireShapeParity(t *testing.T) {
	tsSrc, err := os.ReadFile("../frontend/src/app/components/api-client.ts")
	if err != nil {
		t.Fatalf("read api-client.ts: %v", err)
	}

	t.Run("MicrosoftAllocation", func(t *testing.T) {
		domain := append(serverJSONFieldNames(nios.MSAllocationScenarioSet{}), "evidence")
		serverFields := serverJSONFieldNames(MicrosoftAllocation{})
		exporterFields := serverJSONFieldNames(exporter.MicrosoftAllocation{})
		tsFields := tsInterfaceFieldNames(t, tsSrc, "MicrosoftAllocationAPI")

		assertServerFieldSetsEqual(t, "server.MicrosoftAllocation vs domain", serverFields, domain)
		assertServerFieldSetsEqual(t, "exporter.MicrosoftAllocation vs domain", exporterFields, domain)
		assertServerFieldSetsEqual(t, "MicrosoftAllocationAPI vs domain", tsFields, domain)
	})

	t.Run("MSAllocationScenario", func(t *testing.T) {
		domain := serverJSONFieldNames(nios.MSAllocationScenario{})
		serverFields := serverJSONFieldNames(MSAllocationScenario{})
		exporterFields := serverJSONFieldNames(exporter.MSAllocationScenario{})
		tsFields := tsInterfaceFieldNames(t, tsSrc, "MSAllocationScenarioAPI")

		assertServerFieldSetsEqual(t, "server.MSAllocationScenario vs domain", serverFields, domain)
		assertServerFieldSetsEqual(t, "exporter.MSAllocationScenario vs domain", exporterFields, domain)
		assertServerFieldSetsEqual(t, "MSAllocationScenarioAPI vs domain", tsFields, domain)
	})

	t.Run("MSCategoryTokens", func(t *testing.T) {
		domain := serverJSONFieldNames(nios.MSCategoryTokens{})
		serverFields := serverJSONFieldNames(MSCategoryTokens{})
		exporterFields := serverJSONFieldNames(exporter.MSCategoryTokens{})
		tsFields := tsInterfaceFieldNames(t, tsSrc, "MSCategoryTokensAPI")

		assertServerFieldSetsEqual(t, "server.MSCategoryTokens vs domain", serverFields, domain)
		assertServerFieldSetsEqual(t, "exporter.MSCategoryTokens vs domain", exporterFields, domain)
		assertServerFieldSetsEqual(t, "MSCategoryTokensAPI vs domain", tsFields, domain)
	})

	// serverJSONFieldNames enumerates top-level fields only — it does not recurse.
	// The MicrosoftAllocation subtest above therefore proves only that the literal
	// names "diagnostic" and "evidence" exist on all three copies, and says nothing
	// about what is inside them. These two subtests close that gap; without them a
	// rename of MSAllocationDiagnostic.Code or a dropped MSEvidenceCounts field
	// desyncs silently, the exact failure the rest of this file exists to prevent.
	t.Run("MSAllocationDiagnostic", func(t *testing.T) {
		domain := serverJSONFieldNames(nios.MSAllocationDiagnostic{})
		serverFields := serverJSONFieldNames(MSAllocationDiagnostic{})
		exporterFields := serverJSONFieldNames(exporter.MSAllocationDiagnostic{})
		tsFields := tsInterfaceFieldNames(t, tsSrc, "MSAllocationDiagnosticAPI")

		assertServerFieldSetsEqual(t, "server.MSAllocationDiagnostic vs domain", serverFields, domain)
		assertServerFieldSetsEqual(t, "exporter.MSAllocationDiagnostic vs domain", exporterFields, domain)
		assertServerFieldSetsEqual(t, "MSAllocationDiagnosticAPI vs domain", tsFields, domain)
	})

	t.Run("MSEvidenceCounts", func(t *testing.T) {
		domain := serverJSONFieldNames(nios.MSEvidenceCounts{})
		serverFields := serverJSONFieldNames(MSEvidenceCounts{})
		exporterFields := serverJSONFieldNames(exporter.MSEvidenceCounts{})
		tsFields := tsInterfaceFieldNames(t, tsSrc, "MSEvidenceCountsAPI")

		assertServerFieldSetsEqual(t, "server.MSEvidenceCounts vs domain", serverFields, domain)
		assertServerFieldSetsEqual(t, "exporter.MSEvidenceCounts vs domain", exporterFields, domain)
		assertServerFieldSetsEqual(t, "MSEvidenceCountsAPI vs domain", tsFields, domain)
	})
}
