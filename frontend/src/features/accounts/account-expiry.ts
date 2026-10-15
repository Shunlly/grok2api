const SOON_MS = 3 * 24 * 60 * 60 * 1000;

/** 过期状态：expired 已过期；soon 3 天内到期；ok 其他。自动续期的凭据不高亮。 */
export function expiryState(at: string | undefined, autoRenew: boolean | undefined, now: Date = new Date()): "unknown" | "expired" | "soon" | "ok" {
  if (!at || Number.isNaN(Date.parse(at))) return "unknown";
  if (autoRenew) return "ok";
  const diff = Date.parse(at) - now.getTime();
  if (diff <= 0) return "expired";
  return diff < SOON_MS ? "soon" : "ok";
}

export type CredentialIssue = "noRefreshToken" | "refreshFailed" | "expired" | "reauthRequired";

type CredentialFields = { authType: string; authStatus: string; refreshable: boolean; refreshFailureCount: number; expiresAt?: string };

/** 凭证是否无法自动续期（需要重新登录）。只对 OAuth（Build）凭证判断；SSO Cookie 不走 refresh token。 */
export function credentialIssues(account: CredentialFields, now: Date = new Date()): CredentialIssue[] {
  const issues: CredentialIssue[] = [];
  if (account.authStatus === "reauthRequired") issues.push("reauthRequired");
  if (account.authType !== "oauth") return issues;
  if (!account.refreshable) issues.push("noRefreshToken");
  if (account.refreshFailureCount > 0) issues.push("refreshFailed");
  // 给后台续期留 10 分钟余量，避免刚好过期还没轮到续期时误报。
  if (account.expiresAt && !Number.isNaN(Date.parse(account.expiresAt)) && Date.parse(account.expiresAt) + 10 * 60 * 1000 <= now.getTime()) issues.push("expired");
  return issues;
}
