package vuln

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/mod/semver"
)

const (
	// ScoringVersion tags risk snapshots; trends only read the current formula.
	ScoringVersion = 3
	// TopPackageInsightLimit caps the packages listed in scan insights.
	TopPackageInsightLimit = 5

	threatFactorNoDataInternal = 0.7
	usageFactorStoppedInternal = 0.8
	usageFactorUnusedInternal  = 0.5
	// concentrationPenaltyInternal is the most the fixable finding count adds to an image; it saturates over concentrationScaleInternal findings.
	concentrationPenaltyInternal = 8
	concentrationScaleInternal   = 40
	// exposureWeight*Internal set how much a scanned image counts in the environment average; unused and unknown images count once.
	exposureWeightRunningInternal = 3
	exposureWeightStoppedInternal = 2
	// exploitedRunningFloorInternal is the lowest environment score while a fixable known-exploited finding is running.
	exploitedRunningFloorInternal = 70
)

// rankWeightsInternal is the share of each of an image's worst fixable findings that enters its score, worst first.
var rankWeightsInternal = [...]float64{0.70, 0.15, 0.05, 0.02}

// ThreatIntel is the exploitation evidence known for one CVE.
type ThreatIntel struct {
	KnownExploited bool
	EPSS           *float64
}

// CVSSScore returns the CVSS v3 score, else v2, else 0.
func CVSSScore(cvss *types.CVSSInfo) float64 {
	if cvss == nil {
		return 0
	}
	if cvss.V3Score > 0 {
		return cvss.V3Score
	}
	return cvss.V2Score
}

// BaseScore returns the CVSS score, falling back to a severity midpoint when unscored.
func BaseScore(cvss float64, severity types.VulnerabilitySeverity) float64 {
	if cvss > 0 {
		return cvss
	}
	switch severity {
	case types.VulnerabilitySeverityCritical:
		return 9.5
	case types.VulnerabilitySeverityHigh:
		return 8
	case types.VulnerabilitySeverityMedium:
		return 5.5
	case types.VulnerabilitySeverityLow:
		return 2.5
	case types.VulnerabilitySeverityUnknown:
		return 0
	default:
		return 0
	}
}

// SeverityRank orders severities from unknown (0) to critical (4).
func SeverityRank(severity types.VulnerabilitySeverity) int {
	switch severity {
	case types.VulnerabilitySeverityCritical:
		return 4
	case types.VulnerabilitySeverityHigh:
		return 3
	case types.VulnerabilitySeverityMedium:
		return 2
	case types.VulnerabilitySeverityLow:
		return 1
	case types.VulnerabilitySeverityUnknown:
		return 0
	default:
		return 0
	}
}

// SeveritySummary counts findings per severity.
func SeveritySummary(vulns []types.Vulnerability) *types.VulnerabilitySeveritySummary {
	summary := &types.VulnerabilitySeveritySummary{}
	for _, v := range vulns {
		switch v.Severity {
		case types.VulnerabilitySeverityCritical:
			summary.Critical++
		case types.VulnerabilitySeverityHigh:
			summary.High++
		case types.VulnerabilitySeverityMedium:
			summary.Medium++
		case types.VulnerabilitySeverityLow:
			summary.Low++
		case types.VulnerabilitySeverityUnknown:
			summary.Unknown++
		default:
			summary.Unknown++
		}
		summary.Total++
	}
	return summary
}

// IsCVE reports whether an advisory ID can carry KEV/EPSS data; GHSA, DLA, DSA and similar IDs never do.
func IsCVE(vulnerabilityID string) bool {
	return strings.HasPrefix(strings.ToUpper(vulnerabilityID), "CVE-")
}

// FindingPriority maps a 0-10 severity onto a 0-100 patch priority using exploitation evidence and container usage.
// A known-exploited finding takes the maximum rating regardless of CVSS, as RS³ does for threats seen in the wild.
func FindingPriority(base float64, intel ThreatIntel, exposure types.ImageExposure) float64 {
	threat := threatFactorNoDataInternal
	switch {
	case intel.KnownExploited:
		base, threat = 10, 1
	case intel.EPSS != nil:
		threat = threatFactorNoDataInternal + (1-threatFactorNoDataInternal)*math.Sqrt(*intel.EPSS)
	}
	usage := 1.0
	switch exposure {
	case types.ImageExposureStopped:
		usage = usageFactorStoppedInternal
	case types.ImageExposureUnused:
		usage = usageFactorUnusedInternal
	case types.ImageExposureRunning, types.ImageExposureUnknown:
	}
	return min(100, 10*base*threat*usage)
}

