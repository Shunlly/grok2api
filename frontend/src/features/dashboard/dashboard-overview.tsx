import { useQuery } from "@tanstack/react-query";
import { Activity, AlertTriangle, Zap, CircleDollarSign, Gauge, Info, UsersRound, WholeWord, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Pie, PieChart } from "recharts";

import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getAccountConcurrency, getAccountSummary } from "@/features/accounts/accounts-api";
import { buildPoolOutage } from "@/features/dashboard/build-outage";
import type { DashboardDTO } from "@/features/dashboard/dashboard-api";
import { formatUSD, formatUSDValue } from "@/features/dashboard/dashboard-format";
import { DashboardPanel } from "@/features/dashboard/dashboard-panel";
import { cn } from "@/shared/lib/cn";
import { formatDuration, formatNumber } from "@/shared/lib/format";
import { usdTicksToValue } from "@/shared/lib/usd";

type DashboardDataProps = {
  dashboard?: DashboardDTO;
  locale: string;
  loading: boolean;
};

export function DashboardOverview({ dashboard, locale, loading }: DashboardDataProps) {
  const { t } = useTranslation();
  const resources = dashboard?.resources;
  const usage = dashboard?.usage;
  const concurrencyQuery = useQuery({ queryKey: ["accounts", "concurrency"], queryFn: getAccountConcurrency, refetchInterval: 5_000 });
  const summaryQuery = useQuery({ queryKey: ["accounts", "summary"], queryFn: getAccountSummary, refetchInterval: 15_000 });
  const buildOutage = buildPoolOutage(summaryQuery.data);
  const cacheHitRate = (usage?.inputTokens ?? 0) > 0 ? (usage?.cachedInputTokens ?? 0) / (usage?.inputTokens ?? 1) * 100 : 0;
  const averageRequestCost = (usage?.requests ?? 0) > 0 ? usdTicksToValue(usage?.billedCostUsdTicks ?? 0) / (usage?.requests ?? 1) : 0;
  const hasFirstTokenSamples = (usage?.firstTokenSamples ?? 0) > 0;
  const hasThroughputSamples = (usage?.throughputSamples ?? 0) > 0;
  const performanceDetail = hasThroughputSamples
    ? t("audits.averageOutputSpeed", { speed: formatNumber(usage?.outputTokensPerSecond ?? 0, locale, 1), count: formatNumber(usage?.throughputSamples ?? 0, locale) })
    : t(hasFirstTokenSamples ? "audits.throughputPending" : "audits.performancePending");

  return (
    <section aria-label={t("dashboard.usage")}>
      {buildOutage ? (
        <div role="alert" className="mb-2 flex items-start gap-2 rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <div className="min-w-0">
            <p className="font-medium">{t("dashboard.buildOutageTitle")}</p>
            <p className="break-words text-xs opacity-90">{t("dashboard.buildOutageDetail", { total: formatNumber(buildOutage.total, locale) })}</p>
          </div>
        </div>
      ) : null}
      <div className="grid grid-cols-2 gap-2 xl:grid-cols-6">
        <DashboardMetric
          icon={Zap}
          label={t("dashboard.currentConcurrency")}
          value={formatNumber(concurrencyQuery.data?.total ?? 0, locale)}
          detail={(concurrencyQuery.data?.queued ?? 0) > 0 ? t("dashboard.currentConcurrencyQueuedDetail", { count: formatNumber(concurrencyQuery.data?.activeAccounts ?? 0, locale), queued: formatNumber(concurrencyQuery.data?.queued ?? 0, locale) }) : t("dashboard.currentConcurrencyDetail", { count: formatNumber(concurrencyQuery.data?.activeAccounts ?? 0, locale) })}
          loading={concurrencyQuery.isPending}
        />
        <DashboardMetric
          icon={UsersRound}
          label={t("dashboard.accountCount")}
          value={formatNumber(resources?.totalAccounts ?? 0, locale)}
          detail={t("dashboard.accountDistribution", { build: formatNumber(resources?.buildAccounts ?? 0, locale), web: formatNumber(resources?.webAccounts ?? 0, locale), console: formatNumber(resources?.consoleAccounts ?? 0, locale) })}
          loading={loading}
        />
        <DashboardMetric
          icon={Activity}
          label={t("dashboard.requests")}
          value={formatNumber(usage?.requests ?? 0, locale)}
          detail={t("dashboard.requestSuccessRate", { rate: formatNumber(usage?.successRate ?? 0, locale, 1) })}
          loading={loading}
        />
        <DashboardMetric
          icon={WholeWord}
          label={t("dashboard.tokens")}
          value={formatNumber(usage?.tokens ?? 0, locale)}
          detail={t("dashboard.tokenEfficiency", { rate: formatNumber(cacheHitRate, locale, 1) })}
          loading={loading}
        />
        <DashboardMetric
          icon={CircleDollarSign}
          label={t("dashboard.billing")}
          value={formatUSD(usage?.billedCostUsdTicks ?? 0, locale)}
          detail={t("dashboard.averageRequestCost", { cost: formatUSDValue(averageRequestCost, locale) })}
          loading={loading}
        />
        <DashboardMetric
          icon={Gauge}
          label={t("dashboard.ttftTitle")}
          value={hasFirstTokenSamples ? formatDuration(usage?.medianFirstTokenMs ?? usage?.averageFirstTokenMs ?? 0) : "—"}
          detail={hasFirstTokenSamples
            ? t("dashboard.ttftDetail", { avg: formatDuration(usage?.averageFirstTokenMs ?? 0), p90: formatDuration(usage?.p90FirstTokenMs ?? 0), count: formatNumber(usage?.firstTokenSamples ?? 0, locale) })
            : performanceDetail}
          hint={<FirstTokenHint usage={usage} locale={locale} performanceDetail={performanceDetail} />}
          loading={loading}
        />
      </div>
    </section>
  );
}

