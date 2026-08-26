// Package exporter — NIOS migration sheet builders.
//
// Three conditional sheets added to the Excel export when NiosServerMetrics is present:
//   - NIOS Migration Scenarios: current vs full-UDDI token comparison
//   - NIOS Server Tokens: per-member tier and server token calculation
//   - NIOS Member Details: full metrics for all members including infra-only

package exporter

import (
	"fmt"
	"math"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/xuri/excelize/v2"
)

// ceilDiv computes ceiling(n / d). Returns 0 if n is 0.
func ceilDiv(n, d int) int {
	if n == 0 {
		return 0
	}
	return (n + d - 1) / d
}

// calcUddiTokensAggregated computes UDDI management tokens using native rates (25/13/3).
// Aggregates counts per category, applies one ceiling division per category, then SUMS.
func calcUddiTokensAggregated(findings []calculator.FindingRow) int {
	var totalDDI, totalIP, totalAsset int
	for _, f := range findings {
		switch f.Category {
		case calculator.CategoryDDIObjects:
			totalDDI += f.Count
		case calculator.CategoryActiveIPs:
			totalIP += f.Count
		case calculator.CategoryManagedAssets:
			totalAsset += f.Count
		}
	}
	return ceilDiv(totalDDI, calculator.TokensPerDDIObject) +
		ceilDiv(totalIP, calculator.TokensPerActiveIP) +
		ceilDiv(totalAsset, calculator.TokensPerManagedAsset)
}

// calcNiosTokens computes NIOS licensing tokens using NIOS rates (50/25/13).
// Aggregates counts per category, applies one ceiling division per category, then sums
// (SUM-native, matching the official Infoblox engine).
func calcNiosTokens(findings []calculator.FindingRow) int {
	var totalDDI, totalIP, totalAsset int
	for _, f := range findings {
		switch f.Category {
		case calculator.CategoryDDIObjects:
			totalDDI += f.Count
		case calculator.CategoryActiveIPs:
			totalIP += f.Count
		case calculator.CategoryManagedAssets:
			totalAsset += f.Count
		}
	}
	ddi := ceilDiv(totalDDI, calculator.NIOSTokensPerDDIObject)
	ip := ceilDiv(totalIP, calculator.NIOSTokensPerActiveIP)
	asset := ceilDiv(totalAsset, calculator.NIOSTokensPerManagedAsset)

	return ddi + ip + asset
}

// applyGrowthBuffer applies a percentage growth buffer to a value.
func applyGrowthBuffer(value int, pct float64) int {
	return int(math.Ceil(float64(value) * (1 + pct)))
}

// applyGrowthToFindings returns a copy of findings with counts scaled by the growth buffer.
func applyGrowthToFindings(findings []calculator.FindingRow, pct float64) []calculator.FindingRow {
	out := make([]calculator.FindingRow, len(findings))
	for i, f := range findings {
		out[i] = f
		out[i].Count = applyGrowthBuffer(f.Count, pct)
	}
	return out
}

// serverSizingObjects returns the sizing object count for tier calculation.
func serverSizingObjects(m *NiosServerMetricFull) int {
	return m.ObjectCount + m.ActiveIPCount
}

// gmStatusLabel mirrors wizard-gm-status.ts's resolveGmStatus label logic for
// the Go-side exporter (2026-07-08 mode-aware GM handling spec).
//
// NOTE: ReportInput.NiosMigrationMap now carries per-member migration status
// to the exporter, but the call sites below don't consult it yet — see
// docs/superpowers/specs/2026-07-08-nios-migration-mode-gm-handling-design.md.
// They still pass isMigrated=false unconditionally, meaning GM/GMC members
// always report "Retained on NIOS" until that map is wired into these calls.
func gmStatusLabel(m *NiosServerMetricFull, isMigrated bool) string {
	if m.Role != "GM" && m.Role != "GMC" {
		return ""
	}
	if !isMigrated {
		return "Retained on NIOS"
	}
	if !m.RunsDnsDhcp {
		return "Replaced by Infoblox Portal"
	}
	return ""
}

