// Package exporter — Microsoft allocation sheet builder.
//
// buildMicrosoftAllocationSheet writes the "Microsoft Allocation" sheet
// (D-10): the four precomputed Microsoft DNS/DHCP allocation scenarios with
// the user's selection marked (D-11), the all-NIOS baseline, the delta
// labelled as a cost (D-12), and the raw relationship evidence counts (D-06).
// Every figure is read straight off in.MicrosoftAllocation — this file
// recomputes nothing.
package exporter

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// msScenarioLabel maps a scenario id to its display label. Returns "" for an
// unrecognised id so a payload-supplied string is never echoed into a cell.
func msScenarioLabel(id string) string {
	switch id {
	case "none":
		return "All NIOS (baseline)"
	case "dns-only":
		return "Microsoft DNS on Universal DDI"
	case "dhcp-only":
		return "Microsoft DHCP on Universal DDI"
	case "both":
		return "Microsoft DNS + DHCP on Universal DDI"
	default:
		return ""
	}
}

// buildMicrosoftAllocationSheet writes the Microsoft Allocation sheet.
// Conditional — present only when in.MicrosoftAllocation != nil.
func buildMicrosoftAllocationSheet(f *excelize.File, styles sheetStyles, in ReportInput) error {
	ma := in.MicrosoftAllocation

	sw, err := f.NewStreamWriter("Microsoft Allocation")
	if err != nil {
		return fmt.Errorf("exporter: StreamWriter Microsoft Allocation: %w", err)
	}

	_ = sw.SetColWidth(1, 1, 34)
	_ = sw.SetColWidth(2, 3, 14)
	_ = sw.SetColWidth(4, 9, 15)
	_ = sw.SetColWidth(10, 12, 18)
	_ = sw.SetColWidth(13, 13, 10)

	h := func(v string) excelize.Cell { return excelize.Cell{StyleID: styles.header, Value: v} }
	n := func(v int) excelize.Cell { return excelize.Cell{StyleID: styles.number, Value: v} }
	yesNo := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}

	row := 1
	setRow := func(cells ...interface{}) error {
		cell, _ := excelize.CoordinatesToCellName(1, row)
		err := sw.SetRow(cell, cells)
		row++
		return err
	}
	blank := func() error { return setRow("") }

	// Block one: scenario comparison — header row plus four data rows.
	if err := setRow(
		h("Scenario"), h("Manage MS DNS"), h("Manage MS DHCP"),
		h("DDI Objects (NIOS)"), h("DDI Objects (Universal DDI)"), h("DDI Object Tokens"),
		h("Active IPs (NIOS)"), h("Active IPs (Universal DDI)"), h("Active IP Tokens"),
		h("Managed Asset Tokens"), h("Effective Tokens"),
		h("Additional Tokens vs all-NIOS"), h("Selected"),
	); err != nil {
		return err
	}

	for _, sc := range ma.Scenarios {
		marker := ""
		if in.SelectedMSScenario != "" && in.SelectedMSScenario == sc.ID {
			marker = "Selected"
		}
		ddi, ip, asset := sc.Categories[0], sc.Categories[1], sc.Categories[2]

		values := []interface{}{
			msScenarioLabel(sc.ID), yesNo(sc.DNSEnabled), yesNo(sc.DHCPEnabled),
			ddi.NIOSCount, ddi.NativeCount, ddi.Tokens,
			ip.NIOSCount, ip.NativeCount, ip.Tokens,
			asset.Tokens, sc.EffectiveTokens, sc.DeltaTokens, marker,
		}
		if marker == "Selected" {
			cell, _ := excelize.CoordinatesToCellName(1, row)
			styled := make([]interface{}, len(values))
			for i, v := range values {
				styled[i] = excelize.Cell{StyleID: styles.total, Value: v}
			}
			if err := sw.SetRow(cell, styled); err != nil {
				return err
			}
			row++
			continue
		}
		if err := setRow(
			values[0], values[1], values[2],
			n(ddi.NIOSCount), n(ddi.NativeCount), n(ddi.Tokens),
			n(ip.NIOSCount), n(ip.NativeCount), n(ip.Tokens),
			n(asset.Tokens), n(sc.EffectiveTokens), n(sc.DeltaTokens), values[12],
		); err != nil {
			return err
		}
	}

	// Block two: the all-NIOS baseline figure.
	if err := blank(); err != nil {
		return err
	}
	if err := setRow(
		excelize.Cell{StyleID: styles.total, Value: "All-NIOS Baseline Tokens"},
		excelize.Cell{StyleID: styles.total, Value: ma.BaselineTokens},
	); err != nil {
		return err
	}

	// Block three: raw relationship evidence (D-06) — never a token contribution.
	if err := blank(); err != nil {
		return err
	}
	if err := setRow(excelize.Cell{StyleID: styles.sectionHeader, Value: "Raw Relationship Evidence"}); err != nil {
		return err
	}
	if err := setRow(excelize.Cell{
		StyleID: styles.assumption,
		Value:   "These counts describe the backup's relationship rows. They do not contribute to any token figure above.",
	}); err != nil {
		return err
	}
	if err := setRow("Relationship rows observed", n(ma.Evidence.RelationshipRows)); err != nil {
		return err
	}
	if err := setRow("Duplicate relationship rows", n(ma.Evidence.DuplicateRelationshipRows)); err != nil {
		return err
	}
	if err := setRow("Relationship anomalies", n(ma.Evidence.RelationshipAnomalies)); err != nil {
		return err
	}

	return sw.Flush()
}
