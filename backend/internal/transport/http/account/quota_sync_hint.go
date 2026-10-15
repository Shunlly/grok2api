package account

import (
	"time"

	quotasyncapp "github.com/chenyme/grok2api/backend/internal/application/quotasync"
)

// QuotaSyncSchedule exposes the background quota auto-sync schedule.
type QuotaSyncSchedule interface {
	Status() quotasyncapp.Status
	ResetDelay() time.Duration
	PendingResetSyncs() map[uint64]time.Time
}

// WithQuotaSync attaches the auto-sync schedule used for per-account hints.
func (h *Handler) WithQuotaSync(schedule QuotaSyncSchedule) *Handler {
	if schedule != nil {
		h.quotaSync = schedule
	}
	return h
}

// Next sync reasons exposed as nextQuotaSyncReason.
const (
	quotaSyncReasonHourly      = "hourly"
	quotaSyncReasonReset       = "reset"
	quotaSyncReasonDisabled    = "autoSyncDisabled"
	quotaSyncReasonAccountOff  = "accountDisabled"
	quotaSyncReasonReauth      = "reauthRequired"
	quotaSyncReasonUnsupported = "unsupported"
)

// lastQuotaSync is the newest upstream sync among billing and quota windows.
func lastQuotaSync(item accountResponse) *time.Time {
	var best time.Time
	if item.Billing != nil && item.Billing.SyncedAt.After(best) {
		best = item.Billing.SyncedAt
	}
	for _, w := range item.QuotaWindows {
		if w.SyncedAt != nil && w.SyncedAt.After(best) {
			best = *w.SyncedAt
		}
	}
	if best.IsZero() {
		return nil
	}
	return &best
}

type quotaSyncInputs struct {
	supported  bool
	status     quotasyncapp.Status
	hasStatus  bool
	resetDelay time.Duration
	pending    map[uint64]time.Time
	now        time.Time
}

// computeQuotaSyncHint fills lastQuotaSyncAt / nextQuotaSyncAt / nextQuotaSyncReason.
func computeQuotaSyncHint(item *accountResponse, in quotaSyncInputs) {
	item.LastQuotaSyncAt = lastQuotaSync(*item)
	item.NextQuotaSyncAt = nil
	switch {
	case !in.hasStatus || !in.status.Enabled:
		item.NextQuotaSyncReason = quotaSyncReasonDisabled
		return
	case !in.supported:
		item.NextQuotaSyncReason = quotaSyncReasonUnsupported
		return
	case !item.Enabled:
		item.NextQuotaSyncReason = quotaSyncReasonAccountOff
		return
	case item.AuthStatus == "reauthRequired":
		item.NextQuotaSyncReason = quotaSyncReasonReauth
		return
	}
	var next time.Time
	reason := ""
	if in.status.NextRunAt != nil {
		next, reason = *in.status.NextRunAt, quotaSyncReasonHourly
	}
	reset, ok := in.pending[item.ID]
	if !ok && item.NextResetAt != nil && !item.NoQuota {
		// Scheduler has no boundary yet (e.g. before the first run): derive it.
		due := item.NextResetAt.Add(in.resetDelay)
		if due.After(in.now) {
			reset, ok = due, true
		}
	}
	if ok && (next.IsZero() || reset.Before(next)) {
		next, reason = reset, quotaSyncReasonReset
	}
	if ok {
		r := reset
		item.ResetQuotaSyncAt = &r
	}
	if next.IsZero() {
		item.NextQuotaSyncReason = quotaSyncReasonHourly
		return
	}
	item.NextQuotaSyncAt, item.NextQuotaSyncReason = &next, reason
}

func (h *Handler) applyQuotaSyncHints(items []accountResponse) {
	in := quotaSyncInputs{now: time.Now()}
	if h.quotaSync != nil {
		in.status, in.hasStatus = h.quotaSync.Status(), true
		in.resetDelay = h.quotaSync.ResetDelay()
		in.pending = h.quotaSync.PendingResetSyncs()
	}
	for i := range items {
		in.supported = h.service == nil || h.service.AutoSyncSupported(items[i].Provider)
		computeQuotaSyncHint(&items[i], in)
	}
}
