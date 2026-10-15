import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getQuotaAutoSyncStatus, type AccountDTO } from "@/features/accounts/accounts-api";
import { credentialIssues, expiryState } from "@/features/accounts/account-expiry";
import { quotaSyncHintText } from "@/features/accounts/account-quota-sync-hint";
import { displayTimeZoneLabel, formatDateTime, formatRelativeFuture, formatResetClock } from "@/shared/lib/format";

function TimeLine({ label, at, source, className, tone, emptyHint }: { label: string; at?: string; source?: string; className?: string; tone?: string; emptyHint?: string }) {
  const { t, i18n } = useTranslation();
  const base = className ?? "mt-1 w-fit text-[11px] tabular-nums";
  if (!at || Number.isNaN(Date.parse(at))) {
    const empty = <div className={`${base} text-muted-foreground`}>{label}-</div>;
    if (!emptyHint) return empty;
    return (
      <Tooltip>
        <TooltipTrigger asChild>{empty}</TooltipTrigger>
        <TooltipContent className="max-w-xs">{emptyHint}</TooltipContent>
      </Tooltip>
    );
  }
  const text = t("accounts.nextResetValue", { time: formatResetClock(at), relative: formatRelativeFuture(at, i18n.language) });
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className={`${base} ${tone ?? "text-muted-foreground"}`}>{label}{text}</div>
      </TooltipTrigger>
      <TooltipContent>
        <div>{formatDateTime(at, i18n.language)} ({displayTimeZoneLabel()})</div>
        {source ? <div className="text-muted-foreground">{source}</div> : null}
      </TooltipContent>
    </Tooltip>
  );
}

type CredentialFields = Pick<AccountDTO, "authType" | "authStatus" | "refreshable" | "refreshFailureCount" | "expiresAt" | "lastRefreshAt" | "lastRefreshErrorStatus" | "lastRefreshErrorCode" | "lastRefreshErrorMessage">;

/**
 * 凭证需重新登录：只在凭证无法自动续期时出现（无 refresh token / 续期失败 / 已过期未续上）。
 * authStatus=reauthRequired 时状态列已有「需重新登录」徽章，这里不重复显示。
 */
