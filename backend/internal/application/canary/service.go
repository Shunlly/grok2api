// Package canary schedules observe-only account canary probes. Results are
// stored and shown in the admin panel only; they never influence routing,
// cooldown or account enablement.
package canary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	gatewayapp "github.com/chenyme/grok2api/backend/internal/application/gateway"
	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/canary"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
)

// Config is the static canary policy.
type Config struct {
	Enabled         bool
	Interval        time.Duration
	InitialDelay    time.Duration
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	ProbeTimeout    time.Duration
	// IdleTimeout is the first-token / inter-chunk silence budget inside ProbeTimeout.
	IdleTimeout time.Duration
	Retention   time.Duration
	// ArtifactRetention keeps prompt/answer/reasoning artifacts (aligned with audit retention).
	ArtifactRetention time.Duration
}

type Prober interface {
	ProbeAccountCanary(context.Context, gatewayapp.CanaryProbeInput) (gatewayapp.CanaryProbeOutput, error)
}

type Accounts interface {
	ListEnabled(context.Context, accountdomain.Provider) ([]accountdomain.Credential, error)
}

type Store interface {
	Create(context.Context, canary.Result) (canary.Result, error)
	ListRecent(context.Context, uint64, int) ([]canary.Result, error)
	CountRounds(context.Context) (int64, error)
	DeleteBefore(context.Context, time.Time) error
}

// ArtifactStore is the optional artifact persistence (prompt, answer, reasoning).
type ArtifactStore interface {
	CreateArtifact(context.Context, canary.Artifact) error
	GetResult(context.Context, uint64) (canary.Result, *canary.Artifact, error)
	DeleteArtifactsBefore(context.Context, time.Time) error
}

var ErrRoundRunning = errors.New("金丝雀探测轮次正在运行")

type Service struct {
	cfg         Config
	clientKeyID uint64
	prober      Prober
	accounts    Accounts
	store       Store
	logger      *slog.Logger

	running sync.Mutex
	mu      sync.Mutex
	lastRun time.Time
	nextRun time.Time
	active  string
}

func NewService(cfg Config, clientKeyID uint64, prober Prober, accounts Accounts, store Store, logger *slog.Logger) *Service {
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.Model == "" {
		cfg.Model = "grok-4.7"
	}
	if cfg.ReasoningEffort == "" {
		cfg.ReasoningEffort = "high"
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 6000
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 5 * time.Minute
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = gatewayapp.DefaultCanaryIdleTimeout
	}
	if cfg.IdleTimeout > cfg.ProbeTimeout {
		cfg.IdleTimeout = cfg.ProbeTimeout
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 30 * 24 * time.Hour
	}
	if cfg.ArtifactRetention <= 0 || cfg.ArtifactRetention > cfg.Retention {
		cfg.ArtifactRetention = min(7*24*time.Hour, cfg.Retention)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{cfg: cfg, clientKeyID: clientKeyID, prober: prober, accounts: accounts, store: store, logger: logger}
}

// Run is the scheduler loop. It returns when ctx is done.
func (s *Service) Run(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}
	delay := s.cfg.InitialDelay
	if delay <= 0 {
		delay = 10 * time.Minute
	}
	s.setNext(time.Now().Add(delay))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if _, err := s.RunRound(ctx, "schedule"); err != nil && !errors.Is(err, ErrRoundRunning) {
			s.logger.Warn("canary_round_failed", "error", err)
		}
		s.setNext(time.Now().Add(s.cfg.Interval))
		timer.Reset(s.cfg.Interval)
	}
}

func (s *Service) setNext(value time.Time) {
	s.mu.Lock()
	s.nextRun = value
	s.mu.Unlock()
}

// Trigger starts an on-demand round in the background.
func (s *Service) Trigger(ctx context.Context) (string, error) {
	if !s.running.TryLock() {
		return "", ErrRoundRunning
	}
	roundID, err := newRoundID()
	if err != nil {
		s.running.Unlock()
		return "", err
	}
	go func() {
		defer s.running.Unlock()
		if _, err := s.runRoundLocked(context.WithoutCancel(ctx), roundID, "manual"); err != nil {
			s.logger.Warn("canary_round_failed", "round_id", roundID, "error", err)
		}
	}()
	return roundID, nil
}

// RunRound probes every enabled Build account once, synchronously.
func (s *Service) RunRound(ctx context.Context, trigger string) ([]canary.Result, error) {
	if !s.running.TryLock() {
		return nil, ErrRoundRunning
	}
	defer s.running.Unlock()
	roundID, err := newRoundID()
	if err != nil {
		return nil, err
	}
	return s.runRoundLocked(ctx, roundID, trigger)
}

