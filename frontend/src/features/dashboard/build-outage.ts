// Structural subset of AccountSummaryDTO; kept local so node tests do not load the API client.
type ProviderSummary = { providers?: Partial<Record<string, { total: number; available: number }>> };

export type BuildPoolOutage = { total: number; available: number };

// buildPoolOutage reports an outage only when Build accounts exist and none of
// them can currently serve requests (all reauthorizing, disabled or cooling).
export function buildPoolOutage(summary: ProviderSummary | undefined): BuildPoolOutage | null {
  const build = summary?.providers?.grok_build;
  if (!build || build.total <= 0 || build.available > 0) return null;
  return { total: build.total, available: build.available };
}
