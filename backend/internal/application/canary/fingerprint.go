package canary

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	gatewayapp "github.com/chenyme/grok2api/backend/internal/application/gateway"
	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/canary"
	"github.com/chenyme/grok2api/backend/internal/thirdparty/lmfpd"
)

// FingerprintConfig is the observe-only model identity probe policy.
type FingerprintConfig struct {
	Enabled         bool
	Interval        time.Duration
	InitialDelay    time.Duration
	SampleAccounts  int
	MinConfidence   float64
	SuspectAfter    int
	FamilyTolerant  bool
	Models          []string
	ReasoningEffort string
	MaxOutputTokens int
	// AutoAfterFails probes an account automatically after this many
	// consecutive reasoning canary failures (0 disables).
	AutoAfterFails int
}

func normalizeFingerprint(cfg FingerprintConfig) FingerprintConfig {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 30 * time.Minute
	}
	if cfg.SampleAccounts <= 0 {
		cfg.SampleAccounts = 3
	}
	if cfg.MinConfidence <= 0 || cfg.MinConfidence > 1 {
		cfg.MinConfidence = 0.6
	}
	if cfg.SuspectAfter <= 0 {
		cfg.SuspectAfter = 2
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []string{"grok-4.7", "grok-4.5"}
	}
	if cfg.ReasoningEffort == "" {
		cfg.ReasoningEffort = "low"
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 4000
	}
	return cfg
}

// FingerprintStore persists fingerprint results.
type FingerprintStore interface {
	CreateFingerprint(context.Context, canary.FingerprintResult) (canary.FingerprintResult, error)
	ListFingerprints(context.Context, uint64, int) ([]canary.FingerprintResult, error)
	GetFingerprint(context.Context, uint64) (canary.FingerprintResult, error)
	CountFingerprintRounds(context.Context) (int64, error)
	DeleteFingerprintsBefore(context.Context, time.Time) error
}

// Analyzer scores fingerprint replies (lmfpd.Detector in production).
type Analyzer interface {
	Analyze([]lmfpd.Output, bool) (*lmfpd.Analysis, error)
	ModelFamily(string) string
}

// ChallengeSource generates the three number challenges of one probe.
type ChallengeSource func() ([]lmfpd.Challenge, error)

var (
	ErrFingerprintRunning     = errors.New("模型指纹探测正在运行")
	ErrFingerprintUnavailable = errors.New("模型指纹探测不可用")
)

type fingerprintState struct {
	running  sync.Mutex
	mu       sync.Mutex
	active   string
	lastRun  time.Time
	nextRun  time.Time
	analyzer Analyzer
	loadErr  error
	once     sync.Once
	source   ChallengeSource
}

// SetFingerprintAnalyzer overrides the detector and challenge source (tests).
func (s *Service) SetFingerprintAnalyzer(analyzer Analyzer, source ChallengeSource) {
	s.fp.once.Do(func() {})
	s.fp.analyzer, s.fp.source = analyzer, source
}

func (s *Service) fingerprintStore() (FingerprintStore, bool) {
	store, ok := s.store.(FingerprintStore)
	return store, ok
}

func (s *Service) loadAnalyzer() (Analyzer, error) {
	s.fp.once.Do(func() {
		detector, err := lmfpd.LoadEmbedded()
		if err != nil {
			s.fp.loadErr = err
			return
		}
		s.fp.analyzer = detector
		s.fp.source = func() ([]lmfpd.Challenge, error) { return lmfpd.GenerateChallenges(3) }
	})
	if s.fp.analyzer == nil {
		return nil, errors.Join(ErrFingerprintUnavailable, s.fp.loadErr)
	}
	return s.fp.analyzer, nil
}

// RunFingerprint is the fingerprint scheduler loop (only when enabled).
func (s *Service) RunFingerprint(ctx context.Context) error {
	if !s.fpCfg.Enabled {
		// Stay parked so the background supervisor does not treat this as a crash.
		<-ctx.Done()
		return nil
	}
	s.setFingerprintNext(time.Now().Add(s.fpCfg.InitialDelay))
	timer := time.NewTimer(s.fpCfg.InitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if s.fp.running.TryLock() {
			roundID, err := newRoundID()
			if err == nil {
				_, err = s.runFingerprintLocked(ctx, roundID, "schedule", nil)
			}
			s.fp.running.Unlock()
			if err != nil {
				s.logger.Warn("canary_fingerprint_round_failed", "error", err)
			}
		}
		s.setFingerprintNext(time.Now().Add(s.fpCfg.Interval))
		timer.Reset(s.fpCfg.Interval)
	}
}