// excludeFromServerTokens reports whether m should be excluded from
// server-token sizing given its migration status — true for retained GM/GMC
// and for migrated management-only GM/GMC, false otherwise.
func excludeFromServerTokens(m *NiosServerMetricFull, isMigrated bool) bool {
	return gmStatusLabel(m, isMigrated) != ""
}

// tierNames maps tier index to display name.
var tierNames = []string{"2XS", "XS", "S", "M", "L", "XL"}

// calcServerTokenTier returns the tier name and server tokens for a member.
func calcServerTokenTier(qps, lps, sizingObjects int) (string, int) {
	for i, t := range niosXTiers {
		if qps <= t.maxQPS && lps <= t.maxLPS && sizingObjects <= t.maxObjects {
			return tierNames[i], t.serverTokens
		}
	}
	return tierNames[len(tierNames)-1], niosXTiers[len(niosXTiers)-1].serverTokens
}

// buildNiosMigrationScenariosSheet writes the NIOS Migration Scenarios sheet.
func buildNiosMigrationScenariosSheet(f *excelize.File, styles sheetStyles, in ReportInput) error {
	metrics := in.NiosServerMetrics

	niosFindings := findingsByProvider(in.Findings, "nios")

	// Current NIOS tokens (no growth buffer)
	currentNiosTokens := calcNiosTokens(niosFindings)

	// UDDI management tokens with the report's growth buffer
	bufferedFindings := applyGrowthToFindings(niosFindings, in.GrowthBufferPct)
	uddiMgmtTokens := calcUddiTokensAggregated(bufferedFindings)

	// Total server tokens (non-infra-only members). No growth buffer here:
	// the on-screen Migration Planner's own scenario cards size server tokens
	// unbuffered too (ServerGrowthBufferPct only feeds the separate BOM/hero
	// summary total, which this sheet's simplified per-member sum doesn't
	// reproduce).
	totalServerTokens := 0
	for i := range metrics {
		if excludeFromServerTokens(&metrics[i], false) {
			continue
		}
		totalServerTokens += calcNiosXServerTokens(metrics[i].QPS, metrics[i].LPS, serverSizingObjects(&metrics[i]))
	}

	sw, err := f.NewStreamWriter("NIOS Migration Scenarios")
	if err != nil {
		return fmt.Errorf("exporter: StreamWriter NIOS Migration Scenarios: %w", err)
	}

	_ = sw.SetColWidth(1, 1, 30)
	_ = sw.SetColWidth(2, 2, 22)
	_ = sw.SetColWidth(3, 3, 18)
	_ = sw.SetColWidth(4, 4, 60)

	h := func(v string) excelize.Cell { return excelize.Cell{StyleID: styles.header, Value: v} }
	n := func(v int) excelize.Cell { return excelize.Cell{StyleID: styles.number, Value: v} }

	if err := sw.SetRow("A1", []interface{}{
		h("Scenario"), h("Management Tokens"), h("Server Tokens"), h("Description"),
	}); err != nil {
		return err
	}

	// Row 2: Current (NIOS Only)
	if err := sw.SetRow("A2", []interface{}{
		"Current (NIOS Only)", n(currentNiosTokens), n(0), "Current NIOS licensing (no server tokens needed)",
	}); err != nil {
		return err
	}

	// Row 3: Full Universal DDI
	if err := sw.SetRow("A3", []interface{}{
		"Full Universal DDI", n(uddiMgmtTokens), n(totalServerTokens), "All members migrated to Universal DDI",
	}); err != nil {
		return err
	}

	// Row 4: blank separator
	if err := sw.SetRow("A4", []interface{}{""}); err != nil {
		return err
	}

	// Row 5: Note
	if err := sw.SetRow("A5", []interface{}{
		fmt.Sprintf("Assumes %d%% management growth buffer (applied above) and %d%% server growth buffer "+
			"(disclosed only; not applied to this sheet's server tokens). Management tokens use UDDI rates "+
			"(25/13/3). Server tokens assume NIOS-X form factor for all members.",
			int(math.Round(in.GrowthBufferPct*100)), int(math.Round(in.ServerGrowthBufferPct*100))),
	}); err != nil {
		return err
	}

	return sw.Flush()
}

