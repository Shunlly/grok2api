import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FlaskConical, Play } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { getCanarySummary, runCanaryRound, type CanaryResultDTO } from "@/features/quality-guard/quality-guard-api";
import { EmptyState, ErrorState } from "@/shared/components/data-state";
import { ApiError } from "@/shared/api/client";
import { cn } from "@/shared/lib/cn";
import { formatCompactDateTime } from "@/shared/lib/format";

export function CanaryPanel() {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["quality-canary"], queryFn: getCanarySummary, refetchInterval: (q) => (q.state.data?.running ? 5_000 : 30_000) });
  const run = useMutation({
    mutationFn: runCanaryRound,
    onSuccess: (value) => {
      toast.success(t("qualityGuard.canary.triggered", { id: value.roundId }));
      void queryClient.invalidateQueries({ queryKey: ["quality-canary"] });
    },
    onError: (error) => toast.error(error instanceof ApiError && error.status === 409 ? t("qualityGuard.canary.conflict") : error instanceof Error ? error.message : String(error)),
  });
  const data = query.data;
  if (query.isError && !data) return <ErrorState message={query.error.message} onRetry={() => void query.refetch()} />;
  if (!data) return null;
  const when = (value?: string) => (value ? formatCompactDateTime(value, i18n.language) : "-");

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3 rounded-lg border p-4">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 font-medium"><FlaskConical className="size-4" />{t("qualityGuard.canary.title")}<Badge variant="outline">{t("qualityGuard.canary.observeOnly")}</Badge></div>
          <p className="text-xs text-muted-foreground">{t("qualityGuard.canary.description", { count: data.suspectAfter })}</p>
          <p className="text-xs text-muted-foreground tabular-nums">
            {data.enabled ? `${t("qualityGuard.canary.interval")} ${data.interval}` : t("qualityGuard.canary.disabled")} · {t("qualityGuard.canary.model")} {data.model} ({data.reasoningEffort}, ≤{data.maxOutputTokens}) · {t("qualityGuard.canary.lastRun")} {when(data.lastRun)} · {t("qualityGuard.canary.nextRun")} {when(data.nextRun)}
          </p>
        </div>
        <Button type="button" size="sm" onClick={() => run.mutate()} disabled={run.isPending || data.running}><Play />{data.running ? t("qualityGuard.canary.running") : t("qualityGuard.canary.run")}</Button>
      </div>
      {data.accounts.length === 0 ? <EmptyState message={t("qualityGuard.canary.none")} /> : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("qualityGuard.canary.account")}</TableHead>
                <TableHead>{t("qualityGuard.canary.latest")}</TableHead>
                <TableHead>{t("qualityGuard.canary.prompt")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.score")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.reasoning")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.poolMedian")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.duration")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.fails")}</TableHead>
                <TableHead className="text-right">{t("qualityGuard.canary.passRate")}</TableHead>
                <TableHead>{t("qualityGuard.canary.history")}</TableHead>
                <TableHead>{t("qualityGuard.canary.reasons")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.accounts.map((account) => (
                <TableRow key={account.accountId}>
                  <TableCell className="text-xs"><span className="font-medium">{account.accountName || `#${account.accountId}`}</span><span className="ml-1 text-muted-foreground">#{account.accountId}</span></TableCell>
                  <TableCell><div className="flex items-center gap-1"><StatusBadge value={account.latest} />{account.latest.suspect ? <Badge variant="outline" className="text-destructive">{t("qualityGuard.canary.suspect")}</Badge> : null}</div><div className="mt-1 text-[11px] text-muted-foreground">{when(account.latest.createdAt)}</div></TableCell>
                  <TableCell className="font-mono text-xs">{account.latest.promptId}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{account.latest.status === "pass" || account.latest.status === "fail" ? account.latest.score : "-"}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{account.latest.reasoningTokens}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{Math.round(account.latest.poolMedianReasoning)}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{account.latest.durationMs ? `${(account.latest.durationMs / 1000).toFixed(1)}s` : "-"}</TableCell>
                  <TableCell className={cn("text-right font-mono text-xs tabular-nums", account.latest.consecutiveFails >= data.suspectAfter && "text-destructive")}>{account.latest.consecutiveFails}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{Math.round(account.passRate * 100)}%</TableCell>
                  <TableCell><div className="flex gap-0.5">{account.history.slice().reverse().map((item) => <span key={item.id} title={`${when(item.createdAt)} ${item.promptId} ${item.status} ${item.score}`} className={cn("inline-block size-2.5 rounded-sm", item.status === "pass" ? "bg-emerald-500" : item.status === "fail" ? "bg-destructive" : "bg-muted-foreground/40")} />)}</div></TableCell>
                  <TableCell className="max-w-72 truncate text-xs text-muted-foreground" title={account.latest.error || account.latest.reasons}>{account.latest.error || account.latest.reasons || "-"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ value }: { value: CanaryResultDTO }) {
  const { t } = useTranslation();
  const tone = value.status === "pass" ? "text-emerald-600 dark:text-emerald-400" : value.status === "fail" ? "text-destructive" : "text-muted-foreground";
  return <Badge variant="outline" className={tone}>{t(`qualityGuard.canary.${value.status}`)}</Badge>;
}
