import { useTranslation } from "react-i18next";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AccountDTO } from "@/features/accounts/accounts-api";
import { formatDateTime, formatRelativeFuture, formatResetClock } from "@/shared/lib/format";

/** 显示账号下次额度重置时间：浏览器本地时区绝对时间 + 相对倒计时；未知显示 "-"。 */
export function AccountNextReset({ account, showLabel = true, className }: { account: Pick<AccountDTO, "nextResetAt" | "nextResetSource">; showLabel?: boolean; className?: string }) {
  const { t, i18n } = useTranslation();
  const at = account.nextResetAt;
  const label = showLabel ? `${t("accounts.nextReset")} ` : "";
  if (!at || Number.isNaN(Date.parse(at))) {
    return <div className={className ?? "mt-1 text-[11px] text-muted-foreground"}>{label}{t("accounts.nextResetUnknown")}</div>;
  }
  const text = t("accounts.nextResetValue", { time: formatResetClock(at), relative: formatRelativeFuture(at, i18n.language) });
  const [kind, subject] = (account.nextResetSource ?? "").split(/:(.*)/s);
  const source = kind ? t(`accounts.nextResetSource.${kind}`, { mode: subject, model: subject, defaultValue: account.nextResetSource }) : "";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className={className ?? "mt-1 w-fit text-[11px] text-muted-foreground tabular-nums"}>{label}{text}</div>
      </TooltipTrigger>
      <TooltipContent>
        <div>{formatDateTime(at, i18n.language)}</div>
        {source ? <div className="text-muted-foreground">{source}</div> : null}
      </TooltipContent>
    </Tooltip>
  );
}
