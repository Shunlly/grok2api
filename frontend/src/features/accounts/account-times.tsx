import { useTranslation } from "react-i18next";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AccountDTO } from "@/features/accounts/accounts-api";
import { expiryState } from "@/features/accounts/account-expiry";
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

/** 凭证过期（Access Token）：显示时区（默认 UTC+8）+ 相对时间；已过期红色、3 天内琥珀色；未知显示 "-"。 */
export function AccountExpiry({ account, showLabel = true, className }: { account: Pick<AccountDTO, "expiresAt" | "expiresSource" | "expiresAutoRenew">; showLabel?: boolean; className?: string }) {
  const { t } = useTranslation();
  const state = expiryState(account.expiresAt, account.expiresAutoRenew);
  const tone = state === "expired" ? "font-medium text-red-600 dark:text-red-400" : state === "soon" ? "font-medium text-amber-600 dark:text-amber-400" : undefined;
  const parts: string[] = [];
  if (account.expiresSource) parts.push(t(`accounts.expiresSource.${account.expiresSource}`, { defaultValue: account.expiresSource }));
  if (account.expiresAutoRenew) parts.push(t("accounts.expiresAutoRenew"));
  return <TimeLine label={showLabel ? `${t("accounts.expiry")} ` : ""} at={account.expiresAt} source={parts.join(" · ")} className={className} tone={tone} emptyHint={t("accounts.expiryUnknownHint")} />;
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