// ProbeAccount runs one canary task against a single account (used by the
// re-activation scheduler) and stores the result with trigger "recovery".
// Without a pool to compare against, only the absolute thresholds apply.
func (s *Service) ProbeAccount(ctx context.Context, accountID uint64, accountName string) (canary.Result, error) {
	roundID, err := newRoundID()
	if err != nil {
		return canary.Result{}, err
	}
	sequence, err := s.store.CountRounds(ctx)
	if err != nil {
		return canary.Result{}, err
	}
	prompt := canary.PromptForRound(sequence)
	for offset := int64(1); prompt.Kind == canary.KindSVG && offset < int64(len(canary.Prompts)); offset++ {
		prompt = canary.PromptForRound(sequence + offset)
	}
	result := canary.Result{
		RoundID: roundID, Trigger: "recovery", AccountID: accountID, AccountName: accountName,
		Model: s.cfg.Model, PromptID: prompt.ID, PromptKind: prompt.Kind, CreatedAt: time.Now().UTC(),
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.cfg.ProbeTimeout)
	out, probeErr := s.prober.ProbeAccountCanary(probeCtx, gatewayapp.CanaryProbeInput{
		ClientKeyID: s.clientKeyID, AccountID: accountID, Model: s.cfg.Model, Prompt: prompt.Text,
		ReasoningEffort: s.cfg.ReasoningEffort, MaxOutputTokens: s.cfg.MaxOutputTokens,
		IdleTimeout: s.cfg.IdleTimeout,
	})
	cancel()
	result.RequestID = out.RequestID
	artifact := s.newArtifact(prompt, out)
	if probeErr != nil {
		result.Status = canary.StatusError
		result.Error = truncate(probeErr.Error(), 500)
	} else {
		result.ReasoningTokens, result.OutputTokens, result.VisibleChars = out.ReasoningTokens, out.OutputTokens, out.VisibleChars
		result.FirstTokenMS, result.DurationMS = out.FirstTokenMS, out.DurationMS
		correct, reason := canary.CheckAnswer(prompt, out.Text)
		canary.Score(&result, prompt, correct, reason, 0)
	}
	stored, err := s.store.Create(ctx, result)
	if err != nil {
		return result, err
	}
	s.saveArtifact(ctx, stored, artifact, probeErr)
	s.logger.Info("canary_recovery_probe_result", "account_id", accountID, "status", stored.Status, "score", stored.Score, "reasons", stored.Reasons, "error", stored.Error)
	return stored, probeErr
}

func newRoundID() (string, error) {
	token, err := security.NewOpaqueToken(6)
	if err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + token, nil
}

