// Package canary defines the observe-only account canary probe: a fixed,
// rotating prompt set sent to one pinned Build account at a time and scored
// against the pool. Results never affect routing, cooldown or enablement.
package canary

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	KindReasoning = "reasoning"
	KindSVG       = "svg"

	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusError   = "error"   // transport/upstream failure: not counted
	StatusSkipped = "skipped" // model hides reasoning: cannot be judged

	// SuspectAfterFails is the consecutive judged failures needed before an
	// account is reported as suspect, and only while the rest of the pool passes.
	SuspectAfterFails = 3
	// MinReasoningTokens is the absolute floor for a high-effort reasoning run.
	MinReasoningTokens = 64
	// ReasoningMedianRatio is the minimum share of the pool median reasoning tokens.
	ReasoningMedianRatio = 0.25
)

// Prompt is one fixed canary task.
type Prompt struct {
	ID       string
	Kind     string
	Text     string
	Expected string // reasoning: exact integer answer
}

// Prompts rotate per round so every account in one round sees the same task.
var Prompts = []Prompt{
	{ID: "count-3-5-not-7", Kind: KindReasoning, Expected: "401", Text: "How many positive integers n with 1 <= n <= 1000 are divisible by 3 or by 5 but not divisible by 7? Think carefully, then end your reply with a final line exactly in the form `Final answer: <integer>`."},
	{ID: "svg-shapes", Kind: KindSVG, Text: "Output only a single self-contained HTML file (no external resources, under 3000 characters) containing an inline SVG with viewBox=\"0 0 200 200\" that shows a red circle, a blue square (rect) and a green triangle (polygon). The circle must rotate continuously around the center using <animateTransform> or a CSS @keyframes animation. Output only the HTML."},
	{ID: "pow-7-2026-mod-1000", Kind: KindReasoning, Expected: "649", Text: "What is the remainder when 7^2026 is divided by 1000? Think carefully, then end your reply with a final line exactly in the form `Final answer: <integer>`."},
	{ID: "domino-2x10", Kind: KindReasoning, Expected: "89", Text: "In how many ways can a 2 x 10 rectangle be tiled completely with 1 x 2 dominoes (rotations allowed, tilings that differ only by symmetry count as different)? Think carefully, then end your reply with a final line exactly in the form `Final answer: <integer>`."},
}

// PromptForRound picks the rotating prompt for a round sequence number.
func PromptForRound(sequence int64) Prompt {
	if sequence < 0 {
		sequence = -sequence
	}
	return Prompts[sequence%int64(len(Prompts))]
}

// Result is one stored probe of one account.
type Result struct {
	ID               uint64    `json:"id"`
	RoundID          string    `json:"roundId"`
	Trigger          string    `json:"trigger"`
	AccountID        uint64    `json:"accountId"`
	AccountName      string    `json:"accountName"`
	Model            string    `json:"model"`
	PromptID         string    `json:"promptId"`
	PromptKind       string    `json:"promptKind"`
	Status           string    `json:"status"`
	Reasons          string    `json:"reasons"`
	Score            float64   `json:"score"`
	Correct          bool      `json:"correct"`
	ReasoningTokens  int64     `json:"reasoningTokens"`
	OutputTokens     int64     `json:"outputTokens"`
	VisibleChars     int64     `json:"visibleChars"`
	PoolMedianReason float64   `json:"poolMedianReasoning"`
	FirstTokenMS     int64     `json:"firstTokenMs"`
	DurationMS       int64     `json:"durationMs"`
	ConsecutiveFails int       `json:"consecutiveFails"`
	Suspect          bool      `json:"suspect"`
	Error            string    `json:"error,omitempty"`
	RequestID        string    `json:"requestId"`
	CreatedAt        time.Time `json:"createdAt"`
}

var finalAnswerPattern = regexp.MustCompile(`(?i)final\s*answer\s*[:：]\s*\**\s*(-?[0-9][0-9,]*)`)

