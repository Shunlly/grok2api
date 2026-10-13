package gateway

import "context"

type recoveryProbeKey struct{}

// WithRecoveryProbe marks a pinned canary request issued by the account
// re-activation scheduler. Such a request may use an account that is held in
// cooldown so the account can be verified before normal traffic returns.
func WithRecoveryProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, recoveryProbeKey{}, true)
}

func isRecoveryProbe(ctx context.Context) bool {
	value, _ := ctx.Value(recoveryProbeKey{}).(bool)
	return value
}