func (s *Service) runRoundLocked(ctx context.Context, roundID, trigger string) ([]canary.Result, error) {
	s.mu.Lock()
	s.active = roundID
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active = ""
		s.lastRun = time.Now()
		s.mu.Unlock()
	}()
	accounts, err := s.accounts.ListEnabled(ctx, accountdomain.ProviderBuild)
	if err != nil {
		return nil, fmt.Errorf("读取 Build 账号: %w", err)
	}
	sequence, err := s.store.CountRounds(ctx)
	if err != nil {
		return nil, err
	}
	prompt := canary.PromptForRound(sequence)
	now := time.Now().UTC()
	results := make([]canary.Result, len(accounts))
	texts := make([]string, len(accounts))
	artifacts := make([]canary.Artifact, len(accounts))
	errs := make([]error, len(accounts))
	skipped := modeldomain.HidesReasoningStreamForProvider(accountdomain.ProviderBuild, s.cfg.Model)
	var wg sync.WaitGroup
	for index, credential := range accounts {
		results[index] = canary.Result{
			RoundID: roundID, Trigger: trigger, AccountID: credential.ID, AccountName: credential.Name,
			Model: s.cfg.Model, PromptID: prompt.ID, PromptKind: prompt.Kind, CreatedAt: now,
		}
		if skipped {
			results[index].Status = canary.StatusSkipped
			results[index].Reasons = "模型不暴露思考流，无法评估"
			continue
		}
		wg.Add(1)
		go func(index int, accountID uint64) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, s.cfg.ProbeTimeout)
			defer cancel()
			out, err := s.prober.ProbeAccountCanary(probeCtx, gatewayapp.CanaryProbeInput{
				ClientKeyID: s.clientKeyID, AccountID: accountID, Model: s.cfg.Model, Prompt: prompt.Text,
				ReasoningEffort: s.cfg.ReasoningEffort, MaxOutputTokens: s.cfg.MaxOutputTokens,
				IdleTimeout: s.cfg.IdleTimeout,
			})
			value := &results[index]
			value.RequestID = out.RequestID
			artifacts[index] = s.newArtifact(prompt, out)
			errs[index] = err
			if err != nil {
				value.Status = canary.StatusError
				value.Error = truncate(err.Error(), 500)
				return
			}
			value.ReasoningTokens = out.ReasoningTokens
			value.OutputTokens = out.OutputTokens
			value.VisibleChars = out.VisibleChars
			value.FirstTokenMS = out.FirstTokenMS
			value.DurationMS = out.DurationMS
			texts[index] = out.Text
		}(index, credential.ID)
	}
	wg.Wait()

	reasoning := make([]int64, 0, len(results))
	for _, value := range results {
		if value.Status == "" {
			reasoning = append(reasoning, value.ReasoningTokens)
		}
	}
	median := canary.Median(reasoning)
	for index := range results {
		if results[index].Status != "" {
			continue
		}
		correct, reason := canary.CheckAnswer(prompt, texts[index])
		canary.Score(&results[index], prompt, correct, reason, median)
	}
	for index := range results {
		value := &results[index]
		history, err := s.store.ListRecent(ctx, value.AccountID, 20)
		if err != nil {
			return nil, err
		}
		value.ConsecutiveFails = canary.ConsecutiveFails(append([]canary.Result{*value}, history...))
		value.Suspect = value.ConsecutiveFails >= canary.SuspectAfterFails && canary.PoolPasses(results, value.AccountID)
		stored, err := s.store.Create(ctx, *value)
		if err != nil {
			return nil, err
		}
		*value = stored
		if value.Status != canary.StatusSkipped {
			s.saveArtifact(ctx, stored, artifacts[index], errs[index])
		}
		s.logger.Info("canary_probe_result", "round_id", roundID, "account_id", value.AccountID, "prompt", value.PromptID,
			"status", value.Status, "score", value.Score, "reasoning_tokens", value.ReasoningTokens, "consecutive_fails", value.ConsecutiveFails,
			"suspect", value.Suspect, "observe_only", true, "reasons", value.Reasons, "error", value.Error)
	}
	if err := s.store.DeleteBefore(ctx, now.Add(-s.cfg.Retention)); err != nil {
		s.logger.Warn("canary_prune_failed", "error", err)
	}
	if store, ok := s.store.(ArtifactStore); ok {
		if err := store.DeleteArtifactsBefore(ctx, now.Add(-s.cfg.ArtifactRetention)); err != nil {
			s.logger.Warn("canary_artifact_prune_failed", "error", err)
		}
	}
	return results, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// AccountSummary is the latest view of one account.
type AccountSummary struct {
	AccountID   uint64          `json:"accountId"`
	AccountName string          `json:"accountName"`
	Latest      canary.Result   `json:"latest"`
	History     []canary.Result `json:"history"`
	PassRate    float64         `json:"passRate"`
	AvgScore    float64         `json:"avgScore"`
}

type Summary struct {
	Enabled      bool             `json:"enabled"`
	ObserveOnly  bool             `json:"observeOnly"`
	Interval     string           `json:"interval"`
	Model        string           `json:"model"`
	Effort       string           `json:"reasoningEffort"`
	MaxOutput    int              `json:"maxOutputTokens"`
	SuspectAfter int              `json:"suspectAfter"`
	Running      bool             `json:"running"`
	ActiveRound  string           `json:"activeRound,omitempty"`
	LastRun      *time.Time       `json:"lastRun,omitempty"`
	NextRun      *time.Time       `json:"nextRun,omitempty"`
	Accounts     []AccountSummary `json:"accounts"`
}