export function DashboardResources({ dashboard, locale, loading }: DashboardDataProps) {
  const { t } = useTranslation();
  const resources = dashboard?.resources;
  const activeAccounts = resources?.activeAccounts ?? 0;
  const totalAccounts = resources?.totalAccounts ?? 0;
  const unavailableAccounts = Math.max(0, totalAccounts - activeAccounts);
  const availability = totalAccounts > 0 ? activeAccounts / totalAccounts * 100 : 0;
  const chartConfig = {
    active: {
      label: t("dashboard.activeAccounts"),
      theme: { light: "oklch(0.68 0.14 160)", dark: "oklch(0.74 0.12 160)" },
    },
    unavailable: {
      label: t("dashboard.unavailableAccounts"),
      theme: { light: "oklch(0.88 0.01 250)", dark: "oklch(0.36 0.01 250)" },
    },
  } satisfies ChartConfig;
  const chartData = totalAccounts > 0 ? [
    { status: "active", value: activeAccounts, fill: "var(--color-active)" },
    { status: "unavailable", value: unavailableAccounts, fill: "var(--color-unavailable)" },
  ] : [
    { status: "unavailable", value: 1, fill: "var(--color-unavailable)" },
  ];

  return (
    <DashboardPanel
      id="dashboard-resources-title"
      title={t("dashboard.resourcesTitle")}
      className="flex h-full min-h-[360px] flex-col"
      contentClassName="flex flex-1"
    >
      {loading ? (
        <div className="flex min-h-[260px] items-center justify-center"><Spinner className="size-5" /></div>
      ) : (
        <div className="grid min-h-[260px] w-full flex-1 grid-cols-[minmax(0,1fr)_minmax(128px,0.8fr)] items-center gap-4">
          <div className="relative mx-auto size-44 max-w-full">
            <ChartContainer config={chartConfig} className="size-full aspect-square" aria-label={t("dashboard.resourcesTitle")}>
              <PieChart>
                <Pie
                  data={chartData}
                  dataKey="value"
                  nameKey="status"
                  innerRadius={56}
                  outerRadius={78}
                  paddingAngle={activeAccounts > 0 && unavailableAccounts > 0 ? 3 : 0}
                  strokeWidth={0}
                  animationDuration={700}
                  animationEasing="ease-out"
                />
              </PieChart>
            </ChartContainer>
            <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
              <span className="text-2xl font-medium tabular-nums">{formatNumber(availability, locale, 0)}%</span>
              <span className="mt-1 text-[10px] text-muted-foreground">{t("dashboard.availability")}</span>
            </div>
          </div>

          <div className="min-w-0 divide-y divide-border/60">
            <ResourceSummary
              color="bg-emerald-500"
              label={t("dashboard.activeAccounts")}
              value={formatNumber(activeAccounts, locale)}
              detail={t("dashboard.availableSummary", { active: formatNumber(activeAccounts, locale), total: formatNumber(totalAccounts, locale) })}
            />
            <ResourceSummary
              color="bg-muted-foreground/35"
              label={t("dashboard.unavailableAccounts")}
              value={formatNumber(unavailableAccounts, locale)}
              detail={t("dashboard.unavailableSummary", { unavailable: formatNumber(unavailableAccounts, locale), total: formatNumber(totalAccounts, locale) })}
            />
            <ResourceSummary
              color="bg-violet-500"
              label={t("dashboard.enabledModels")}
              value={formatNumber(resources?.enabledModels ?? 0, locale)}
              detail={t("dashboard.modelsAvailableSummary", { enabled: formatNumber(resources?.enabledModels ?? 0, locale), total: formatNumber(resources?.totalModels ?? 0, locale) })}
            />
            <ResourceSummary
              color="bg-sky-500"
              label={t("dashboard.activeClientKeys")}
              value={formatNumber(resources?.activeClientKeys ?? 0, locale)}
              detail={t("dashboard.keysAvailableSummary", { active: formatNumber(resources?.activeClientKeys ?? 0, locale), total: formatNumber(resources?.totalClientKeys ?? 0, locale) })}
            />
          </div>
        </div>
      )}
    </DashboardPanel>
  );
}

