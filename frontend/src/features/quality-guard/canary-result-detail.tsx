import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { getCanaryResultDetail } from "@/features/quality-guard/quality-guard-api";
import { buildSandboxedDocument, extractRenderableMarkup } from "@/features/quality-guard/canary-artifact";
import { ErrorState } from "@/shared/components/data-state";

export function CanaryResultDetail({ resultId }: { resultId: number }) {
  const { t } = useTranslation();
  const [raw, setRaw] = useState(false);
  const query = useQuery({ queryKey: ["quality-canary-result", resultId], queryFn: () => getCanaryResultDetail(resultId), staleTime: Infinity });
  if (query.isError) return <ErrorState message={query.error.message} onRetry={() => void query.refetch()} />;
  if (!query.data) return <p className="text-xs text-muted-foreground">{t("common.loading")}</p>;
  const { result, artifact } = query.data;
  const verdict = [result.reasons, result.error].filter(Boolean).join(" · ") || (result.status === "pass" ? t("qualityGuard.canary.detail.passed") : "-");
  if (!artifact) {
    return (
      <div className="space-y-1 text-xs">
        <p><span className="text-muted-foreground">{t("qualityGuard.canary.detail.verdict")}:</span> {verdict}</p>
        <p className="text-muted-foreground">{t("qualityGuard.canary.detail.noArtifact")}</p>
      </div>
    );
  }
  const markup = extractRenderableMarkup(artifact.response);
  const stat = (label: string, value: string | number) => <span className="whitespace-nowrap"><span className="text-muted-foreground">{label}</span> <span className="font-mono tabular-nums">{value}</span></span>;
  return (
    <div className="space-y-3 text-xs">
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {stat(t("qualityGuard.canary.detail.model"), `${artifact.responseModel || result.model} (${artifact.reasoningEffort}, ≤${artifact.maxOutputTokens})`)}
        {stat(t("qualityGuard.canary.detail.input"), artifact.inputTokens)}
        {stat(t("qualityGuard.canary.detail.output"), artifact.outputTokens)}
        {stat(t("qualityGuard.canary.detail.reasoningTokens"), artifact.reasoningTokens)}
        {stat(t("qualityGuard.canary.detail.total"), artifact.totalTokens)}
        {stat(t("qualityGuard.canary.detail.firstToken"), artifact.firstTokenMs ? `${(artifact.firstTokenMs / 1000).toFixed(1)}s` : "-")}
        {stat(t("qualityGuard.canary.detail.duration"), artifact.durationMs ? `${(artifact.durationMs / 1000).toFixed(1)}s` : "-")}
      </div>
      <div className="space-y-1">
        <p><span className="text-muted-foreground">{t("qualityGuard.canary.detail.verdict")}:</span> {verdict}</p>
        {artifact.checkDetail ? <p><span className="text-muted-foreground">{t("qualityGuard.canary.detail.check")}:</span> {artifact.checkDetail}</p> : null}
      </div>
      <details>
        <summary className="cursor-pointer text-muted-foreground">{t("qualityGuard.canary.detail.prompt")}</summary>
        <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-muted/50 p-2">{artifact.prompt}</pre>
      </details>
      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">{t("qualityGuard.canary.detail.answer")}{artifact.responseTruncated ? ` (${t("qualityGuard.canary.detail.truncated")})` : ""}</span>
          {markup ? <Button type="button" size="sm" variant="ghost" onClick={() => setRaw((value) => !value)}>{raw ? t("qualityGuard.canary.detail.showRender") : t("qualityGuard.canary.detail.showRaw")}</Button> : null}
        </div>
        {markup && !raw ? (
          <iframe title={`canary-${resultId}`} sandbox="" referrerPolicy="no-referrer" srcDoc={buildSandboxedDocument(markup)} className="h-72 w-full max-w-md rounded border bg-white" />
        ) : (
          <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-muted/50 p-2">{artifact.response || "-"}</pre>
        )}
      </div>
      {artifact.reasoning ? (
        <details>
          <summary className="cursor-pointer text-muted-foreground">{t("qualityGuard.canary.detail.reasoning", { count: artifact.reasoningChars })}{artifact.reasoningTruncated ? ` (${t("qualityGuard.canary.detail.truncated")})` : ""}</summary>
          <pre className="mt-1 max-h-96 overflow-auto whitespace-pre-wrap rounded bg-muted/50 p-2">{artifact.reasoning}</pre>
        </details>
      ) : <p className="text-muted-foreground">{t("qualityGuard.canary.detail.noReasoning")}</p>}
    </div>
  );
}
