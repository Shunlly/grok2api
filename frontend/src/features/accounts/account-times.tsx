import { useTranslation } from "react-i18next";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AccountDTO } from "@/features/accounts/accounts-api";
import { expiryState } from "@/features/accounts/account-expiry";
import { formatDateTime, formatRelativeFuture, formatResetClock } from "@/shared/lib/format";

function TimeLine({ label, at, source, className, tone }: { label: string; at?: string; source?: string; className?: string; tone?: string }) {
  const { t, i18n } = useTranslation();
  const base = className ?? "mt-1 w-fit text-[11px] tabular-nums";
  if (!at || Number.isNaN(Date.parse(at))) {
    return <div className={`${base} text-muted-foreground`}>{label}-</div>;
  }
  const text = t("accounts.nextResetValue", { time: formatResetClock(at), relative: formatRelativeFuture(at, i18n.language) });
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className={`${base} ${tone ?? "text-muted-foreground"}`}>{label}{text}</div>
      </TooltipTrigger>
      <TooltipContent>
        <div>{formatDateTime(at, i18n.language)}</div>
        {source ? <div className="text-muted-foreground">{source}</div> : null}
      </TooltipContent>
    </Tooltip>
  );
}

/** 过期时间：浏览器本地时间 + 相对时间；已过期红色、3 天内琥珀色；未知显示 "-"。 */
export function AccountExpiry({ account, showLabel = true, className }: { account: Pick<AccountDTO, "expiresAt" | "expiresSource" | "expiresAutoRenew">; showLabel?: boolean; className?: string }) {
  const { t } = useTranslation();
  const state = expiryState(account.expiresAt, account.expiresAutoRenew);
  const tone = state === "expired" ? "font-medium text-red-600 dark:text-red-400" : state === "soon" ? "font-medium text-amber-600 dark:text-amber-400" : undefined;
  const parts: string[] = [];
  if (account.expiresSource) parts.push(t(`accounts.expiresSource.${account.expiresSource}`, { defaultValue: account.expiresSource }));
  if (account.expiresAutoRenew) parts.push(t("accounts.expiresAutoRenew"));
  return <TimeLine label={showLabel ? `${t("accounts.expiry")} ` : ""} at={account.expiresAt} source={parts.join(" · ")} className={className} tone={tone} />;
}

/** 最近使用时间：最近一次成功服务的真实请求（排除探针/考题）。 */
export function AccountLastUsed({ account, showLabel = true, className }: { account: Pick<AccountDTO, "lastUsedAt" | "lastUsedSource">; showLabel?: boolean; className?: string }) {
  const { t } = useTranslation();
  const source = account.lastUsedSource ? t(`accounts.lastUsedSource.${account.lastUsedSource}`, { defaultValue: account.lastUsedSource }) : "";
  return <TimeLine label={showLabel ? `${t("accounts.lastUsed")} ` : ""} at={account.lastUsedAt} source={source} className={className} />;
}