function FirstTokenHint({ usage, locale, performanceDetail }: { usage?: DashboardDTO["usage"]; locale: string; performanceDetail: string }) {
  const { t } = useTranslation();
  const buckets = usage?.firstTokenBuckets ?? [];
  return (
    <div className="max-w-72 space-y-2 text-xs">
      <p>{t("dashboard.ttftHint")}</p>
      {buckets.length > 0 ? (
        <div>
          <p className="mb-1 font-medium">{t("dashboard.ttftBreakdownTitle")}</p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 tabular-nums">
            {buckets.map((bucket) => (
              <div key={bucket.key} className="contents">
                <dt className="opacity-80">{t(`dashboard.ttftBuckets.${bucket.key}`, { defaultValue: bucket.key })}</dt>
                <dd className="text-right">{bucket.samples > 0 ? t("dashboard.ttftBucketRow", { count: formatNumber(bucket.samples, locale), median: formatDuration(bucket.medianMs) }) : t("dashboard.ttftBucketEmpty")}</dd>
              </div>
            ))}
          </dl>
        </div>
      ) : null}
      <p className="opacity-80">{performanceDetail}</p>
    </div>
  );
}

function DashboardMetric({ icon: Icon, label, value, detail, loading, hint }: { icon: LucideIcon; label: string; value: string; detail: string; loading: boolean; hint?: ReactNode }) {
  return (
    <article className="min-h-24 min-w-0 rounded-lg bg-card p-3 sm:min-h-28 sm:p-4" aria-busy={loading}>
      <header className="flex min-h-5 items-center justify-between gap-3">
        {hint ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="button" className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                {label}
                <Info className="size-3" aria-hidden />
              </button>
            </TooltipTrigger>
            <TooltipContent>{hint}</TooltipContent>
          </Tooltip>
        ) : (
          <span className="text-xs text-muted-foreground">{label}</span>
        )}
        <Icon className="size-4 shrink-0 text-muted-foreground" />
      </header>
      <div className="mt-2 flex min-h-8 items-center text-xl font-medium tracking-tight tabular-nums sm:mt-3 sm:text-2xl">{loading ? <Spinner /> : value}</div>
      <p className={cn("mt-1.5 min-h-4 truncate text-[11px] text-muted-foreground", loading && "invisible")} title={detail}>{detail}</p>
    </article>
  );
}

function ResourceSummary({ color, label, value, detail }: { color: string; label: string; value: string; detail: string }) {
  return (
    <div className="flex items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
      <div className="flex min-w-0 items-center gap-2">
        <span className={cn("size-2 shrink-0 rounded-full", color)} />
        <div className="min-w-0">
          <p className="truncate text-xs">{label}</p>
          <p className="mt-0.5 truncate text-[10px] text-muted-foreground" title={detail}>{detail}</p>
        </div>
      </div>
      <span className="shrink-0 text-sm font-medium tabular-nums">{value}</span>
    </div>
  );
}