// CheckAnswer validates the visible text for a prompt.
func CheckAnswer(prompt Prompt, text string) (bool, string) {
	switch prompt.Kind {
	case KindReasoning:
		matches := finalAnswerPattern.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			return false, "缺少 Final answer"
		}
		got := strings.ReplaceAll(matches[len(matches)-1][1], ",", "")
		if got != prompt.Expected {
			return false, "答案错误(" + got + "≠" + prompt.Expected + ")"
		}
		return true, ""
	case KindSVG:
		return CheckSVG(text)
	}
	return false, "未知任务"
}

// CheckSVG performs a structural validity check of the SVG/HTML task.
func CheckSVG(text string) (bool, string) {
	lower := strings.ToLower(text)
	open := strings.Index(lower, "<svg")
	closeAt := strings.LastIndex(lower, "</svg>")
	if open < 0 || closeAt < open {
		return false, "无完整 <svg>"
	}
	svg := lower[open:closeAt]
	missing := make([]string, 0, 4)
	if !strings.Contains(svg, "<circle") {
		missing = append(missing, "circle")
	}
	if !strings.Contains(svg, "<rect") {
		missing = append(missing, "rect")
	}
	if !strings.Contains(svg, "<polygon") && !strings.Contains(svg, "<path") {
		missing = append(missing, "triangle")
	}
	if !strings.Contains(lower, "animatetransform") && !strings.Contains(lower, "@keyframes") && !strings.Contains(lower, "<animate") {
		missing = append(missing, "animation")
	}
	if strings.Count(svg, "<") < 4 {
		missing = append(missing, "elements")
	}
	if len(missing) > 0 {
		return false, "SVG 缺少 " + strings.Join(missing, "/")
	}
	return true, ""
}

// Median returns the median of positive values (0 when none).
func Median(values []int64) float64 {
	positive := make([]int64, 0, len(values))
	for _, value := range values {
		if value > 0 {
			positive = append(positive, value)
		}
	}
	if len(positive) == 0 {
		return 0
	}
	sort.Slice(positive, func(i, j int) bool { return positive[i] < positive[j] })
	mid := len(positive) / 2
	if len(positive)%2 == 1 {
		return float64(positive[mid])
	}
	return float64(positive[mid-1]+positive[mid]) / 2
}

// Score judges one finished (non-error, non-skipped) result against the pool
// median reasoning tokens of the same round and fills Status/Score/Reasons.
func Score(result *Result, prompt Prompt, correct bool, checkReason string, poolMedian float64) {
	reasons := make([]string, 0, 3)
	score := 0.0
	result.Correct = correct
	result.PoolMedianReason = poolMedian
	if correct {
		score += 50
	} else {
		reasons = append(reasons, checkReason)
	}
	reasoningOK := result.ReasoningTokens >= MinReasoningTokens
	if poolMedian > 0 {
		ratio := float64(result.ReasoningTokens) / poolMedian
		score += 30 * min(1, ratio)
		if ratio < ReasoningMedianRatio {
			reasoningOK = false
		}
	} else if reasoningOK {
		score += 30
	}
	if !reasoningOK {
		reasons = append(reasons, "思考 token 过少")
	}
	minChars := int64(8)
	if prompt.Kind == KindSVG {
		minChars = 200
	}
	if result.VisibleChars >= minChars {
		score += 20
	} else {
		reasons = append(reasons, "可见输出过短")
	}
	result.Score = float64(int(score*10+0.5)) / 10
	result.Reasons = strings.Join(reasons, "；")
	if len(reasons) == 0 {
		result.Status = StatusPass
	} else {
		result.Status = StatusFail
	}
}

// Judged reports whether a status counts toward consecutive failures.
func Judged(status string) bool { return status == StatusPass || status == StatusFail }

