package vuln

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
)

func TestBuildScanInsights(t *testing.T) {
	vulns := []vulnerability.Vulnerability{
		{VulnerabilityID: "CVE-LOW", PkgName: "zlib", InstalledVersion: "1.0.0", Severity: vulnerability.SeverityLow, CVSS: &vulnerability.CVSSInfo{V3Score: 3}},
		{VulnerabilityID: "CVE-HIGH-1", PkgName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.2", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V3Score: 7.5}},
		{VulnerabilityID: "CVE-HIGH-2", PkgName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.10", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V2Score: 7.8}},
		{VulnerabilityID: "CVE-MED", PkgName: "curl", InstalledVersion: "8.0.0", Severity: vulnerability.SeverityMedium},
	}

	insights := BuildScanInsights(vulns, nil, vulnerability.ImageExposureUnknown)

	require.Equal(t, "CVE-HIGH-2", insights.HighestVulnerabilityID)
	require.InDelta(t, 7.8, insights.HighestCVSS, 0.001)
	require.Equal(t, 2, insights.FixableCount)
	// Only the two fixable findings count: 0.7 x 54.6 + 0.15 x 52.5 plus a sliver for volume, with no threat data and unknown usage.
	require.Equal(t, 46, insights.RiskScore)
	require.Equal(t, vulnerability.RiskBandMedium, insights.RiskBand)
	require.Equal(t, vulnerability.ScoreStatusComplete, insights.ScoreStatus)

	require.Len(t, insights.TopPackages, 3)
	require.Equal(t, vulnerability.PackageInsight{PkgName: "openssl", Severity: vulnerability.SeverityHigh, Count: 2, InstalledVersion: "3.0.0", FixedVersion: "3.0.10"}, insights.TopPackages[0])
	require.Equal(t, "curl", insights.TopPackages[1].PkgName)
	require.Equal(t, "zlib", insights.TopPackages[2].PkgName)
}

func TestBuildScanInsights_VolumeSaturatesWithDiminishingReturns(t *testing.T) {
	build := func(n int, severity vulnerability.Severity, cvss float64, knownExploited bool) ([]vulnerability.Vulnerability, map[string]ThreatIntel) {
		vulns := make([]vulnerability.Vulnerability, 0, n)
		intel := map[string]ThreatIntel{}
		for i := range n {
			id := fmt.Sprintf("CVE-%d", i)
			vulns = append(vulns, vulnerability.Vulnerability{VulnerabilityID: id, PkgName: fmt.Sprintf("pkg-%d", i), FixedVersion: "1.0.1", Severity: severity, CVSS: &vulnerability.CVSSInfo{V3Score: cvss}})
			intel[id] = ThreatIntel{KnownExploited: knownExploited, EPSS: new(0.0)}
		}
		return vulns, intel
	}

	oneVulns, oneIntel := build(1, vulnerability.SeverityCritical, 9.8, true)
	manyVulns, manyIntel := build(8, vulnerability.SeverityCritical, 9.8, true)
	lowVulns, lowIntel := build(8, vulnerability.SeverityLow, 3, false)
	one := BuildScanInsights(oneVulns, oneIntel, vulnerability.ImageExposureRunning)
	many := BuildScanInsights(manyVulns, manyIntel, vulnerability.ImageExposureRunning)
	lows := BuildScanInsights(lowVulns, lowIntel, vulnerability.ImageExposureRunning)
	// One running KEV lands at the High floor; seven more add their shrinking shares and volume, and eight lows stay Low.
	require.Equal(t, 70, one.RiskScore)
	require.Equal(t, vulnerability.RiskBandHigh, one.RiskBand)
	require.Equal(t, 93, many.RiskScore)
	require.Equal(t, vulnerability.RiskBandCritical, many.RiskBand)
	require.Equal(t, 21, lows.RiskScore)
	require.Equal(t, vulnerability.RiskBandLow, lows.RiskBand)
	require.Equal(t, vulnerability.ScoreStatusComplete, many.ScoreStatus)
	require.Len(t, many.TopPackages, TopPackageInsightLimit)
}

func TestBuildScanInsights_NoFixableFindings(t *testing.T) {
	vulns := []vulnerability.Vulnerability{
		{VulnerabilityID: "CVE-KEV", PkgName: "openssl", Severity: vulnerability.SeverityCritical, CVSS: &vulnerability.CVSSInfo{V3Score: 9.8}},
	}
	intel := map[string]ThreatIntel{"CVE-KEV": {KnownExploited: true}}

	insights := BuildScanInsights(vulns, intel, vulnerability.ImageExposureRunning)

	// An unfixable KEV stays visible but does not set a patch priority.
	require.Equal(t, 1, insights.KnownExploitedCount)
	require.Zero(t, insights.RiskScore)
	require.Equal(t, vulnerability.RiskBandNone, insights.RiskBand)
	require.Equal(t, vulnerability.ScoreStatusComplete, insights.ScoreStatus)
}

func TestBuildScanInsights_DeduplicatesFixableFindings(t *testing.T) {
	vulns := []vulnerability.Vulnerability{
		{VulnerabilityID: "CVE-DUP", PkgName: "libssl", FixedVersion: "3.0.1", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V3Score: 8}},
		{VulnerabilityID: "CVE-DUP", PkgName: "openssl", FixedVersion: "3.0.1", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V3Score: 8}},
		{VulnerabilityID: "CVE-DUP", PkgName: "openssl-dev", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V3Score: 8}},
		{VulnerabilityID: "CVE-LOW", PkgName: "zlib", FixedVersion: "1.3", Severity: vulnerability.SeverityLow, CVSS: &vulnerability.CVSSInfo{V3Score: 2}},
	}
	intel := map[string]ThreatIntel{
		"CVE-DUP": {EPSS: new(0.0)},
		"CVE-LOW": {EPSS: new(0.0)},
	}

	insights := BuildScanInsights(vulns, intel, vulnerability.ImageExposureRunning)

	// CVE-DUP counts once at 56 and CVE-LOW at 14 takes the second rank: the three package rows do not stack.
	require.Equal(t, 42, insights.RiskScore)
	require.Equal(t, 3, insights.FixableCount)
	require.Equal(t, vulnerability.ScoreStatusComplete, insights.ScoreStatus)
}

