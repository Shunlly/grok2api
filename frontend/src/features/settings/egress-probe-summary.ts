export type ProbeFamilyLike = { status: "unknown" | "healthy" | "unhealthy"; exitIp?: string; latencyMs?: number; error?: string };
export type ProbeResultLike = { status: "unknown" | "healthy" | "unhealthy"; exitIp?: string; error?: string; ipv4?: ProbeFamilyLike; ipv6?: ProbeFamilyLike };

export type ProbeSummary = {
  kind: "success" | "warning" | "error";
  exitIp: string;
  ipv4: "healthy" | "unhealthy" | "unknown";
  ipv6: "healthy" | "unhealthy" | "unknown";
  error: string;
};

// summarizeProbe turns one IPv4/IPv6 probe into a single toast. A proxy that
// only reaches IPv4 is usable for Grok, so it is a warning rather than a
// failure; otherwise the user sees "代理连接失败" and assumes the test is broken.
export function summarizeProbe(result: ProbeResultLike): ProbeSummary {
  const ipv4 = result.ipv4?.status ?? "unknown";
  const ipv6 = result.ipv6?.status ?? "unknown";
  const exitIp = result.exitIp || result.ipv4?.exitIp || result.ipv6?.exitIp || "";
  const error = result.error || result.ipv4?.error || result.ipv6?.error || "";
  const anyHealthy = result.status === "healthy" || ipv4 === "healthy" || ipv6 === "healthy";
  const allHealthy = ipv4 === "healthy" && ipv6 === "healthy";
  const kind = !anyHealthy ? "error" : allHealthy || (ipv4 === "unknown" && ipv6 === "unknown") ? "success" : "warning";
  return { kind, exitIp, ipv4, ipv6, error };
}