// ConsecutiveFails counts leading judged failures in newest-first history.
func ConsecutiveFails(newestFirst []Result) int {
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

// PoolPasses reports whether the other judged accounts of the round mostly pass.
func PoolPasses(round []Result, accountID uint64) bool {
	judged, passed := 0, 0
	for _, value := range round {
		if value.AccountID == accountID || !Judged(value.Status) {
			continue
		}
		judged++
		if value.Status == StatusPass {
			passed++
		}
	}
	return judged > 0 && passed*2 >= judged
}

// AccountFlag summarises real quality hits for one account (observe-only).
type AccountFlag struct {
	AccountID        uint64     `json:"accountId,string"`
	DegradedHits     int64      `json:"degradedHits"`
	LastDegradedAt   *time.Time `json:"lastDegradedAt,omitempty"`
	CanaryFailures   int64      `json:"canaryFailures"`
	LastCanaryFailAt *time.Time `json:"lastCanaryFailAt,omitempty"`
	LastCanaryStatus string     `json:"lastCanaryStatus,omitempty"`
	LastCanaryReason string     `json:"lastCanaryReason,omitempty"`
}

// Artifact is the stored output of one probe, kept for admin inspection.
type Artifact struct {
	ResultID           uint64    `json:"resultId"`
	Prompt             string    `json:"prompt"`
	Expected           string    `json:"expected,omitempty"`
	Response           string    `json:"response"`
	ResponseTruncated  bool      `json:"responseTruncated"`
	Reasoning          string    `json:"reasoning"`
	ReasoningTruncated bool      `json:"reasoningTruncated"`
	ReasoningChars     int64     `json:"reasoningChars"`
	InputTokens        int64     `json:"inputTokens"`
	OutputTokens       int64     `json:"outputTokens"`
	ReasoningTokens    int64     `json:"reasoningTokens"`
	TotalTokens        int64     `json:"totalTokens"`
	ResponseModel      string    `json:"responseModel"`
	ReasoningEffort    string    `json:"reasoningEffort"`
	MaxOutputTokens    int       `json:"maxOutputTokens"`
	FirstTokenMS       int64     `json:"firstTokenMs"`
	DurationMS         int64     `json:"durationMs"`
	CheckDetail        string    `json:"checkDetail"`
	ErrorCode          string    `json:"errorCode,omitempty"`
	StreamBytes        int64     `json:"streamBytes"`
	StreamChunks       int64     `json:"streamChunks"`
	CreatedAt          time.Time `json:"createdAt"`
}

const (
	// MaxArtifactResponseBytes caps the stored visible answer.
	MaxArtifactResponseBytes = 200 << 10
	// MaxArtifactReasoningBytes caps the stored reasoning text.
	MaxArtifactReasoningBytes = 64 << 10
)

// TruncateBytes cuts value to at most limit bytes on a rune boundary.
func TruncateBytes(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	cut := limit
	for cut > 0 && !utf8RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ExplainCheck describes what the checker saw, for the admin detail view.
func ExplainCheck(prompt Prompt, text string) string {
	switch prompt.Kind {
	case KindReasoning:
		matches := finalAnswerPattern.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			return "期望 Final answer: " + prompt.Expected + "；回复中未找到 Final answer 行"
		}
		got := strings.ReplaceAll(matches[len(matches)-1][1], ",", "")
		return "期望 " + prompt.Expected + "，回复 Final answer: " + got
	case KindSVG:
		lower := strings.ToLower(text)
		found := []string{}
		for _, item := range []struct{ name, marker string }{{"svg", "<svg"}, {"circle", "<circle"}, {"rect", "<rect"}, {"polygon", "<polygon"}, {"path", "<path"}, {"animateTransform", "animatetransform"}, {"@keyframes", "@keyframes"}, {"animate", "<animate"}} {
			if strings.Contains(lower, item.marker) {
				found = append(found, item.name)
			}
		}
		if len(found) == 0 {
			return "未检测到任何 SVG 元素"
		}
		return "检测到: " + strings.Join(found, ", ")
	}
	return ""
}

// PromptByID returns the prompt with the given id (zero Prompt when unknown).
func PromptByID(id string) Prompt {
	for _, prompt := range Prompts {
		if prompt.ID == id {
			return prompt
		}
	}
	return Prompt{}
}