func (s *Service) setFingerprintNext(value time.Time) {
	s.fp.mu.Lock()
	s.fp.nextRun = value
	s.fp.mu.Unlock()
}

// TriggerFingerprint starts a background fingerprint round. Empty accountIDs
// samples the configured number of accounts. Works even when the scheduled
// fingerprint probe is disabled.
func (s *Service) TriggerFingerprint(ctx context.Context, accountIDs []uint64, trigger string) (string, error) {
	if _, ok := s.fingerprintStore(); !ok {
		return "", ErrFingerprintUnavailable
	}
	if _, err := s.loadAnalyzer(); err != nil {
		return "", err
	}
	if !s.fp.running.TryLock() {
		return "", ErrFingerprintRunning
	}
	roundID, err := newRoundID()
	if err != nil {
		s.fp.running.Unlock()
		return "", err
	}
	if trigger == "" {
		trigger = "manual"
	}
	go func() {
		defer s.fp.running.Unlock()
		if _, err := s.runFingerprintLocked(context.WithoutCancel(ctx), roundID, trigger, accountIDs); err != nil {
			s.logger.Warn("canary_fingerprint_round_failed", "round_id", roundID, "error", err)
		}
	}()
	return roundID, nil
}

// RunFingerprintRound runs one round synchronously (tests and tools).
func (s *Service) RunFingerprintRound(ctx context.Context, accountIDs []uint64, trigger string) ([]canary.FingerprintResult, error) {
	if !s.fp.running.TryLock() {
		return nil, ErrFingerprintRunning
	}
	defer s.fp.running.Unlock()
	roundID, err := newRoundID()
	if err != nil {
		return nil, err
	}
	return s.runFingerprintLocked(ctx, roundID, trigger, accountIDs)
}

func (s *Service) selectFingerprintAccounts(ctx context.Context, store FingerprintStore, accountIDs []uint64) ([]accountdomain.Credential, error) {
	accounts, err := s.accounts.ListEnabled(ctx, accountdomain.ProviderBuild)
	if err != nil {
		return nil, fmt.Errorf("读取 Build 账号: %w", err)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	if len(accountIDs) > 0 {
		wanted := map[uint64]bool{}
		for _, id := range accountIDs {
			wanted[id] = true
		}
		out := make([]accountdomain.Credential, 0, len(accountIDs))
		for _, credential := range accounts {
			if wanted[credential.ID] {
				out = append(out, credential)
			}
		}
		return out, nil
	}
	n := s.fpCfg.SampleAccounts
	if n >= len(accounts) {
		return accounts, nil
	}
	rounds, err := store.CountFingerprintRounds(ctx)
	if err != nil {
		return nil, err
	}
	start := int(rounds*int64(n)) % len(accounts)
	out := make([]accountdomain.Credential, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, accounts[(start+i)%len(accounts)])
	}
	return out, nil
}