// RiskScore collects scored members keyed by ID: distinct fixable findings for an image, scanned images for an environment.
type RiskScore struct {
	scores   map[string]float64
	weights  map[string]float64
	unscored map[string]struct{}
	// exploitedRunning records a fixable known-exploited finding on a running (or unknown) image.
	exploitedRunning bool
}

// AddFinding scores one finding occurrence, records it when fixable, and returns its 0-100 priority.
// An unrated non-CVE advisory is left out entirely: it has no CVSS to score and no rating to wait for.
func (r *RiskScore) AddFinding(vulnerabilityID string, fixable bool, base float64, intel ThreatIntel, exposure types.ImageExposure) float64 {
	priority := FindingPriority(base, intel, exposure)
	if !fixable || (priority <= 0 && !IsCVE(vulnerabilityID)) {
		return priority
	}
	status := kit.Ternary(priority <= 0, types.VulnerabilityScoreStatusUnavailable, types.VulnerabilityScoreStatusComplete)
	r.addScore(vulnerabilityID, priority, 1, status)
	if intel.KnownExploited && (exposure == types.ImageExposureRunning || exposure == types.ImageExposureUnknown) {
		r.exploitedRunning = true
	}
	return priority
}

// AddImage records a scanned image's score for the environment, weighted by how exposed the image is.
func (r *RiskScore) AddImage(imageID string, score float64, status types.VulnerabilityScoreStatus, exposure types.ImageExposure, exploitedRunning bool) {
	weight := 1.0
	switch exposure {
	case types.ImageExposureRunning:
		weight = exposureWeightRunningInternal
	case types.ImageExposureStopped:
		weight = exposureWeightStoppedInternal
	case types.ImageExposureUnused, types.ImageExposureUnknown:
	}
	r.addScore(imageID, score, weight, status)
	r.exploitedRunning = r.exploitedRunning || exploitedRunning
}

// addScore records a member's score; the highest score per ID wins and unavailable members stay out of the total.
func (r *RiskScore) addScore(id string, score, weight float64, status types.VulnerabilityScoreStatus) {
	if r.scores == nil {
		r.scores, r.weights, r.unscored = map[string]float64{}, map[string]float64{}, map[string]struct{}{}
	}
	if status == types.VulnerabilityScoreStatusUnavailable {
		if _, ok := r.scores[id]; !ok {
			r.unscored[id] = struct{}{}
		}
		return
	}
	delete(r.unscored, id)
	if current, ok := r.scores[id]; !ok || score > current {
		r.scores[id] = score
	}
	r.weights[id] = weight
}

// Scored returns how many members entered the score.
func (r *RiskScore) Scored() int {
	return len(r.scores)
}

// ExploitedRunning reports whether a fixable known-exploited finding was recorded on a running image.
func (r *RiskScore) ExploitedRunning() bool {
	return r.exploitedRunning
}

// ImageScore returns an image's unrounded 0-100 score and the rounded API score.
// Modeled on the RS³ security risk: the worst findings count with shrinking weights and the fixable count adds a little on top.
func (r *RiskScore) ImageScore() (float64, types.VulnerabilityPatchPriority) {
	values := slices.Sorted(maps.Values(r.scores))
	slices.Reverse(values)
	total := 0.0
	for i, score := range values[:min(len(values), len(rankWeightsInternal))] {
		total += rankWeightsInternal[i] * score
	}
	if len(values) > 0 {
		total += concentrationPenaltyInternal * (1 - math.Exp(-float64(len(values))/concentrationScaleInternal))
	}
	return total, r.result(total)
}

// EnvironmentScore returns the environment's unrounded 0-100 score and the rounded API score.
// Modeled on the overall RS³: an exposure-weighted average of image scores that stays in the High band while a fixable known-exploited finding is running.
func (r *RiskScore) EnvironmentScore() (float64, types.VulnerabilityPatchPriority) {
	basis := r.Basis()
	total := basis.Score
	if basis.ExploitedRunning {
		total = max(total, exploitedRunningFloorInternal)
	}
	return total, r.result(total)
}

func (r *RiskScore) Basis() types.RiskScoreBasis {
	sum, weight := 0.0, 0.0
	for id, score := range r.scores {
		sum += score * r.weights[id]
		weight += r.weights[id]
	}
	average := 0.0
	if weight > 0 {
		average = sum / weight
	}
	return types.RiskScoreBasis{
		Score:            average,
		Weight:           weight,
		ExploitedRunning: r.exploitedRunning,
	}
}

