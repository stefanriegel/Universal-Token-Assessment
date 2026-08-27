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
	"sort"

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

func nativeManagementRate(category string) int {
	switch category {
	case calculator.CategoryDDIObjects:
		return calculator.TokensPerDDIObject
	case calculator.CategoryActiveIPs:
		return calculator.TokensPerActiveIP
	case calculator.CategoryManagedAssets:
		return calculator.TokensPerManagedAsset
	default:
		return 1
	}
}

func niosManagementRate(category string) int {
	switch category {
	case calculator.CategoryDDIObjects:
		return calculator.NIOSTokensPerDDIObject
	case calculator.CategoryActiveIPs:
		return calculator.NIOSTokensPerActiveIP
	case calculator.CategoryManagedAssets:
		return calculator.NIOSTokensPerManagedAsset
	default:
		return 1
	}
}

// calcManagementScenarioBase pools every provider in one exact fractional
// category total. This is intentionally not a sum of separately-ceiled NIOS
// and non-NIOS subtotals: 1 native DDI object at 1/25 plus 1 retained NIOS DDI
// object at 1/50 is one token after the single category ceiling, not two.
func calcManagementScenarioBase(
	findings []calculator.FindingRow,
	migratedNiosSources map[string]struct{},
	full bool,
) int {
	priced := make([]calculator.FindingRow, len(findings))
	copy(priced, findings)
	for i := range priced {
		row := &priced[i]
		rate := nativeManagementRate(row.Category)
		if !full && row.Provider == "nios" {
			if _, migrated := migratedNiosSources[row.Source]; !migrated {
				rate = niosManagementRate(row.Category)
			}
		} else if !full && row.Provider != "nios" && row.TokensPerUnit > 0 {
			// Preserve an explicitly supplied current rate for non-NIOS rows.
			rate = row.TokensPerUnit
		}
		row.TokensPerUnit = rate
	}
	return calculator.Calculate(priced).GrandTotal
}

// applyManagementGrowth applies the report's management growth buffer once to
// an authoritative pooled scenario total. Applying growth to individual rows
// would introduce another non-additive rounding layer.
func applyManagementGrowth(tokens int, pct float64) int {
	return int(math.Ceil(float64(tokens) * (1 + pct)))
}

// selectedMicrosoftAllocationDelta returns the backend-computed delta for the
// selected Microsoft allocation scenario. It deliberately mirrors the UI's
// validation gates: unavailable or incomplete snapshots cannot affect totals.
func selectedMicrosoftAllocationDelta(in ReportInput) int {
	ma := in.MicrosoftAllocation
	if ma == nil || !ma.Diagnostic.Available || in.SelectedMSScenario == "" || in.SelectedMSScenario == "none" || len(ma.Scenarios) != 4 {
		return 0
	}

	hasBaseline := false
	for _, scenario := range ma.Scenarios {
		if scenario.ID == "none" {
			hasBaseline = true
			break
		}
	}
	if !hasBaseline {
		return 0
	}

	for _, scenario := range ma.Scenarios {
		if scenario.ID == in.SelectedMSScenario {
			return scenario.DeltaTokens
		}
	}
	return 0
}

func isMigrated(in ReportInput, memberName string) bool {
	_, ok := in.NiosMigrationMap[memberName]
	return ok
}

func selectableNiosScenarioSources(in ReportInput, findings []calculator.FindingRow) map[string]struct{} {
	sources := make(map[string]struct{}, len(findings)+len(in.NiosServerMetrics))
	metricsByName := make(map[string]NiosServerMetricFull, len(in.NiosServerMetrics))
	for _, metric := range in.NiosServerMetrics {
		metricsByName[metric.MemberName] = metric
	}
	for _, finding := range findings {
		if finding.Source == "" {
			continue
		}
		metric, hasMetric := metricsByName[finding.Source]
		if hasMetric && (metric.Role == "GM" || metric.Role == "GMC") && !metric.RunsDnsDhcp {
			continue
		}
		sources[finding.Source] = struct{}{}
	}
	for _, metric := range in.NiosServerMetrics {
		if metric.MemberName == "" || ((metric.Role == "GM" || metric.Role == "GMC") && !metric.RunsDnsDhcp) {
			continue
		}
		sources[metric.MemberName] = struct{}{}
	}
	return sources
}

func partialNiosMigrationCount(in ReportInput, findings []calculator.FindingRow) (int, bool) {
	sources := selectableNiosScenarioSources(in, findings)
	migrated := 0
	for source := range sources {
		if isMigrated(in, source) {
			migrated++
		}
	}
	return migrated, migrated > 0 && migrated < len(sources)
}

type xaasTokenTier struct {
	maxQPS, maxLPS, maxObjects, serverTokens, maxConnections int
}

