package canary

import "time"

// Fingerprint probe: an observe-only model-identity check based on the
// lm-detector number fingerprint (see internal/thirdparty/lmfpd). It flags
// accounts whose replies look like a different model than the one requested;
// it never changes routing, cooldown or enablement.

const (
	KindFingerprint = "fingerprint"

	VerdictMatch        = "match"        // top-1 is the claimed model
	VerdictFamily       = "family"       // top-1 is a sibling of the claimed model (tolerated)
	VerdictMismatch     = "mismatch"     // confident top-1 outside the claimed model/family
	VerdictInconclusive = "inconclusive" // ranked, but below the confidence threshold
	VerdictInsufficient = "insufficient" // fewer than the minimum valid numbers
)

// FingerprintCandidate is one ranked reference model.
type FingerprintCandidate struct {
	Model       string  `json:"model"`
	Family      string  `json:"family"`
	Score       float64 `json:"score"`
	Probability float64 `json:"probability"`
}

// FingerprintResult is one stored fingerprint probe (3 requests) of one account and model.
type FingerprintResult struct {
	ID                  uint64                 `json:"id"`
	RoundID             string                 `json:"roundId"`
	Trigger             string                 `json:"trigger"`
	AccountID           uint64                 `json:"accountId"`
	AccountName         string                 `json:"accountName"`
	Model               string                 `json:"model"`
	Status              string                 `json:"status"`
	Verdict             string                 `json:"verdict"`
	TopModel            string                 `json:"topModel"`
	TopFamily           string                 `json:"topFamily"`
	Confidence          float64                `json:"confidence"`
	Candidates          []FingerprintCandidate `json:"candidates"`
	ValidCounts         []int                  `json:"validCounts"`
	ValidOutputs        int                    `json:"validOutputs"`
	OutputTokens        int64                  `json:"outputTokens"`
	DurationMS          int64                  `json:"durationMs"`
	ConsecutiveMismatch int                    `json:"consecutiveMismatches"`
	Suspect             bool                   `json:"suspectSubstituted"`
	Reasons             string                 `json:"reasons"`
	Error               string                 `json:"error,omitempty"`
	RequestIDs          string                 `json:"requestIds"`
	Prompts             []string               `json:"prompts,omitempty"`
	RawOutputs          []string               `json:"rawOutputs,omitempty"`
	CreatedAt           time.Time              `json:"createdAt"`
}

// FingerprintPolicy holds the judging thresholds.
type FingerprintPolicy struct {
	MinConfidence  float64
	FamilyTolerant bool
	SuspectAfter   int
}

// JudgeFingerprint fills Status/Verdict/Reasons from the ranking already set
// on result (TopModel, TopFamily, Confidence, ValidOutputs). claimedFamily is
// the reference family of the requested model ("" when not in the bank).
func JudgeFingerprint(result *FingerprintResult, claimedFamily string, policy FingerprintPolicy) {
	switch {
	case result.ValidOutputs == 0 || result.TopModel == "":
		result.Status, result.Verdict = StatusSkipped, VerdictInsufficient
		if result.Reasons == "" {
			result.Reasons = "有效数字不足 80 个，无法判定"
		}
	case result.TopModel == result.Model:
		result.Status, result.Verdict, result.Reasons = StatusPass, VerdictMatch, ""
	case policy.FamilyTolerant && claimedFamily != "" && result.TopFamily == claimedFamily:
		result.Status, result.Verdict, result.Reasons = StatusPass, VerdictFamily, "同家族近似（"+result.TopModel+"）"
	case result.Confidence < policy.MinConfidence:
		result.Status, result.Verdict, result.Reasons = StatusSkipped, VerdictInconclusive, "置信度不足，最像 "+result.TopModel
	default:
		result.Status, result.Verdict, result.Reasons = StatusFail, VerdictMismatch, "疑似换模型，最像 "+result.TopModel
	}
}

// ConsecutiveMismatches counts leading judged mismatches in newest-first history of one model.
func ConsecutiveMismatches(newestFirst []FingerprintResult) int {
	count := 0
	for _, value := range newestFirst {
		if !Judged(value.Status) {
			continue
		}
		if value.Status != StatusFail {
			break
		}
		count++
	}
	return count
}

// FingerprintPoolPasses reports whether the other accounts' judged results for
// the same model mostly pass. pool should hold at most one (latest) result per account.
func FingerprintPoolPasses(pool []FingerprintResult, accountID uint64, model string) bool {
	judged, passed := 0, 0
	for _, value := range pool {
		if value.AccountID == accountID || value.Model != model || !Judged(value.Status) {
			continue
		}
		judged++
		if value.Status == StatusPass {
			passed++
		}
	}
	return judged > 0 && passed*2 >= judged
}