export function AccountCredentialWarning({ account, className }: { account: CredentialFields; className?: string }) {
  const { t, i18n } = useTranslation();
  if (account.authStatus === "reauthRequired") return null;
  const issues = credentialIssues(account);
  if (!issues.length) return null;
  const lastError = [account.lastRefreshErrorStatus, account.lastRefreshErrorCode, account.lastRefreshErrorMessage].filter(Boolean).join(" · ");
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className={className ?? "mt-1 inline-block w-fit cursor-help rounded bg-amber-100 px-1.5 text-[11px] font-medium text-amber-800 dark:bg-amber-950 dark:text-amber-300"}>{t("accounts.credentialWarning.badge")}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <div className="space-y-0.5 text-xs">
          {issues.map((issue) => <div key={issue}>{t(`accounts.credentialWarning.reasons.${issue}`)}</div>)}
          <div>{t("accounts.credentialWarning.failures", { count: account.refreshFailureCount })}</div>
          {lastError ? <div className="break-words">{t("accounts.credentialWarning.lastError", { error: lastError })}</div> : null}
          {account.lastRefreshAt ? <div>{t("accounts.credentialWarning.lastRefresh", { time: formatDateTime(account.lastRefreshAt, i18n.language) })}</div> : null}
          <div>{t("accounts.credentialWarning.expiresAt", { time: account.expiresAt ? `${formatDateTime(account.expiresAt, i18n.language)} (${displayTimeZoneLabel()})` : "-" })}</div>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

type PlanFields = Pick<AccountDTO, "provider" | "planExpiresAt" | "planExpiresSource" | "planAutoRenew" | "planCancelAtPeriodEnd" | "planStatus" | "planName" | "planBillingInterval" | "planSyncedAt" | "planNoSubscription">;

/** 套餐到期：订阅当前计费周期结束时间（不是周额度窗口）。<3 天琥珀色、已过期红色；自动续订的不提示临期。 */
export function AccountPlanExpiry({ account, showLabel = true, className }: { account: PlanFields; showLabel?: boolean; className?: string }) {
  const { t, i18n } = useTranslation();
  const label = showLabel ? `${t("accounts.planExpiry")} ` : "";
  if (!account.planExpiresAt) {
    let hint = t("accounts.planExpiryUnavailable.notSynced");
    if (account.provider === "grok_console") hint = t("accounts.planExpiryUnavailable.console");
    else if (account.planNoSubscription) hint = t("accounts.planExpiryUnavailable.noSubscription");
    else if (account.provider === "grok_build" && !account.planExpiresSource) hint = t("accounts.planExpiryUnavailable.buildNoLinkedWeb");
    return <TimeLine label={label} className={className} emptyHint={hint} />;
  }
  const state = expiryState(account.planExpiresAt, false);
  const tone = state === "expired" ? "font-medium text-red-600 dark:text-red-400" : state === "soon" && !account.planAutoRenew ? "font-medium text-amber-600 dark:text-amber-400" : undefined;
  const parts: string[] = [];
  if (account.planExpiresSource) parts.push(t(`accounts.planExpiresSource.${account.planExpiresSource}`, { defaultValue: account.planExpiresSource }));
  parts.push(account.planAutoRenew ? t("accounts.planAutoRenew") : account.planCancelAtPeriodEnd ? t("accounts.planCancelAtPeriodEnd") : t("accounts.planRenewUnknown"));
  if (account.planName) parts.push(account.planName);
  if (account.planStatus) parts.push(account.planStatus.replace("SUBSCRIPTION_STATUS_", "").toLowerCase());
  if (account.planSyncedAt) parts.push(t("accounts.planSyncedAt", { time: formatDateTime(account.planSyncedAt, i18n.language) }));
  return <TimeLine label={label} at={account.planExpiresAt} source={parts.join(" · ")} className={className} tone={tone} />;
}

/** 最近使用时间：最近一次成功服务的真实请求（排除探针/考题）。 */
export function AccountLastUsed({ account, showLabel = true, className }: { account: Pick<AccountDTO, "lastUsedAt" | "lastUsedSource">; showLabel?: boolean; className?: string }) {
  const { t } = useTranslation();
  const source = account.lastUsedSource ? t(`accounts.lastUsedSource.${account.lastUsedSource}`, { defaultValue: account.lastUsedSource }) : "";
  return <TimeLine label={showLabel ? `${t("accounts.lastUsed")} ` : ""} at={account.lastUsedAt} source={source} className={className} />;
}

/** 本地时钟：同一天只显示 HH:mm，否则 MM-DD HH:mm（UTC+8）。 */
function compactClock(value: string, now: Date = new Date()): string {
  const full = formatResetClock(value);
  return full.slice(0, 5) === formatResetClock(now).slice(0, 5) ? full.slice(6) : full;
}


/** 额度刷新：上次同步 · 下次刷新（定时 / 重置后），tooltip 说明同步规则。 */
export function AccountQuotaSyncHint({ account, className }: { account: Pick<AccountDTO, "lastQuotaSyncAt" | "nextQuotaSyncAt" | "nextQuotaSyncReason" | "resetQuotaSyncAt">; className?: string }) {
  const { t, i18n } = useTranslation();
  const status = useQuery({ queryKey: ["accounts", "auto-sync-status"], queryFn: getQuotaAutoSyncStatus, refetchInterval: 60_000, staleTime: 30_000 }).data;
  const text = quotaSyncHintText(account, t as never, (value) => compactClock(value));
  const lines: string[] = [];
  if (status?.enabled) {
    lines.push(t("accounts.quotaSyncHint.tipHourly", { minutes: Math.round(status.intervalSeconds / 60) }));
    lines.push(t("accounts.quotaSyncHint.tipReset", { seconds: 90 }));
  } else if (status) {
    lines.push(t("accounts.quotaSyncHint.autoSyncDisabled"));
  }
  if (account.resetQuotaSyncAt) lines.push(t("accounts.quotaSyncHint.tipResetAt", { time: formatDateTime(account.resetQuotaSyncAt, i18n.language) }));
  lines.push(t("accounts.quotaSyncHint.tipManual"), t("accounts.quotaSyncHint.tipZone"));
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className={className ?? "mt-1 w-fit cursor-help text-[11px] tabular-nums text-muted-foreground"}>{text}</div>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <div className="space-y-0.5 text-xs">{lines.map((line) => <div key={line}>{line}</div>)}</div>
      </TooltipContent>
    </Tooltip>
  );
}