var xaasTokenTiers = []xaasTokenTier{
	{20_000, 200, 29_000, 2_400, 10},
	{40_000, 300, 110_000, 4_100, 20},
	{70_000, 400, 440_000, 6_100, 35},
	{115_000, 675, 880_000, 8_500, 85},
}

const (
	xaasExtraConnectionCost = 100
	xaasMaxExtraConnections = 400
)

// calcXaasServerTokens mirrors the frontend's consolidateXaasInstances
// algorithm: largest workloads are packed first, each instance is sized from
// pooled QPS/LPS/objects and connection count, and excess XL connections are
// charged individually.
func calcXaasServerTokens(metrics []NiosServerMetricFull, objectOverrides map[string]int) int {
	if len(metrics) == 0 {
		return 0
	}

	sorted := append([]NiosServerMetricFull(nil), metrics...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].QPS+sorted[i].LPS*100 > sorted[j].QPS+sorted[j].LPS*100
	})

	xl := xaasTokenTiers[len(xaasTokenTiers)-1]
	total := 0
	connections := 0
	qps := 0
	lps := 0
	objects := 0

	flush := func() {
		if connections == 0 {
			return
		}
		tier := xl
		for _, candidate := range xaasTokenTiers {
			if qps <= candidate.maxQPS && lps <= candidate.maxLPS && objects <= candidate.maxObjects && connections <= candidate.maxConnections {
				tier = candidate
				break
			}
		}
		extraConnections := connections - tier.maxConnections
		if extraConnections < 0 {
			extraConnections = 0
		}
		total += tier.serverTokens + extraConnections*xaasExtraConnectionCost
		connections = 0
		qps = 0
		lps = 0
		objects = 0
	}

	for i := range sorted {
		metric := &sorted[i]
		nextConnections := connections + 1
		nextQPS := qps + metric.QPS
		nextLPS := lps + metric.LPS
		nextObjects := objects + serverSizingObjects(metric, objectOverrides)
		if connections > 0 && (nextQPS > xl.maxQPS || nextLPS > xl.maxLPS || nextObjects > xl.maxObjects || nextConnections > xl.maxConnections+xaasMaxExtraConnections) {
			flush()
		}
		connections++
		qps += metric.QPS
		lps += metric.LPS
		objects += serverSizingObjects(metric, objectOverrides)
	}
	flush()

	return total
}

// calcScenarioServerTokens uses only migrated members in a partial scenario and
// every sizeable member in a full scenario. Both scenarios honor map-selected
// XaaS form factors and pool those members into consolidated instances; members
// without an explicit XaaS target default to individual NIOS-X appliances.
func calcScenarioServerTokens(metrics []NiosServerMetricFull, in ReportInput, full bool) int {
	niosXTotal := 0
	xaasMetrics := make([]NiosServerMetricFull, 0)
	for i := range metrics {
		migrated := full || isMigrated(in, metrics[i].MemberName)
		if !migrated || excludeFromServerTokens(&metrics[i], true) {
			continue
		}
		if in.NiosMigrationMap[metrics[i].MemberName] == "nios-xaas" {
			xaasMetrics = append(xaasMetrics, metrics[i])
			continue
		}
		niosXTotal += calcNiosXServerTokens(metrics[i].QPS, metrics[i].LPS, serverSizingObjects(&metrics[i], in.NiosServerObjectOverrides))
	}
	return niosXTotal + calcXaasServerTokens(xaasMetrics, in.NiosServerObjectOverrides)
}

// serverSizingObjects returns the sizing object count for tier calculation.
func serverSizingObjects(m *NiosServerMetricFull, objectOverrides ...map[string]int) int {
	if len(objectOverrides) > 0 {
		if override, ok := objectOverrides[0][m.MemberID]; ok {
			return override
		}
	}
	if m.ServerObjectCount > 0 {
		return m.ServerObjectCount
	}
	return m.ObjectCount
}