func TestBuildScanInsights_UnknownSeverity(t *testing.T) {
	unscored := []vulnerability.Vulnerability{
		{VulnerabilityID: "CVE-UNK", PkgName: "foo", FixedVersion: "2.0", Severity: vulnerability.SeverityUnknown},
	}
	insights := BuildScanInsights(unscored, nil, vulnerability.ImageExposureRunning)
	require.Equal(t, vulnerability.ScoreStatusUnavailable, insights.ScoreStatus)
	require.Zero(t, insights.RiskScore)

	mixed := append(unscored, vulnerability.Vulnerability{VulnerabilityID: "CVE-KEV", PkgName: "bar", FixedVersion: "1.1", Severity: vulnerability.SeverityMedium, CVSS: &vulnerability.CVSSInfo{V3Score: 6}})
	intel := map[string]ThreatIntel{"CVE-KEV": {KnownExploited: true}}
	insights = BuildScanInsights(mixed, intel, vulnerability.ImageExposureRunning)
	// The unscored finding is left out of the score.
	require.Equal(t, 70, insights.RiskScore)
	require.Equal(t, vulnerability.ScoreStatusComplete, insights.ScoreStatus)
}

func TestBuildScanInsights_NonCVEAdvisories(t *testing.T) {
	vulns := []vulnerability.Vulnerability{
		{VulnerabilityID: "GHSA-xxxx-yyyy-zzzz", PkgName: "lodash", FixedVersion: "4.17.21", Severity: vulnerability.SeverityHigh, CVSS: &vulnerability.CVSSInfo{V3Score: 7.5}},
		{VulnerabilityID: "DLA-2424-1", PkgName: "tzdata", FixedVersion: "2020d-0+deb9u1", Severity: vulnerability.SeverityUnknown},
		{VulnerabilityID: "CVE-KEV", PkgName: "openssl", FixedVersion: "3.0.1", Severity: vulnerability.SeverityMedium, CVSS: &vulnerability.CVSSInfo{V3Score: 6}},
	}
	intel := map[string]ThreatIntel{"CVE-KEV": {KnownExploited: true}}

	insights := BuildScanInsights(vulns, intel, vulnerability.ImageExposureRunning)

	// KEV and EPSS never cover GHSA/DLA IDs: the GHSA scores with the no-data factor and the unrated DLA is skipped.
	require.Equal(t, 3, insights.FixableCount)
	require.Equal(t, 78, insights.RiskScore)
	require.Equal(t, vulnerability.ScoreStatusComplete, insights.ScoreStatus)

	unrated := BuildScanInsights(vulns[1:2], nil, vulnerability.ImageExposureRunning)
	require.Zero(t, unrated.RiskScore)
	require.Equal(t, vulnerability.ScoreStatusComplete, unrated.ScoreStatus)
}

