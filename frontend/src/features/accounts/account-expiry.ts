const SOON_MS = 3 * 24 * 60 * 60 * 1000;

/** 过期状态：expired 已过期；soon 3 天内到期；ok 其他。自动续期的凭据不高亮。 */
export function expiryState(at: string | undefined, autoRenew: boolean | undefined, now: Date = new Date()): "unknown" | "expired" | "soon" | "ok" {
  if (!at || Number.isNaN(Date.parse(at))) return "unknown";
  if (autoRenew) return "ok";
  const diff = Date.parse(at) - now.getTime();
  if (diff <= 0) return "expired";
  return diff < SOON_MS ? "soon" : "ok";
}