func (s *Service) Summary(ctx context.Context, historyLimit int) (Summary, error) {
	if historyLimit <= 0 || historyLimit > 50 {
		historyLimit = 12
	}
	rows, err := s.store.ListRecent(ctx, 0, 500)
	if err != nil {
		return Summary{}, err
	}
	s.mu.Lock()
	out := Summary{
		Enabled: s.cfg.Enabled, ObserveOnly: true, Interval: s.cfg.Interval.String(), Model: s.cfg.Model,
		Effort: s.cfg.ReasoningEffort, MaxOutput: s.cfg.MaxOutputTokens, SuspectAfter: canary.SuspectAfterFails,
		Running: s.active != "", ActiveRound: s.active,
	}
	if !s.lastRun.IsZero() {
		value := s.lastRun.UTC()
		out.LastRun = &value
	}
	if !s.nextRun.IsZero() && s.cfg.Enabled {
		value := s.nextRun.UTC()
		out.NextRun = &value
	}
	s.mu.Unlock()
	byAccount := map[uint64]*AccountSummary{}
	order := []uint64{}
	for _, row := range rows {
		summary := byAccount[row.AccountID]
		if summary == nil {
			summary = &AccountSummary{AccountID: row.AccountID, AccountName: row.AccountName, Latest: row}
			byAccount[row.AccountID] = summary
			order = append(order, row.AccountID)
		}
		if len(summary.History) < historyLimit {
			summary.History = append(summary.History, row)
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out.Accounts = make([]AccountSummary, 0, len(order))
	for _, id := range order {
		summary := byAccount[id]
		judged, passed, scoreSum := 0, 0, 0.0
		for _, row := range summary.History {
			if canary.Judged(row.Status) {
				judged++
				scoreSum += row.Score
				if row.Status == canary.StatusPass {
					passed++
				}
			}
		}
		if judged > 0 {
			summary.PassRate = float64(passed) / float64(judged)
			summary.AvgScore = float64(int(scoreSum/float64(judged)*10+0.5)) / 10
		}
		out.Accounts = append(out.Accounts, *summary)
	}
	return out, nil
}

type accountFlagStore interface {
	AccountQualityFlags(context.Context, time.Time) ([]canary.AccountFlag, error)
}

// AccountFlags returns accounts with real quality hits in the last window.
func (s *Service) AccountFlags(ctx context.Context, window time.Duration) ([]canary.AccountFlag, error) {
	store, ok := s.store.(accountFlagStore)
	if !ok {
		return []canary.AccountFlag{}, nil
	}
	return store.AccountQualityFlags(ctx, time.Now().UTC().Add(-window))
}

func (s *Service) newArtifact(prompt canary.Prompt, out gatewayapp.CanaryProbeOutput) canary.Artifact {
	response, responseCut := canary.TruncateBytes(out.Text, canary.MaxArtifactResponseBytes)
	reasoning, reasoningCut := canary.TruncateBytes(out.ReasoningText, canary.MaxArtifactReasoningBytes)
	return canary.Artifact{
		Prompt: prompt.Text, Expected: prompt.Expected, Response: response, ResponseTruncated: responseCut,
		Reasoning: reasoning, ReasoningTruncated: reasoningCut || int64(len([]rune(reasoning))) < out.ReasoningChars, ReasoningChars: out.ReasoningChars,
		InputTokens: out.InputTokens, OutputTokens: out.OutputTokens, ReasoningTokens: out.ReasoningTokens, TotalTokens: out.TotalTokens,
		ResponseModel: out.ResponseModel, ReasoningEffort: s.cfg.ReasoningEffort, MaxOutputTokens: s.cfg.MaxOutputTokens,
		FirstTokenMS: out.FirstTokenMS, DurationMS: out.DurationMS,
		ErrorCode: out.ErrorCode, StreamBytes: out.StreamBytes, StreamChunks: out.StreamChunks,
	}
}

func (s *Service) saveArtifact(ctx context.Context, result canary.Result, artifact canary.Artifact, probeErr error) {
	store, ok := s.store.(ArtifactStore)
	if !ok || result.ID == 0 {
		return
	}
	artifact.ResultID = result.ID
	artifact.CreatedAt = result.CreatedAt
	if probeErr == nil {
		artifact.CheckDetail = truncate(canary.ExplainCheck(canary.PromptByID(result.PromptID), artifact.Response), 1000)
	} else {
		artifact.CheckDetail = truncate("探测失败: "+probeErr.Error(), 1000)
	}
	if err := store.CreateArtifact(ctx, artifact); err != nil {
		s.logger.Warn("canary_artifact_save_failed", "result_id", result.ID, "error", err)
	}
}

// ErrResultNotFound is returned when a probe result id does not exist.
var ErrResultNotFound = errors.New("金丝雀探测结果不存在")

// ResultDetail is one probe with its artifact (nil for results recorded before artifacts existed).
type ResultDetail struct {
	Result   canary.Result    `json:"result"`
	Artifact *canary.Artifact `json:"artifact"`
}

// ResultDetail loads one probe result with its stored artifact.
func (s *Service) ResultDetail(ctx context.Context, id uint64) (ResultDetail, error) {
	store, ok := s.store.(ArtifactStore)
	if !ok {
		return ResultDetail{}, ErrResultNotFound
	}
	result, artifact, err := store.GetResult(ctx, id)
	if err != nil {
		if result.ID == 0 {
			return ResultDetail{}, ErrResultNotFound
		}
		return ResultDetail{}, err
	}
	return ResultDetail{Result: result, Artifact: artifact}, nil
}