// buildNiosServerTokensSheet writes the NIOS Server Tokens sheet (excludes infra-only).
func buildNiosServerTokensSheet(f *excelize.File, styles sheetStyles, in ReportInput) error {
	metrics := in.NiosServerMetrics

	sw, err := f.NewStreamWriter("NIOS Server Tokens")
	if err != nil {
		return fmt.Errorf("exporter: StreamWriter NIOS Server Tokens: %w", err)
	}

	_ = sw.SetColWidth(1, 1, 30)
	_ = sw.SetColWidth(2, 2, 10)
	_ = sw.SetColWidth(3, 3, 15)
	_ = sw.SetColWidth(4, 4, 12)
	_ = sw.SetColWidth(5, 5, 10)
	_ = sw.SetColWidth(6, 6, 10)
	_ = sw.SetColWidth(7, 7, 15)
	_ = sw.SetColWidth(8, 8, 10)
	_ = sw.SetColWidth(9, 9, 15)

	h := func(v string) excelize.Cell { return excelize.Cell{StyleID: styles.header, Value: v} }
	n := func(v int) excelize.Cell { return excelize.Cell{StyleID: styles.number, Value: v} }

	if err := sw.SetRow("A1", []interface{}{
		h("Grid Member"), h("Role"), h("Model"), h("Platform"),
		h("QPS"), h("LPS"), h("Sizing Objects"), h("Tier"), h("Server Tokens"),
	}); err != nil {
		return err
	}

	row := 2
	totalServerTokens := 0
	for i := range metrics {
		if excludeFromServerTokens(&metrics[i], false) {
			continue
		}
		m := &metrics[i]
		sizingObj := serverSizingObjects(m)
		tierName, tokens := calcServerTokenTier(m.QPS, m.LPS, sizingObj)
		totalServerTokens += tokens

		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := sw.SetRow(cell, []interface{}{
			m.MemberName, m.Role, m.Model, m.Platform,
			n(m.QPS), n(m.LPS), n(sizingObj), tierName, n(tokens),
		}); err != nil {
			return err
		}
		row++
	}

	// Blank separator
	cell, _ := excelize.CoordinatesToCellName(1, row)
	_ = sw.SetRow(cell, []interface{}{""})
	row++

	// TOTAL row
	cell, _ = excelize.CoordinatesToCellName(1, row)
	_ = sw.SetRow(cell, []interface{}{
		excelize.Cell{StyleID: styles.total, Value: "TOTAL"},
		"", "", "", "", "", "", "",
		excelize.Cell{StyleID: styles.total, Value: totalServerTokens},
	})

	return sw.Flush()
}

