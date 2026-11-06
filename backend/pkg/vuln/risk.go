package vuln

import (
	"maps"
	"math"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"kit.getarcane.app"

	"getarcane.dev/types/v3"
)

type (
	ThreatIntel struct {
		EPSS           *float64
		KnownExploited bool
		KEVOverdue     bool
	}

	RiskScore struct {
		scores                  map[string]float64
		weights                 map[string]float64
		unscored                map[string]struct{}
		peak                    float64
		exploitedRunning        int
		overdueExploitedRunning int
	}
)

const (
	ScoringVersion         = 4
	TopPackageInsightLimit = 5
	threatFactorNoData     = 0.7
	usageFactorStopped     = 0.8
	usageFactorUnused      = 0.5
	concentrationPenalty   = 8
	concentrationScale     = 40
	exposureWeightRunning  = 3
	exposureWeightStopped  = 2
	peakShare              = 0.6
	// exploitedFloor*Internal set the lowest environment score while fixable known-exploited findings are running:
	// the base for one image, a step per additional image, a bump once any is past its KEV due date, and a cap.
	exploitedFloorBase     = 70
	exploitedFloorPerImage = 4
	exploitedFloorOverdue  = 10
	exploitedFloorMax      = 95
)

var rankWeights = [...]float64{0.70, 0.15, 0.05, 0.02}

func CVSSScore(cvss *types.CVSSInfo) float64 {
	if cvss == nil {
		return 0
	}
	if cvss.V3Score > 0 {
		return cvss.V3Score
	}
	return cvss.V2Score
}

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

func FindingPriority(base float64, intel ThreatIntel, exposure types.ImageExposure) float64 {
	threat := threatFactorNoData
	switch {
	case intel.KnownExploited:
		base, threat = 10, 1
	case intel.EPSS != nil:
		threat = threatFactorNoData + (1-threatFactorNoData)*math.Sqrt(*intel.EPSS)
	}
	usage := 1.0
	switch exposure {
	case types.ImageExposureStopped:
		usage = usageFactorStopped
	case types.ImageExposureUnused:
		usage = usageFactorUnused
	case types.ImageExposureRunning, types.ImageExposureUnknown:
	}
	return min(100, 10*base*threat*usage)
}

func (r *RiskScore) AddFinding(vulnerabilityID string, fixable bool, base float64, intel ThreatIntel, exposure types.ImageExposure) float64 {
	priority := FindingPriority(base, intel, exposure)
	if !fixable || (priority <= 0 && !strings.HasPrefix(strings.ToUpper(vulnerabilityID), "CVE-")) {
		return priority
	}
	status := kit.Ternary(priority <= 0, types.VulnerabilityScoreStatusUnavailable, types.VulnerabilityScoreStatusComplete)
	r.addScore(vulnerabilityID, priority, 1, status)
	if intel.KnownExploited && (exposure == types.ImageExposureRunning || exposure == types.ImageExposureUnknown) {
		r.exploitedRunning = 1
		if intel.KEVOverdue {
			r.overdueExploitedRunning = 1
		}
	}
	return priority
}

func (r *RiskScore) AddImage(imageID string, image *RiskScore, exposure types.ImageExposure) {
	weight := 1.0
	switch exposure {
	case types.ImageExposureRunning:
		weight = exposureWeightRunning
	case types.ImageExposureStopped:
		weight = exposureWeightStopped
	case types.ImageExposureUnused, types.ImageExposureUnknown:
	}
	score, priority := image.ImageScore()
	r.addScore(imageID, score, weight, priority.ScoreStatus)
	if priority.ScoreStatus != types.VulnerabilityScoreStatusUnavailable {
		r.peak = max(r.peak, score)
	}
	r.exploitedRunning += image.exploitedRunning
	r.overdueExploitedRunning += image.overdueExploitedRunning
}

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

func (r *RiskScore) Scored() int {
	return len(r.scores)
}

func (r *RiskScore) ImageScore() (float64, types.VulnerabilityPatchPriority) {
	values := slices.Sorted(maps.Values(r.scores))
	slices.Reverse(values)
	total := 0.0
	for i, score := range values[:min(len(values), len(rankWeights))] {
		total += rankWeights[i] * score
	}
	if len(values) > 0 {
		total += concentrationPenalty * (1 - math.Exp(-float64(len(values))/concentrationScale))
	}
	return total, r.result(total)
}

func (r *RiskScore) EnvironmentScore() (float64, types.VulnerabilityPatchPriority) {
	basis := r.Basis()
	total := basis.Score + peakShare*max(0, basis.Peak-basis.Score)
	if basis.ExploitedRunningImages > 0 {
		floor := exploitedFloorBase + exploitedFloorPerImage*float64(basis.ExploitedRunningImages-1)
		if basis.OverdueExploitedRunningImages > 0 {
			floor += exploitedFloorOverdue
		}
		total = max(total, min(floor, exploitedFloorMax))
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
		Score:                         average,
		Weight:                        weight,
		Peak:                          r.peak,
		ExploitedRunningImages:        r.exploitedRunning,
		OverdueExploitedRunningImages: r.overdueExploitedRunning,
	}
}

func (r *RiskScore) AddEnvironment(environmentID string, basis types.RiskScoreBasis) {
	if basis.Weight > 0 {
		r.addScore(environmentID, basis.Score, basis.Weight, types.VulnerabilityScoreStatusComplete)
	}
	r.peak = max(r.peak, basis.Peak)
	r.exploitedRunning += basis.ExploitedRunningImages
	r.overdueExploitedRunning += basis.OverdueExploitedRunningImages
}

func (r *RiskScore) result(total float64) types.VulnerabilityPatchPriority {
	result := types.VulnerabilityPatchPriority{RiskScore: int(math.Round(total)), ScoreStatus: types.VulnerabilityScoreStatusComplete}
	result.RiskBand = RiskBand(result.RiskScore)
	if len(r.scores) == 0 && len(r.unscored) > 0 {
		result.ScoreStatus = types.VulnerabilityScoreStatusUnavailable
	}
	return result
}

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

func isNewerFixedVersion(candidate, current string) bool {
	if current == "" {
		return true
	}
	a, b := kit.EnsurePrefix(candidate, "v"), kit.EnsurePrefix(current, "v")
	return semver.IsValid(a) && semver.IsValid(b) && semver.Compare(a, b) > 0
}

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
