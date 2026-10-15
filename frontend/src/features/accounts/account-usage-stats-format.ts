const USD_TICKS_PER_DOLLAR = 10_000_000_000;

/** 北京时间（Asia/Shanghai）自然日 YYYY-MM-DD，用于判断快照里的“今日”是否已过期。 */
export function shanghaiDate(at: Date = new Date()): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit" }).format(at);
}

/** 今日数据只在快照日期等于当前北京时间日期时有效；否则视为 0（跨日后尚未重新同步）。 */
export function isTodayCurrent(todayDate: string | undefined, now: Date = new Date()): boolean {
  return !!todayDate && todayDate === shanghaiDate(now);
}

export function formatUsageCompact(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0";
  return new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

export function formatUsageUSD(ticks: number): string {
  const value = (Number.isFinite(ticks) && ticks > 0 ? ticks : 0) / USD_TICKS_PER_DOLLAR;
  return `$${value.toFixed(2)}`;
}

/** 周期来源 → i18n key（无来源 = v3.4.2 旧版累计快照）。 */
export function periodSourceKey(source?: string): "billing" | "window" | "rolling" | "legacy" {
  if (source === "billingUsagePeriod") return "billing";
  if (source?.startsWith("quotaWindow:")) return "window";
  if (source === "rolling7d") return "rolling";
  return "legacy";
}