// gmStatusLabel mirrors wizard-gm-status.ts's resolveGmStatus label logic for
// the Go-side exporter (2026-07-08 mode-aware GM handling spec).
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

	msDelta := selectedMicrosoftAllocationDelta(in)

	// Pool each scenario first, then apply exactly one growth ceiling. Full
	// already prices every object at Universal DDI rates, so adding the selected
	// Microsoft delta there would count that same workload twice.
	currentMgmtTokens := applyManagementGrowth(
		calcManagementScenarioBase(in.Findings, nil, false)+msDelta,
		in.GrowthBufferPct,
	)
	fullMgmtTokens := applyManagementGrowth(
		calcManagementScenarioBase(in.Findings, nil, true),
		in.GrowthBufferPct,
	)

	migratedNiosSources := make(map[string]struct{})
	selectableSources := selectableNiosScenarioSources(in, niosFindings)
	for source := range selectableSources {
		if isMigrated(in, source) {
			migratedNiosSources[source] = struct{}{}
		}
	}
	hybridMgmtTokens := applyManagementGrowth(
		calcManagementScenarioBase(in.Findings, migratedNiosSources, false)+msDelta,
		in.GrowthBufferPct,
	)

	hybridServerTokens := calcScenarioServerTokens(metrics, in, false)
	fullServerTokens := calcScenarioServerTokens(metrics, in, true)

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

	row := 2
	setRow := func(cells ...interface{}) error {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		err := sw.SetRow(cell, cells)
		row++
		return err
	}

	if err := setRow(
		"Current", n(currentMgmtTokens), n(0),
		fmt.Sprintf("Combined assessment baseline with NIOS retained and selected Microsoft allocation delta: %+d tokens", msDelta),
	); err != nil {
		return err
	}

	if migratedCount, partial := partialNiosMigrationCount(in, niosFindings); partial {
		if err := setRow(
			"Hybrid", n(hybridMgmtTokens), n(hybridServerTokens),
			fmt.Sprintf("%d members migrated; remaining members retained on NIOS; selected Microsoft allocation delta: %+d tokens", migratedCount, msDelta),
		); err != nil {
			return err
		}
	}

	if err := setRow(
		"Full", n(fullMgmtTokens), n(fullServerTokens),
		"All assessments migrated to Universal DDI; Microsoft allocation delta is already represented by native rates",
	); err != nil {
		return err
	}

	if err := setRow(""); err != nil {
		return err
	}

	if err := setRow(fmt.Sprintf(
		"Assumes %d%% management growth buffer (applied above) and %d%% server growth buffer "+
			"(disclosed only; not applied to this sheet's server tokens). Management growth is applied once "+
			"after pooled scenario calculation. Full uses Universal DDI rates (25/13/3); Current and retained "+
			"Hybrid members use NIOS rates (50/25/13). Server totals include only migrated, sizeable members "+
			"and honor selected form factors in Hybrid and Full, including pooled XaaS consolidation; "+
			"members without an explicit XaaS target default to NIOS-X.",
		int(math.Round(in.GrowthBufferPct*100)), int(math.Round(in.ServerGrowthBufferPct*100)),
	)); err != nil {
		return err
	}

	return sw.Flush()
}

// buildNiosServerTokensSheet writes server sizing for the selected migration plan.
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
	xaasMetrics := make([]NiosServerMetricFull, 0)
	for i := range metrics {
		migrated := isMigrated(in, metrics[i].MemberName)
		if !migrated || excludeFromServerTokens(&metrics[i], true) {
			continue
		}
		m := &metrics[i]
		sizingObj := serverSizingObjects(m, in.NiosServerObjectOverrides)
		if in.NiosMigrationMap[m.MemberName] == "nios-xaas" {
			xaasMetrics = append(xaasMetrics, *m)
			cell, _ := excelize.CoordinatesToCellName(1, row)
			if err := sw.SetRow(cell, []interface{}{
				m.MemberName, m.Role, m.Model, m.Platform,
				n(m.QPS), n(m.LPS), n(sizingObj), "Pooled XaaS", "",
			}); err != nil {
				return err
			}
			row++
			continue
		}
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

	if len(xaasMetrics) > 0 {
		xaasQPS := 0
		xaasLPS := 0
		xaasObjects := 0
		for i := range xaasMetrics {
			xaasQPS += xaasMetrics[i].QPS
			xaasLPS += xaasMetrics[i].LPS
			xaasObjects += serverSizingObjects(&xaasMetrics[i], in.NiosServerObjectOverrides)
		}
		xaasTokens := calcXaasServerTokens(xaasMetrics, in.NiosServerObjectOverrides)
		totalServerTokens += xaasTokens
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := sw.SetRow(cell, []interface{}{
			fmt.Sprintf("XaaS CONSOLIDATED TOTAL (%d members; one or more instances)", len(xaasMetrics)),
			"", "", "", n(xaasQPS), n(xaasLPS), n(xaasObjects), "Pooled XaaS", n(xaasTokens),
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

		status := gmStatusLabel(&metrics[i], isMigrated(in, m.MemberName))

		// DHCP Utilization: permille to fraction for Excel percentage format
		dhcpUtil := float64(m.DHCPUtilization) / 1000.0

		if err := sw.SetRow(cell, []interface{}{
			m.MemberName, m.Role, m.Model, m.Platform,
			n(m.QPS), n(m.LPS), n(m.ObjectCount), n(serverSizingObjects(&m, in.NiosServerObjectOverrides)), n(m.ActiveIPCount),
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
