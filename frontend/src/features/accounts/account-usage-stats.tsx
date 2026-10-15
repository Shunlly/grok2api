import { useTranslation } from "react-i18next";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AccountDTO } from "@/features/accounts/accounts-api";
import { formatUsageCompact, formatUsageUSD, isTodayCurrent, periodSourceKey } from "@/features/accounts/account-usage-stats-format";
import { displayTimeZoneLabel, formatDateTime } from "@/shared/lib/format";

const chip = "rounded bg-muted px-1.5 py-0.5 text-[11px] tabular-nums text-muted-foreground";

/** 账号用量：周期请求 / 总Token / 总金额 / 今日金额。快照只在同步额度时刷新。 */
export function AccountUsageStats({ account, variant = "row" }: { account: Pick<AccountDTO, "usageStats">; variant?: "row" | "dialog" }) {
  const { t, i18n } = useTranslation();
  const stats = account.usageStats;
  if (!stats) {
    return <div className={variant === "row" ? "mt-1 text-[11px] text-muted-foreground" : "text-sm text-muted-foreground"}>{t("accounts.usageStats.notComputed")}</div>;
  }
  const todayCurrent = isTodayCurrent(stats.todayDate);
  const todayCost = todayCurrent ? stats.todayCostUsdTicks : 0;
  const todayFromReset = todayCurrent ? todayResetTime(stats.todayStart) : null;
  const n = (v: number) => v.toLocaleString(i18n.language);
  const tooltip = (
    <div className="space-y-1 text-xs">
      <div className="font-medium">{t("accounts.usageStats.tooltipPeriod", { start: stats.periodStart ? formatDateTime(stats.periodStart, i18n.language) : "-", end: stats.periodEnd ? formatDateTime(stats.periodEnd, i18n.language) : t("accounts.usageStats.periodNow"), zone: displayTimeZoneLabel() })}</div>
      <div className="text-muted-foreground">{t(`accounts.usageStats.periodSource.${periodSourceKey(stats.periodSource)}`, { mode: stats.periodSource?.split(":")[1] ?? "" })}</div>
      <div>{t("accounts.usageStats.requests", { total: n(stats.totalRequests), failed: n(stats.totalFailedRequests) })}</div>
      <div>{t("accounts.usageStats.tokens", { input: n(stats.totalInputTokens), cached: n(stats.totalCachedInputTokens), output: n(stats.totalOutputTokens), reasoning: n(stats.totalReasoningTokens), total: n(stats.totalTokens) })}</div>
      <div>{t("accounts.usageStats.cost", { total: formatUsageUSD(stats.totalCostUsdTicks), upstream: formatUsageUSD(stats.totalUpstreamCostUsdTicks), estimated: formatUsageUSD(stats.totalEstimatedCostUsdTicks) })}</div>
      <div className="pt-1 font-medium">{t("accounts.usageStats.tooltipToday", { date: stats.todayDate })}</div>
      {todayCurrent ? (
        <>
          {todayFromReset ? <div className="text-muted-foreground">{t("accounts.usageStats.todayFromReset", { time: todayFromReset })}</div> : null}
          <div>{t("accounts.usageStats.requests", { total: n(stats.todayRequests), failed: n(stats.todayFailedRequests) })}</div>
          <div>{t("accounts.usageStats.tokensToday", { input: n(stats.todayInputTokens), cached: n(stats.todayCachedInputTokens), output: n(stats.todayOutputTokens), total: n(stats.todayTokens) })}</div>
          <div>{t("accounts.usageStats.costToday", { total: formatUsageUSD(stats.todayCostUsdTicks) })}</div>
        </>
      ) : <div className="text-amber-600 dark:text-amber-400">{t("accounts.usageStats.staleToday", { date: stats.todayDate })}</div>}
      <div className="pt-1 text-muted-foreground">{t("accounts.usageStats.costNote")}</div>
      <div className="text-muted-foreground">{t("accounts.usageStats.exclusion")}</div>
      <div className="text-muted-foreground">{t("accounts.usageStats.computedAt", { time: `${formatDateTime(stats.computedAt, i18n.language)} (${displayTimeZoneLabel()})` })}</div>
    </div>
  );
  const items: Array<[string, string]> = [
    [t("accounts.usageStats.totalRequests"), formatUsageCompact(stats.totalRequests)],
    [t("accounts.usageStats.totalTokens"), formatUsageCompact(stats.totalTokens)],
    [t("accounts.usageStats.totalCost"), formatUsageUSD(stats.totalCostUsdTicks)],
    [t("accounts.usageStats.todayCost"), formatUsageUSD(todayCost)],
  ];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {variant === "row" ? (
          <div className="mt-1 flex w-fit flex-wrap gap-1">
            {items.map(([label, value]) => <span key={label} className={chip}>{label} {value}</span>)}
          </div>
        ) : (
          <div className="grid w-fit grid-cols-2 gap-x-4 gap-y-0.5 text-sm tabular-nums">
            {items.map(([label, value]) => <div key={label}><span className="text-muted-foreground">{label}</span> {value}</div>)}
          </div>
        )}
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">{tooltip}</TooltipContent>
    </Tooltip>
  );
}

/** 今日起算点不是北京时间零点时（周期在今天重置），返回 HH:mm（北京时间）。 */
function todayResetTime(todayStart?: string): string | null {
  if (!todayStart) return null;
  const date = new Date(todayStart);
  if (Number.isNaN(date.getTime())) return null;
  const hm = new Intl.DateTimeFormat("en-GB", { timeZone: "Asia/Shanghai", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date);
  return hm === "00:00:00" ? null : hm.slice(0, 5);
}