func TestFindingPriority(t *testing.T) {
	zeroEPSS, lowEPSS, highEPSS := 0.0, 0.01, 0.97
	tests := []struct {
		name           string
		base           float64
		knownExploited bool
		epss           *float64
		exposure       vulnerability.ImageExposure
		want           float64
	}{
		{name: "no intel running", base: 7.5, exposure: vulnerability.ImageExposureRunning, want: 52.5},
		{name: "epss zero", base: 7.5, epss: &zeroEPSS, exposure: vulnerability.ImageExposureRunning, want: 52.5},
		{name: "unknown exposure counts as running", base: 7.5, epss: &zeroEPSS, exposure: vulnerability.ImageExposureUnknown, want: 52.5},
		{name: "known exploited takes the maximum", base: 6, knownExploited: true, exposure: vulnerability.ImageExposureRunning, want: 100},
		{name: "low epss", base: 7.5, epss: &lowEPSS, exposure: vulnerability.ImageExposureRunning, want: 54.75},
		{name: "high epss", base: 6, epss: &highEPSS, exposure: vulnerability.ImageExposureRunning, want: 60 * (0.7 + 0.3*math.Sqrt(0.97))},
		{name: "stopped", base: 5, knownExploited: true, exposure: vulnerability.ImageExposureStopped, want: 80},
		{name: "unused", base: 9.8, knownExploited: true, exposure: vulnerability.ImageExposureUnused, want: 50},
		{name: "maximum", base: 10, knownExploited: true, exposure: vulnerability.ImageExposureRunning, want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, FindingPriority(tt.base, ThreatIntel{KnownExploited: tt.knownExploited, EPSS: tt.epss}, tt.exposure), 0.001)
		})
	}
}

func TestBuildScanInsights_ThreatIntelAndExposure(t *testing.T) {
	vulns := []vulnerability.Vulnerability{
		{VulnerabilityID: "CVE-KEV", PkgName: "openssl", FixedVersion: "3.0.1", Severity: vulnerability.SeverityMedium, CVSS: &vulnerability.CVSSInfo{V3Score: 6}},
		{VulnerabilityID: "CVE-OTHER", PkgName: "zlib", FixedVersion: "1.3", Severity: vulnerability.SeverityLow, CVSS: &vulnerability.CVSSInfo{V3Score: 3}},
	}
	intel := map[string]ThreatIntel{"CVE-KEV": {KnownExploited: true}}

	running := BuildScanInsights(vulns, intel, vulnerability.ImageExposureRunning)
	require.Equal(t, 1, running.KnownExploitedCount)
	require.Equal(t, vulnerability.ImageExposureRunning, running.Exposure)
	require.InDelta(t, 6.0, running.HighestCVSS, 0.001)
	// The KEV counts at 100 and CVE-OTHER, with no threat data, at 21 takes the second rank.
	require.Equal(t, 74, running.RiskScore)
	require.Equal(t, vulnerability.ScoreStatusComplete, running.ScoreStatus)

	unused := BuildScanInsights(vulns, intel, vulnerability.ImageExposureUnused)
	require.Equal(t, 37, unused.RiskScore)
	require.Less(t, unused.RiskScore, running.RiskScore)
}
