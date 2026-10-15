/**
 * Display time zone for the admin console. Browser local time is not reliable
 * (e.g. a Mac whose system zone is US Pacific while the operator lives in
 * UTC+8 rendered "10-15 01:58 · 4小时后" at 13:00 UTC+8). Default Asia/Shanghai;
 * override with localStorage "grok2api.timeZone" (IANA name).
 */
export const DEFAULT_DISPLAY_TIME_ZONE = "Asia/Shanghai";

let cachedTimeZone: string | undefined;

export function displayTimeZone(): string {
  if (cachedTimeZone) return cachedTimeZone;
  let value = DEFAULT_DISPLAY_TIME_ZONE;
  try {
    const stored = globalThis.localStorage?.getItem("grok2api.timeZone")?.trim();
    if (stored) {
      new Intl.DateTimeFormat("en", { timeZone: stored });
      value = stored;
    }
  } catch {
    value = DEFAULT_DISPLAY_TIME_ZONE;
  }
  cachedTimeZone = value;
  return value;
}

/** Short label such as "UTC+8" for the display time zone at the given instant. */
export function displayTimeZoneLabel(at: Date = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-US", { timeZone: displayTimeZone(), timeZoneName: "shortOffset" }).formatToParts(at);
  return (parts.find((part) => part.type === "timeZoneName")?.value ?? "UTC").replace("GMT", "UTC");
}

const dateTimeFormatters = new Map<string, Intl.DateTimeFormat>();
const compactDateTimeFormatters = new Map<string, Intl.DateTimeFormat>();
const numberFormatters = new Map<string, Intl.NumberFormat>();
const tokenMillionFormatters = new Map<string, Intl.NumberFormat>();

export function formatDateTime(value: string | null | undefined, locale: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  let formatter = dateTimeFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale, {
      dateStyle: "medium",
      timeStyle: "short",
      hourCycle: "h23",
      timeZone: displayTimeZone(),
    });
    dateTimeFormatters.set(locale, formatter);
  }
  return formatter.format(date);
}

export function formatCompactDateTime(value: string | null | undefined, locale: string): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  let formatter = compactDateTimeFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale, { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZone: displayTimeZone() });
    compactDateTimeFormatters.set(locale, formatter);
  }
  const parts = formatter.formatToParts(date);
  return `${datePart(parts, "year")}/${datePart(parts, "month")}/${datePart(parts, "day")} ${datePart(parts, "hour")}:${datePart(parts, "minute")}:${datePart(parts, "second")}`;
}

function datePart(parts: Intl.DateTimeFormatPart[], type: Intl.DateTimeFormatPartTypes): string {
  return parts.find((part) => part.type === type)?.value ?? "--";
}

export function formatNumber(value: number, locale: string, maximumFractionDigits = 2): string {
  const key = `${locale}:${maximumFractionDigits}`;
  let formatter = numberFormatters.get(key);
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, { maximumFractionDigits });
    numberFormatters.set(key, formatter);
  }
  return formatter.format(value);
}

/** Uses M for large token counts while keeping small values readable. */
export function formatTokenMillions(value: number, locale: string): string {
  const normalized = Number.isFinite(value) ? Math.max(0, value) : 0;
  if (normalized < 1_000) return formatNumber(normalized, locale, 0);
  let formatter = tokenMillionFormatters.get(locale);
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, { maximumSignificantDigits: 3 });
    tokenMillionFormatters.set(locale, formatter);
  }
  return `${formatter.format(normalized / 1_000_000)}M`;
}

export function formatDuration(milliseconds: number): string {
  if (milliseconds < 1000) {
    return `${milliseconds} ms`;
  }
  return `${(milliseconds / 1000).toFixed(milliseconds < 10000 ? 2 : 1)} s`;
}

export function toDateTimeLocal(value: string | null | undefined): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 19);
}


/** 以控制台显示时区（默认 UTC+8）格式化为 "MM-DD HH:mm"，24 小时制。 */
export function formatResetClock(value: string | Date): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const parts = resetClockFormatter().formatToParts(date);
  return `${datePart(parts, "month")}-${datePart(parts, "day")} ${datePart(parts, "hour")}:${datePart(parts, "minute")}`;
}

let resetClock: Intl.DateTimeFormat | undefined;
function resetClockFormatter(): Intl.DateTimeFormat {
  resetClock ??= new Intl.DateTimeFormat("en-US", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: displayTimeZone() });
  return resetClock;
}

/** 相对倒计时，例如 "1天后" / "in 1 day"；未来 1 小时内精确到分钟。 */
export function formatRelativeFuture(value: string | Date, locale: string, now: Date = new Date()): string {
  const date = value instanceof Date ? value : new Date(value);
  const diffMS = date.getTime() - now.getTime();
  if (Number.isNaN(diffMS)) return "-";
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto", style: "short" });
  const minutes = Math.round(diffMS / 60_000);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(diffMS / 3_600_000);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(diffMS / 86_400_000), "day");
}