// buildNiosMemberDetailsSheet writes the NIOS Member Details sheet (ALL members).
func buildNiosMemberDetailsSheet(f *excelize.File, styles sheetStyles, in ReportInput) error {
	metrics := in.NiosServerMetrics

	sw, err := f.NewStreamWriter("NIOS Member Details")
	if err != nil {
		return fmt.Errorf("exporter: StreamWriter NIOS Member Details: %w", err)
	}

	_ = sw.SetColWidth(1, 1, 30)
	_ = sw.SetColWidth(2, 2, 10)
	_ = sw.SetColWidth(3, 3, 15)
	_ = sw.SetColWidth(4, 4, 12)
	_ = sw.SetColWidth(5, 5, 10)
	_ = sw.SetColWidth(6, 6, 10)
	_ = sw.SetColWidth(7, 7, 12)
	_ = sw.SetColWidth(8, 8, 12)
	_ = sw.SetColWidth(9, 9, 12)
	_ = sw.SetColWidth(10, 10, 12)
	_ = sw.SetColWidth(11, 11, 12)
	_ = sw.SetColWidth(12, 12, 12)
	_ = sw.SetColWidth(13, 13, 12)
	_ = sw.SetColWidth(14, 14, 10)

	h := func(v string) excelize.Cell { return excelize.Cell{StyleID: styles.header, Value: v} }
	n := func(v int) excelize.Cell { return excelize.Cell{StyleID: styles.number, Value: v} }

	if err := sw.SetRow("A1", []interface{}{
		h("Grid Member"), h("Role"), h("Model"), h("Platform"),
		h("QPS"), h("LPS"), h("DDI Objects"), h("Sizing Objects"), h("Active IPs"),
		h("Managed IPs"), h("Static Hosts"), h("Dynamic Hosts"),
		h("DHCP Util %"), h("Status"),
	}); err != nil {
		return err
	}

	for i, m := range metrics {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)

		status := gmStatusLabel(&metrics[i], false)

		// DHCP Utilization: permille to fraction for Excel percentage format
		dhcpUtil := float64(m.DHCPUtilization) / 1000.0

		if err := sw.SetRow(cell, []interface{}{
			m.MemberName, m.Role, m.Model, m.Platform,
			n(m.QPS), n(m.LPS), n(m.ObjectCount), n(m.ServerObjectCount), n(m.ActiveIPCount),
			n(m.ManagedIPCount), n(m.StaticHosts), n(m.DynamicHosts),
			excelize.Cell{StyleID: styles.pct, Value: dhcpUtil}, status,
		}); err != nil {
			return err
		}
	}

	return sw.Flush()
}

// buildNiosMicrosoftServersSheet writes the NIOS Microsoft Servers sheet
// (Grid-managed Windows DNS/DHCP servers). Conditional — informational only.
func buildNiosMicrosoftServersSheet(f *excelize.File, styles sheetStyles, in ReportInput) error {
	ms := in.NiosMicrosoftServers

	sw, err := f.NewStreamWriter("NIOS Microsoft Servers")
	if err != nil {
		return fmt.Errorf("exporter: StreamWriter NIOS Microsoft Servers: %w", err)
	}

	_ = sw.SetColWidth(1, 1, 30)
	_ = sw.SetColWidth(2, 2, 16)
	_ = sw.SetColWidth(3, 3, 40)
	_ = sw.SetColWidth(4, 4, 24)
	_ = sw.SetColWidth(5, 6, 14)
	_ = sw.SetColWidth(7, 7, 12)
	_ = sw.SetColWidth(8, 8, 10)

	h := func(v string) excelize.Cell { return excelize.Cell{StyleID: styles.header, Value: v} }
	n := func(v int) excelize.Cell { return excelize.Cell{StyleID: styles.number, Value: v} }
	yesNo := func(b bool) string {
		if b {
			return "Yes"
		}
		return ""
	}

	if err := sw.SetRow("A1", []interface{}{
		h("Server (FQDN)"), h("IP"), h("OS"), h("AD Domain"),
		h("DNS Managed"), h("DHCP Managed"), h("DHCP Hosts"), h("Read-Only"),
	}); err != nil {
		return err
	}

	row := 2
	for _, s := range ms.Servers {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := sw.SetRow(cell, []interface{}{
			s.FQDN, s.Address, s.OS, s.ADDomain,
			yesNo(s.DNSManaged), yesNo(s.DHCPManaged), n(s.DHCPHosts), yesNo(s.ReadOnly),
		}); err != nil {
			return err
		}
		row++
	}

	// Blank separator + MS-managed DNS zone total.
	cell, _ := excelize.CoordinatesToCellName(1, row)
	_ = sw.SetRow(cell, []interface{}{""})
	row++
	cell, _ = excelize.CoordinatesToCellName(1, row)
	_ = sw.SetRow(cell, []interface{}{
		excelize.Cell{StyleID: styles.total, Value: "MS-Managed DNS Zones"},
		excelize.Cell{StyleID: styles.total, Value: ms.ManagedZones},
	})

	return sw.Flush()
}
