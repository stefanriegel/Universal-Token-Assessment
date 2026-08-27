// Package exporter renders an assessment report as an xlsx workbook.
//
// The types in this file are the input contract. Their JSON tags are the wire
// format shared with the frontend export payload — see server/types.go.
//
// Build is the package's single entry point; it writes a valid OOXML workbook
// to the supplied io.Writer using excelize StreamWriter (no disk writes).
package exporter

import (
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
)

// ReportInput is everything the workbook renders. It is self-contained: the
// exporter performs no lookups and holds no reference to scan state, so an
// imported session file and a live scan produce workbooks the same way.
//
// Count fields are effective — the caller has already applied the user's count
// and per-server overrides. Totals are as displayed, growth buffer included.
type ReportInput struct {
	GeneratedAt       time.Time               `json:"generatedAt"`
	SelectedProviders []string                `json:"selectedProviders"`
	Findings          []calculator.FindingRow `json:"findings"`

	Totals            TokenTotals    `json:"totals"`
	ProviderTotals    map[string]int `json:"providerTotals,omitempty"`
	TotalServerTokens int            `json:"totalServerTokens,omitempty"`
	ReportingTokens   int            `json:"reportingTokens,omitempty"`

	NiosServerMetrics []NiosServerMetricFull `json:"niosServerMetrics,omitempty"`
	// NiosServerObjectOverrides is keyed by memberId. Map presence preserves an
	// explicit zero, while absence keeps legacy serverObjectCount fallback.
	NiosServerObjectOverrides map[string]int    `json:"niosServerObjectOverrides,omitempty"`
	ADServerMetrics           []ADServerMetric  `json:"adServerMetrics,omitempty"`
	NiosMigrationMap          map[string]string `json:"niosMigrationMap,omitempty"`
	ADMigrationMap            map[string]string `json:"adMigrationMap,omitempty"`

	GrowthBufferPct       float64        `json:"growthBufferPct"`
	ServerGrowthBufferPct float64        `json:"serverGrowthBufferPct"`
	VariantOverrides      map[string]int `json:"variantOverrides,omitempty"`

	Errors               []ProviderError       `json:"errors,omitempty"`
	NiosMicrosoftServers *NiosMicrosoftServers `json:"niosMicrosoftServers,omitempty"`
	NiosMigrationFlags   *NiosMigrationFlags   `json:"niosMigrationFlags,omitempty"`

	MicrosoftAllocation *MicrosoftAllocation `json:"microsoftAllocation,omitempty"`
	// SelectedMSScenario carries the user's on-screen allocation switch
	// choice. Client state only — it has no server-response counterpart and
	// exists solely on the export payload and the saved session file.
	SelectedMSScenario string `json:"selectedMSScenario,omitempty"`
}

// NiosServerMetricFull is one NIOS Grid member as sized by the report.
// Counts are effective: any per-member overrides the user applied on screen are
// already folded in by the caller.
type NiosServerMetricFull struct {
	MemberID          string          `json:"memberId"`
	MemberName        string          `json:"memberName"`
	Role              string          `json:"role"`
	Model             string          `json:"model"`
	Platform          string          `json:"platform"`
	QPS               int             `json:"qps"`
	LPS               int             `json:"lps"`
	ObjectCount       int             `json:"objectCount"`
	ServerObjectCount int             `json:"serverObjectCount"`
	ActiveIPCount     int             `json:"activeIPCount"`
	ManagedIPCount    int             `json:"managedIPCount"`
	StaticHosts       int             `json:"staticHosts"`
	DynamicHosts      int             `json:"dynamicHosts"`
	DHCPUtilization   int             `json:"dhcpUtilization"`
	RunsDnsDhcp       bool            `json:"runsDnsDhcp"`
	Licenses          map[string]bool `json:"licenses,omitempty"`
}

// ADServerMetric is one Active Directory domain controller as sized by the report.
type ADServerMetric struct {
	Hostname                string `json:"hostname"`
	DNSObjects              int    `json:"dnsObjects"`
	DHCPObjects             int    `json:"dhcpObjects"`
	DHCPObjectsWithOverhead int    `json:"dhcpObjectsWithOverhead"`
	QPS                     int    `json:"qps"`
	LPS                     int    `json:"lps"`
	FormFactor              string `json:"formFactor"`
	Tier                    string `json:"tier"`
	ServerTokens            int    `json:"serverTokens"`
}

