import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Fingerprint, Play, RotateCw } from "lucide-react";
import { Fragment, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { getFingerprintDetail, getFingerprintSummary, runFingerprintRound, type FingerprintAccountDTO, type FingerprintResultDTO } from "@/features/quality-guard/quality-guard-api";
import { ApiError } from "@/shared/api/client";
import { EmptyState, ErrorState } from "@/shared/components/data-state";
import { cn } from "@/shared/lib/cn";
import { formatCompactDateTime } from "@/shared/lib/format";

const pct = (value: number) => `${Math.round(value * 100)}%`;
const secs = (ms: number) => (ms ? `${(ms / 1000).toFixed(1)}s` : "-");

export function FingerprintPanel() {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState<string | null>(null);
  const query = useQuery({ queryKey: ["quality-fingerprint"], queryFn: getFingerprintSummary, refetchInterval: (q) => (q.state.data?.running ? 5_000 : 30_000) });
  const run = useMutation({
    mutationFn: (ids: number[]) => runFingerprintRound(ids),
    onSuccess: (value) => {
      toast.success(t("qualityGuard.fingerprint.triggered", { id: value.roundId }));
      void queryClient.invalidateQueries({ queryKey: ["quality-fingerprint"] });
    },
    onError: (error) => toast.error(error instanceof ApiError && error.status === 409 ? t("qualityGuard.fingerprint.conflict") : error instanceof Error ? error.message : String(error)),
  });
  const data = query.data;
  if (query.isError && !data) return <ErrorState message={query.error.message} onRetry={() => void query.refetch()} />;
  if (!data) return null;
  const when = (value?: string) => (value ? formatCompactDateTime(value, i18n.language) : "-");
  const busy = run.isPending || data.running;
  const keyOf = (row: FingerprintAccountDTO) => `${row.accountId}/${row.model}`;
  const toggle = (row: FingerprintAccountDTO) => setExpanded((value) => (value === keyOf(row) ? null : keyOf(row)));
  const rerun = (row: FingerprintAccountDTO) => run.mutate([row.accountId]);

  return (
    <div className="space-y-4" data-testid="fingerprint-panel">
      <div className="flex flex-wrap items-start justify-between gap-3 rounded-lg border p-4">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2 font-medium"><Fingerprint className="size-4" />{t("qualityGuard.fingerprint.title")}<Badge variant="outline">{t("qualityGuard.canary.observeOnly")}</Badge></div>
          <p className="text-xs text-muted-foreground">{t("qualityGuard.fingerprint.description", { count: data.suspectAfter, confidence: pct(data.minConfidence) })}</p>
          <p className="text-xs text-muted-foreground tabular-nums">
            {data.enabled ? `${t("qualityGuard.canary.interval")} ${data.interval} · ${t("qualityGuard.fingerprint.sample", { count: data.sampleAccounts })}` : t("qualityGuard.fingerprint.disabled")} · {data.models.join(", ")} ({data.reasoningEffort}) · {data.familyTolerant ? t("qualityGuard.fingerprint.familyTolerant") : t("qualityGuard.fingerprint.familyStrict")} · {t("qualityGuard.canary.lastRun")} {when(data.lastRun)}{data.enabled ? ` · ${t("qualityGuard.canary.nextRun")} ${when(data.nextRun)}` : ""}
          </p>
          <p className="text-[11px] text-muted-foreground">{t("qualityGuard.fingerprint.attribution")}</p>
        </div>
        <Button type="button" size="sm" onClick={() => run.mutate([])} disabled={busy}><Play />{data.running ? t("qualityGuard.fingerprint.running") : t("qualityGuard.fingerprint.run")}</Button>
      </div>
      {data.accounts.length === 0 ? <EmptyState message={t("qualityGuard.fingerprint.none")} /> : (
        <>
          <div className="hidden overflow-x-auto rounded-lg border md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("qualityGuard.canary.account")}</TableHead>
                  <TableHead>{t("qualityGuard.fingerprint.model")}</TableHead>
                  <TableHead>{t("qualityGuard.fingerprint.verdict")}</TableHead>
                  <TableHead>{t("qualityGuard.fingerprint.candidates")}</TableHead>
                  <TableHead className="text-right">{t("qualityGuard.fingerprint.valid")}</TableHead>
                  <TableHead className="text-right">{t("qualityGuard.canary.duration")}</TableHead>
                  <TableHead>{t("qualityGuard.canary.history")}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.accounts.map((row) => (
                  <Fragment key={keyOf(row)}>
                    <TableRow className="cursor-pointer" onClick={() => toggle(row)}>
                      <TableCell className="text-xs">{expanded === keyOf(row) ? <ChevronDown className="mr-1 inline size-3" /> : <ChevronRight className="mr-1 inline size-3" />}<span className="font-medium">{row.accountName || `#${row.accountId}`}</span><span className="ml-1 text-muted-foreground">#{row.accountId}</span></TableCell>
                      <TableCell className="font-mono text-xs">{row.model}</TableCell>
                      <TableCell><VerdictBadges value={row.latest} /><div className="mt-1 text-[11px] text-muted-foreground">{when(row.latest.createdAt)}</div></TableCell>
                      <TableCell><Candidates value={row.latest} /></TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">{row.latest.validCounts.length ? row.latest.validCounts.join("/") : "-"}</TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">{secs(row.latest.durationMs)}</TableCell>
                      <TableCell><History row={row} /></TableCell>
                      <TableCell className="text-right"><Button type="button" size="sm" variant="ghost" aria-label={t("qualityGuard.fingerprint.rerun")} title={t("qualityGuard.fingerprint.rerun")} disabled={busy} onClick={(event) => { event.stopPropagation(); rerun(row); }}><RotateCw /></Button></TableCell>
                    </TableRow>
                    {expanded === keyOf(row) ? (
                      <TableRow className="hover:bg-transparent"><TableCell colSpan={8} className="bg-muted/20 p-4"><FingerprintDetail id={row.latest.id} /></TableCell></TableRow>
                    ) : null}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="space-y-2 md:hidden">
            {data.accounts.map((row) => (
              <div key={keyOf(row)} className="rounded-lg border p-3 text-xs">
                <div className="flex items-start justify-between gap-2">
                  <button type="button" className="min-w-0 text-left" onClick={() => toggle(row)}>
                    <div className="truncate font-medium">{row.accountName || `#${row.accountId}`} <span className="text-muted-foreground">#{row.accountId}</span></div>
                    <div className="font-mono text-muted-foreground">{row.model} · {when(row.latest.createdAt)}</div>
                  </button>
                  <Button type="button" size="sm" variant="ghost" aria-label={t("qualityGuard.fingerprint.rerun")} disabled={busy} onClick={() => rerun(row)}><RotateCw /></Button>
                </div>
                <div className="mt-2"><VerdictBadges value={row.latest} /></div>
                <div className="mt-2"><Candidates value={row.latest} /></div>
                <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground tabular-nums">
                  <span>{t("qualityGuard.fingerprint.valid")} <span className="font-mono text-foreground">{row.latest.validCounts.join("/") || "-"}</span></span>
                  <span>{t("qualityGuard.canary.duration")} <span className="font-mono text-foreground">{secs(row.latest.durationMs)}</span></span>
                  <History row={row} />
                </div>
                <button type="button" className="mt-2 text-primary underline-offset-2 hover:underline" onClick={() => toggle(row)}>{expanded === keyOf(row) ? t("qualityGuard.fingerprint.hideDetail") : t("qualityGuard.fingerprint.showDetail")}</button>
                {expanded === keyOf(row) ? <div className="mt-2 border-t pt-2"><FingerprintDetail id={row.latest.id} /></div> : null}
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function VerdictBadges({ value }: { value: FingerprintResultDTO }) {
  const { t } = useTranslation();
  const tone = value.status === "pass" ? "text-emerald-600 dark:text-emerald-400" : value.status === "fail" ? "text-destructive" : "text-muted-foreground";
  const label = value.verdict ? t(`qualityGuard.fingerprint.verdicts.${value.verdict}`) : t(`qualityGuard.canary.${value.status}`);
  return (
    <div className="flex flex-wrap items-center gap-1">
      <Badge variant="outline" className={tone}>{label}</Badge>
      {value.suspectSubstituted ? <Badge variant="outline" className="text-destructive">{t("qualityGuard.fingerprint.suspect")}</Badge> : null}
      {value.consecutiveMismatches > 0 ? <span className="text-[11px] text-destructive tabular-nums">×{value.consecutiveMismatches}</span> : null}
      {value.error ? <span className="max-w-56 truncate text-[11px] text-muted-foreground" title={value.error}>{value.error}</span> : null}
    </div>
  );
}

function Candidates({ value }: { value: FingerprintResultDTO }) {
  if (!value.candidates.length) return <span className="text-xs text-muted-foreground">{value.reasons || "-"}</span>;
  return (
    <ol className="space-y-0.5 text-xs">
      {value.candidates.map((item, index) => (
        <li key={item.model} className="flex items-center gap-2 tabular-nums">
          <span className="w-3 text-muted-foreground">{index + 1}</span>
          <span className={cn("font-mono", item.model === value.model ? "text-emerald-600 dark:text-emerald-400" : index === 0 && value.status === "fail" ? "text-destructive" : "")}>{item.model}</span>
          <span className="font-mono text-muted-foreground">{pct(item.probability)}</span>
          <span className="font-mono text-[11px] text-muted-foreground">{item.score.toFixed(3)}</span>
        </li>
      ))}
    </ol>
  );
}

function History({ row }: { row: FingerprintAccountDTO }) {
  return (
    <span className="inline-flex gap-0.5">
      {row.history.slice().reverse().map((item) => (
        <span key={item.id} title={`${item.createdAt} ${item.verdict || item.status} ${item.topModel}`} className={cn("inline-block size-2.5 rounded-sm", item.status === "pass" ? "bg-emerald-500" : item.status === "fail" ? "bg-destructive" : "bg-muted-foreground/40")} />
      ))}
    </span>
  );
}

function FingerprintDetail({ id }: { id: number }) {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: ["quality-fingerprint-result", id], queryFn: () => getFingerprintDetail(id), staleTime: Infinity });
  if (query.isError) return <ErrorState message={query.error.message} onRetry={() => void query.refetch()} />;
  if (!query.data) return <p className="text-xs text-muted-foreground">{t("common.loading")}</p>;
  const value = query.data;
  return (
    <div className="space-y-3 text-xs">
      <p className="flex flex-wrap gap-x-4 gap-y-1">
        <span><span className="text-muted-foreground">{t("qualityGuard.fingerprint.top")}</span> <span className="font-mono">{value.topModel || "-"}</span> ({pct(value.confidence)})</span>
        <span><span className="text-muted-foreground">{t("qualityGuard.fingerprint.tokens")}</span> <span className="font-mono">{value.outputTokens}</span></span>
        <span><span className="text-muted-foreground">{t("qualityGuard.fingerprint.trigger")}</span> <span className="font-mono">{value.trigger}</span></span>
        {value.reasons ? <span>{value.reasons}</span> : null}
      </p>
      {(value.rawOutputs ?? []).map((raw, index) => (
        <div key={index} className="space-y-1">
          <div className="text-muted-foreground">{t("qualityGuard.fingerprint.output", { index: index + 1, count: value.validCounts[index] ?? 0 })}</div>
          {value.prompts?.[index] ? <details><summary className="cursor-pointer text-muted-foreground">{t("qualityGuard.fingerprint.prompt")}</summary><pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 font-mono text-[11px]">{value.prompts[index]}</pre></details> : null}
          <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-2 font-mono text-[11px]">{raw || "-"}</pre>
        </div>
      ))}
    </div>
  );
}