func (s *Service) runFingerprintLocked(ctx context.Context, roundID, trigger string, accountIDs []uint64) ([]canary.FingerprintResult, error) {
	store, ok := s.fingerprintStore()
	if !ok {
		return nil, ErrFingerprintUnavailable
	}
	analyzer, err := s.loadAnalyzer()
	if err != nil {
		return nil, err
	}
	s.fp.mu.Lock()
	s.fp.active = roundID
	s.fp.mu.Unlock()
	defer func() {
		s.fp.mu.Lock()
		s.fp.active = ""
		s.fp.lastRun = time.Now()
		s.fp.mu.Unlock()
	}()
	accounts, err := s.selectFingerprintAccounts(ctx, store, accountIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	results := make([]canary.FingerprintResult, 0, len(accounts)*len(s.fpCfg.Models))
	for _, credential := range accounts {
		for _, model := range s.fpCfg.Models {
			results = append(results, canary.FingerprintResult{
				RoundID: roundID, Trigger: trigger, AccountID: credential.ID, AccountName: credential.Name, Model: model, CreatedAt: now,
			})
		}
	}
	// Accounts run in parallel; each account sees one stream at a time.
	var wg sync.WaitGroup
	per := len(s.fpCfg.Models)
	for start := 0; start < len(results); start += per {
		wg.Add(1)
		go func(group []canary.FingerprintResult) {
			defer wg.Done()
			for index := range group {
				s.probeFingerprint(ctx, analyzer, &group[index])
			}
		}(results[start:min(start+per, len(results))])
	}
	wg.Wait()

	// Pool for "other accounts pass": this round, plus the latest earlier
	// result of accounts not in this round (single-account triggers).
	history, err := store.ListFingerprints(ctx, 0, 500)
	if err != nil {
		return nil, err
	}
	pool := append([]canary.FingerprintResult{}, results...)
	seen := map[string]bool{}
	for _, value := range results {
		seen[fmt.Sprint(value.AccountID, "/", value.Model)] = true
	}
	for _, value := range history {
		key := fmt.Sprint(value.AccountID, "/", value.Model)
		if seen[key] || now.Sub(value.CreatedAt) > 7*24*time.Hour {
			continue
		}
		seen[key] = true
		pool = append(pool, value)
	}
	for index := range results {
		value := &results[index]
		mine := []canary.FingerprintResult{*value}
		for _, old := range history {
			if old.AccountID == value.AccountID && old.Model == value.Model {
				mine = append(mine, old)
			}
		}
		value.ConsecutiveMismatch = canary.ConsecutiveMismatches(mine)
		value.Suspect = value.ConsecutiveMismatch >= s.fpCfg.SuspectAfter && canary.FingerprintPoolPasses(pool, value.AccountID, value.Model)
		stored, err := store.CreateFingerprint(ctx, *value)
		if err != nil {
			return nil, err
		}
		*value = stored
		s.logger.Info("canary_fingerprint_result", "round_id", roundID, "trigger", trigger, "account_id", value.AccountID, "model", value.Model,
			"status", value.Status, "verdict", value.Verdict, "top", value.TopModel, "confidence", value.Confidence,
			"valid_outputs", value.ValidOutputs, "consecutive_mismatches", value.ConsecutiveMismatch, "suspect_substituted", value.Suspect,
			"observe_only", true, "error", value.Error)
	}
	if err := store.DeleteFingerprintsBefore(ctx, now.Add(-s.cfg.Retention)); err != nil {
		s.logger.Warn("canary_fingerprint_prune_failed", "error", err)
	}
	return results, nil
}

// probeFingerprint sends the three challenges in parallel and scores them.
func (s *Service) probeFingerprint(ctx context.Context, analyzer Analyzer, value *canary.FingerprintResult) {
	challenges, err := s.fp.source()
	if err != nil || len(challenges) == 0 {
		value.Status, value.Error = canary.StatusError, "生成挑战失败"
		return
	}
	outs := make([]gatewayapp.CanaryProbeOutput, len(challenges))
	errs := make([]error, len(challenges))
	// Sequential on purpose: three concurrent streams on one account stalled
	// upstream (idle timeouts) in production. Each failed challenge gets one retry.
	for i, challenge := range challenges {
		for attempt := 0; attempt < 2 && ctx.Err() == nil; attempt++ {
			probeCtx, cancel := context.WithTimeout(ctx, s.cfg.ProbeTimeout)
			outs[i], errs[i] = s.prober.ProbeAccountCanary(probeCtx, gatewayapp.CanaryProbeInput{
				ClientKeyID: s.clientKeyID, AccountID: value.AccountID, Model: value.Model, Prompt: challenge.Prompt,
				ReasoningEffort: s.fpCfg.ReasoningEffort, MaxOutputTokens: s.fpCfg.MaxOutputTokens, IdleTimeout: s.cfg.IdleTimeout,
				MaxVisibleChars: fingerprintMaxVisibleChars,
			})
			cancel()
			if errs[i] == nil {
				break
			}
		}
	}
	inputs := make([]lmfpd.Output, len(challenges))
	requestIDs := make([]string, 0, len(challenges))
	failures := []string{}
	for i, challenge := range challenges {
		value.Prompts = append(value.Prompts, challenge.Prompt)
		raw, _ := canary.TruncateBytes(outs[i].Text, 16<<10)
		value.RawOutputs = append(value.RawOutputs, raw)
		value.OutputTokens += outs[i].OutputTokens
		value.DurationMS += outs[i].DurationMS
		if outs[i].RequestID != "" {
			requestIDs = append(requestIDs, outs[i].RequestID)
		}
		inputs[i] = lmfpd.Output{Text: outs[i].Text, ExpectedCount: challenge.ExpectedCount}
		if errs[i] != nil {
			failures = append(failures, errs[i].Error())
			inputs[i].Text = ""
		}
	}
	value.RequestIDs = truncate(strings.Join(requestIDs, ","), 250)
	if len(failures) == len(challenges) {
		value.Status, value.Verdict = canary.StatusError, ""
		value.Error = truncate(failures[0], 500)
		return
	}
	analysis, err := analyzer.Analyze(inputs, false)
	if err != nil {
		value.Status, value.Error = canary.StatusError, truncate(err.Error(), 500)
		return
	}
	value.ValidCounts = make([]int, len(challenges))
	for _, diagnostic := range analysis.Diagnostics {
		if diagnostic.Index >= 0 && diagnostic.Index < len(value.ValidCounts) {
			value.ValidCounts[diagnostic.Index] = diagnostic.ParsedNumbers
			if diagnostic.Accepted {
				value.ValidOutputs++
			}
		}
	}
	value.Candidates = []canary.FingerprintCandidate{}
	for _, candidate := range analysis.Results {
		if len(value.Candidates) == 3 {
			break
		}
		item := canary.FingerprintCandidate{Model: candidate.Model, Family: candidate.Family, Score: round4(candidate.Score)}
		if candidate.Probability != nil {
			item.Probability = round4(*candidate.Probability)
		}
		value.Candidates = append(value.Candidates, item)
	}
	if len(analysis.Results) > 0 {
		value.TopModel, value.TopFamily = analysis.Prediction, analysis.FamilyPrediction
		if analysis.Probability != nil {
			value.Confidence = round4(*analysis.Probability)
		}
	} else if analysis.Evidence.Reason != "" {
		value.Reasons = truncate(analysis.Evidence.Reason, 500)
	}
	if len(failures) > 0 && value.Error == "" {
		value.Error = truncate(fmt.Sprintf("%d/%d 请求失败: %s", len(failures), len(challenges), failures[0]), 500)
	}
	canary.JudgeFingerprint(value, analyzer.ModelFamily(value.Model), canary.FingerprintPolicy{
		MinConfidence: s.fpCfg.MinConfidence, FamilyTolerant: s.fpCfg.FamilyTolerant, SuspectAfter: s.fpCfg.SuspectAfter,
	})
	// The detector only calibrates a probability when every challenge is
	// accepted; a ranking alone is not evidence either way.
	if analysis.Probability == nil && value.Status != canary.StatusError {
		value.Status, value.Verdict = canary.StatusSkipped, canary.VerdictInsufficient
		value.Reasons = fmt.Sprintf("仅 %d/%d 组有效，未达评分条件（排名第一：%s）", value.ValidOutputs, len(challenges), value.TopModel)
	}
}

// fingerprintMaxVisibleChars caps each answer; 300 numbers need about 1.5KB.
const fingerprintMaxVisibleChars = 8 << 10

func round4(v float64) float64 { return float64(int64(v*10000+0.5)) / 10000 }

// maybeAutoFingerprint probes accounts with repeated reasoning failures.
// It runs when the fingerprint probe is enabled, or for manual canary rounds.
func (s *Service) maybeAutoFingerprint(ctx context.Context, trigger string, results []canary.Result) {
	if s.fpCfg.AutoAfterFails <= 0 || (!s.fpCfg.Enabled && trigger != "manual") {
		return
	}
	ids := []uint64{}
	for _, value := range results {
		if value.PromptKind == canary.KindReasoning && value.Status == canary.StatusFail && value.ConsecutiveFails >= s.fpCfg.AutoAfterFails {
			ids = append(ids, value.AccountID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if _, err := s.TriggerFingerprint(ctx, ids, "auto"); err != nil {
		s.logger.Info("canary_fingerprint_auto_skipped", "accounts", ids, "error", err)
	}
}

// FingerprintAccount is the latest fingerprint view of one account and model.
type FingerprintAccount struct {
	AccountID   uint64                     `json:"accountId"`
	AccountName string                     `json:"accountName"`
	Model       string                     `json:"model"`
	Latest      canary.FingerprintResult   `json:"latest"`
	History     []canary.FingerprintResult `json:"history"`
}

// FingerprintSummary is the admin view of the fingerprint probe.
type FingerprintSummary struct {
	Enabled        bool                 `json:"enabled"`
	ObserveOnly    bool                 `json:"observeOnly"`
	Interval       string               `json:"interval"`
	SampleAccounts int                  `json:"sampleAccounts"`
	MinConfidence  float64              `json:"minConfidence"`
	SuspectAfter   int                  `json:"suspectAfter"`
	FamilyTolerant bool                 `json:"familyTolerant"`
	Models         []string             `json:"models"`
	Effort         string               `json:"reasoningEffort"`
	Running        bool                 `json:"running"`
	ActiveRound    string               `json:"activeRound,omitempty"`
	LastRun        *time.Time           `json:"lastRun,omitempty"`
	NextRun        *time.Time           `json:"nextRun,omitempty"`
	Accounts       []FingerprintAccount `json:"accounts"`
}

// FingerprintSummary returns config and per-account/model latest results.
func (s *Service) FingerprintSummary(ctx context.Context, historyLimit int) (FingerprintSummary, error) {
	if historyLimit <= 0 || historyLimit > 50 {
		historyLimit = 8
	}
	out := FingerprintSummary{
		Enabled: s.fpCfg.Enabled, ObserveOnly: true, Interval: s.fpCfg.Interval.String(), SampleAccounts: s.fpCfg.SampleAccounts,
		MinConfidence: s.fpCfg.MinConfidence, SuspectAfter: s.fpCfg.SuspectAfter, FamilyTolerant: s.fpCfg.FamilyTolerant,
		Models: s.fpCfg.Models, Effort: s.fpCfg.ReasoningEffort, Accounts: []FingerprintAccount{},
	}
	s.fp.mu.Lock()
	out.Running, out.ActiveRound = s.fp.active != "", s.fp.active
	if !s.fp.lastRun.IsZero() {
		value := s.fp.lastRun.UTC()
		out.LastRun = &value
	}
	if !s.fp.nextRun.IsZero() && s.fpCfg.Enabled {
		value := s.fp.nextRun.UTC()
		out.NextRun = &value
	}
	s.fp.mu.Unlock()
	store, ok := s.fingerprintStore()
	if !ok {
		return out, nil
	}
	rows, err := store.ListFingerprints(ctx, 0, 500)
	if err != nil {
		return out, err
	}
	index := map[string]int{}
	for _, row := range rows {
		key := fmt.Sprint(row.AccountID, "/", row.Model)
		i, ok := index[key]
		if !ok {
			index[key] = len(out.Accounts)
			out.Accounts = append(out.Accounts, FingerprintAccount{AccountID: row.AccountID, AccountName: row.AccountName, Model: row.Model, Latest: row})
			i = index[key]
		}
		if len(out.Accounts[i].History) < historyLimit {
			out.Accounts[i].History = append(out.Accounts[i].History, row)
		}
	}
	sort.SliceStable(out.Accounts, func(i, j int) bool {
		if out.Accounts[i].AccountID != out.Accounts[j].AccountID {
			return out.Accounts[i].AccountID < out.Accounts[j].AccountID
		}
		return out.Accounts[i].Model < out.Accounts[j].Model
	})
	return out, nil
}

// FingerprintDetail returns one result with prompts and raw outputs.
func (s *Service) FingerprintDetail(ctx context.Context, id uint64) (canary.FingerprintResult, error) {
	store, ok := s.fingerprintStore()
	if !ok {
		return canary.FingerprintResult{}, ErrResultNotFound
	}
	value, err := store.GetFingerprint(ctx, id)
	if err != nil {
		return canary.FingerprintResult{}, ErrResultNotFound
	}
	return value, nil
}

// mergeFingerprintFlags adds suspect_substituted to the account flags.
func (s *Service) mergeFingerprintFlags(ctx context.Context, flags []canary.AccountFlag, window time.Duration) []canary.AccountFlag {
	store, ok := s.fingerprintStore()
	if !ok {
		return flags
	}
	rows, err := store.ListFingerprints(ctx, 0, 500)
	if err != nil {
		return flags
	}
	cutoff := time.Now().UTC().Add(-window)
	latest := map[string]bool{}
	byID := map[uint64]int{}
	for i, flag := range flags {
		byID[flag.AccountID] = i
	}
	for _, row := range rows {
		key := fmt.Sprint(row.AccountID, "/", row.Model)
		if latest[key] || row.CreatedAt.Before(cutoff) || !canary.Judged(row.Status) {
			continue
		}
		latest[key] = true
		if !row.Suspect {
			continue
		}
		i, ok := byID[row.AccountID]
		if !ok {
			flags = append(flags, canary.AccountFlag{AccountID: row.AccountID})
			i = len(flags) - 1
			byID[row.AccountID] = i
		}
		if flags[i].SuspectSubstituted {
			continue
		}
		at := row.CreatedAt.UTC()
		flags[i].SuspectSubstituted, flags[i].FingerprintModel, flags[i].FingerprintTop = true, row.Model, row.TopModel
		flags[i].FingerprintConfidence, flags[i].LastFingerprintAt = row.Confidence, &at
	}
	return flags
}