// NiosMicrosoftServer is a Windows DNS/DHCP server managed remotely by the NIOS
// Grid. Informational only — these objects are already counted in the Grid data.
type NiosMicrosoftServer struct {
	FQDN        string `json:"fqdn"`
	Address     string `json:"address"`
	OS          string `json:"os"`
	ADDomain    string `json:"adDomain"`
	DNSManaged  bool   `json:"dnsManaged"`
	DHCPManaged bool   `json:"dhcpManaged"`
	DHCPHosts   int    `json:"dhcpHosts"`
	ReadOnly    bool   `json:"readOnly"`
}

type NiosMicrosoftServers struct {
	Servers      []NiosMicrosoftServer `json:"servers"`
	ManagedZones int                   `json:"managedZones"`
}

// MSAllocationDiagnostic reports whether a Microsoft allocation snapshot is
// available for this scan, and if not, why. Independent redefinition of
// internal/scanner/nios.MSAllocationDiagnostic — connected only by matching
// JSON tags, never a shared import.
type MSAllocationDiagnostic struct {
	Available bool   `json:"available"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// MSCategoryTokens reports one token category's raw counts, rates, and exact
// unrounded subtotals. Independent redefinition of
// internal/scanner/nios.MSCategoryTokens.
type MSCategoryTokens struct {
	Category          string `json:"category"`
	NIOSCount         int    `json:"niosCount"`
	NIOSRate          int    `json:"niosRate"`
	NativeCount       int    `json:"nativeCount"`
	NativeRate        int    `json:"nativeRate"`
	NIOSSubtotalNum   int    `json:"niosSubtotalNum"`
	NativeSubtotalNum int    `json:"nativeSubtotalNum"`
	SubtotalDen       int    `json:"subtotalDen"`
	Tokens            int    `json:"tokens"`
}

// MSAllocationScenario is one of the four derived Microsoft allocation
// states. Independent redefinition of
// internal/scanner/nios.MSAllocationScenario. Categories is a three-element
// array, not a slice, so the category ordering and arity are part of the type.
type MSAllocationScenario struct {
	ID              string              `json:"id"`
	DNSEnabled      bool                `json:"dnsEnabled"`
	DHCPEnabled     bool                `json:"dhcpEnabled"`
	Categories      [3]MSCategoryTokens `json:"categories"`
	EffectiveTokens int                 `json:"effectiveTokens"`
	DeltaTokens     int                 `json:"deltaTokens"`
}

// MSEvidenceCounts holds raw relationship metrics that never participate in
// any token figure. Independent redefinition of
// internal/scanner/nios.MSEvidenceCounts.
type MSEvidenceCounts struct {
	RelationshipRows          int `json:"relationshipRows"`
	DuplicateRelationshipRows int `json:"duplicateRelationshipRows"`
	RelationshipAnomalies     int `json:"relationshipAnomalies"`
}

// MicrosoftAllocation is the wire shape of the Microsoft allocation snapshot
// derived from a NIOS backup scan. Independent redefinition of
// internal/scanner/nios.MSAllocationScenarioSet plus its sibling evidence
// field — this copy, server/types.go's copy, and the TypeScript client's
// copy are connected only by matching JSON names.
type MicrosoftAllocation struct {
	Diagnostic     MSAllocationDiagnostic `json:"diagnostic"`
	BaselineTokens int                    `json:"baselineTokens"`
	Scenarios      []MSAllocationScenario `json:"scenarios"`
	Evidence       MSEvidenceCounts       `json:"evidence"`
}

// NiosMigrationFlags collects configuration that needs attention before a
// migration off NIOS.
type NiosMigrationFlags struct {
	DHCPOptions []DHCPOptionFlag `json:"dhcpOptions"`
	HostRoutes  []HostRouteFlag  `json:"hostRoutes"`
}

type DHCPOptionFlag struct {
	Network      string `json:"network"`
	OptionNumber int    `json:"optionNumber"`
	OptionName   string `json:"optionName"`
	OptionType   string `json:"optionType"`
	Flag         string `json:"flag"`
	Member       string `json:"member"`
}

type HostRouteFlag struct {
	Network string `json:"network"`
	Member  string `json:"member"`
}

// ProviderError is one per-resource failure recorded during a scan. It mirrors
// session.ProviderError but carries JSON tags, because it now crosses the wire.
type ProviderError struct {
	Provider string `json:"provider"`
	Resource string `json:"resource"`
	Message  string `json:"message"`
}

// TokenTotals are the aggregate management-token numbers exactly as the report
// displays them, with the growth buffer already applied. The exporter renders
// these values; it must not recompute them from findings.
type TokenTotals struct {
	DDITokens   int `json:"ddiTokens"`
	IPTokens    int `json:"ipTokens"`
	AssetTokens int `json:"assetTokens"`
	GrandTotal  int `json:"grandTotal"`
}
