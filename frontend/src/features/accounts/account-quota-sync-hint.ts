export type QuotaSyncFields = { lastQuotaSyncAt?: string; nextQuotaSyncAt?: string; nextQuotaSyncReason?: string; resetQuotaSyncAt?: string };

/** 额度刷新提示行文本（纯函数，便于测试）。 */
export function quotaSyncHintText(account: QuotaSyncFields, t: (key: string, options?: Record<string, unknown>) => string, clock: (value: string) => string): string {
  const last = account.lastQuotaSyncAt ? t("accounts.quotaSyncHint.last", { time: clock(account.lastQuotaSyncAt) }) : t("accounts.quotaSyncHint.never");
  const reason = account.nextQuotaSyncReason;
  let next: string;
  if (account.nextQuotaSyncAt && (reason === "hourly" || reason === "reset")) {
    next = t(reason === "reset" ? "accounts.quotaSyncHint.nextReset" : "accounts.quotaSyncHint.next", { time: clock(account.nextQuotaSyncAt) });
  } else if (reason && reason !== "hourly" && reason !== "reset") {
    next = t(`accounts.quotaSyncHint.${reason}`);
  } else {
    next = "";
  }
  return next ? `${last} · ${next}` : last;
}