func (r *RiskScore) AddEnvironment(environmentID string, basis types.RiskScoreBasis) {
	if basis.Weight > 0 {
		r.addScore(environmentID, basis.Score, basis.Weight, types.VulnerabilityScoreStatusComplete)
	}
	r.exploitedRunning = r.exploitedRunning || basis.ExploitedRunning
}

// result rounds a score into the API shape; only unscorable members make it unavailable.
func (r *RiskScore) result(total float64) types.VulnerabilityPatchPriority {
	result := types.VulnerabilityPatchPriority{RiskScore: int(math.Round(total)), ScoreStatus: types.VulnerabilityScoreStatusComplete}
	result.RiskBand = RiskBand(result.RiskScore)
	if len(r.scores) == 0 && len(r.unscored) > 0 {
		result.ScoreStatus = types.VulnerabilityScoreStatusUnavailable
	}
	return result
}

// RiskBand labels a 0-100 score.
func RiskBand(score int) types.RiskBand {
	switch {
	case score >= 90:
		return types.RiskBandCritical
	case score >= 70:
		return types.RiskBandHigh
	case score >= 40:
		return types.RiskBandMedium
	case score > 0:
		return types.RiskBandLow
	default:
		return types.RiskBandNone
	}
}

// isNewerFixedVersion reports whether candidate supersedes current; non-semver fixes keep the first (most severe) one.
func isNewerFixedVersion(candidate, current string) bool {
	if current == "" {
		return true
	}
	a, b := kit.EnsurePrefix(candidate, "v"), kit.EnsurePrefix(current, "v")
	return semver.IsValid(a) && semver.IsValid(b) && semver.Compare(a, b) > 0
}

// BuildScanInsights scores an image's findings using threat intel (keyed by CVE) and the image's exposure.
func BuildScanInsights(vulns []types.Vulnerability, intel map[string]ThreatIntel, exposure types.ImageExposure) *types.VulnerabilityScanInsights {
	sorted := slices.Clone(vulns)
	slices.SortStableFunc(sorted, func(a, b types.Vulnerability) int {
		return SeverityRank(b.Severity) - SeverityRank(a.Severity)
	})

	insights := &types.VulnerabilityScanInsights{Exposure: exposure, TopPackages: []types.VulnerabilityPackageInsight{}}
	var score RiskScore
	packages := map[string]*types.VulnerabilityPackageInsight{}
	var packageOrder []string

	for _, v := range sorted {
		base := BaseScore(CVSSScore(v.CVSS), v.Severity)
		if insights.HighestVulnerabilityID == "" || base > insights.HighestCVSS {
			insights.HighestCVSS = base
			insights.HighestVulnerabilityID = v.VulnerabilityID
		}
		threat := intel[v.VulnerabilityID]
		if threat.KnownExploited {
			insights.KnownExploitedCount++
		}
		fixedVersion := strings.TrimSpace(v.FixedVersion)
		if fixedVersion != "" {
			insights.FixableCount++
		}
		score.AddFinding(v.VulnerabilityID, fixedVersion != "", base, threat, exposure)

		pkg, ok := packages[v.PkgName]
		if !ok {
			pkg = &types.VulnerabilityPackageInsight{PkgName: v.PkgName, Severity: v.Severity, InstalledVersion: v.InstalledVersion}
			packages[v.PkgName] = pkg
			packageOrder = append(packageOrder, v.PkgName)
		}
		pkg.Count++
		if fixedVersion != "" && isNewerFixedVersion(fixedVersion, pkg.FixedVersion) {
			pkg.FixedVersion = fixedVersion
		}
	}

	_, insights.VulnerabilityPatchPriority = score.ImageScore()

	for _, name := range packageOrder {
		insights.TopPackages = append(insights.TopPackages, *packages[name])
	}
	slices.SortStableFunc(insights.TopPackages, func(a, b types.VulnerabilityPackageInsight) int {
		if rank := SeverityRank(b.Severity) - SeverityRank(a.Severity); rank != 0 {
			return rank
		}
		return b.Count - a.Count
	})
	if len(insights.TopPackages) > TopPackageInsightLimit {
		insights.TopPackages = insights.TopPackages[:TopPackageInsightLimit]
	}

	return insights
}
