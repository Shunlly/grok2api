import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { getAccountModels, refreshAccountModels, type AccountModelItemDTO, type AccountModelsDTO } from "@/features/accounts/accounts-api";
import { formatDateTime } from "@/shared/lib/format";

type Target = { id: string; name: string };

function formatWindow(value?: number) {
  if (!value) return "-";
  return value >= 1000 ? `${Math.round(value / 1000)}k` : String(value);
}

export function AccountModelsDialog({ target, onClose }: { target: Target | null; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const queryKey = ["account-models", target?.id];
  const query = useQuery({ queryKey, queryFn: () => getAccountModels(target!.id), enabled: target !== null });
  const refresh = useMutation({
    mutationFn: () => refreshAccountModels(target!.id),
    onSuccess: (data: AccountModelsDTO) => {
      queryClient.setQueryData(queryKey, data);
      if (data.modelSyncFailed) toast.warning(t("accountModels.refreshFailed"));
      else toast.success(t("accountModels.refreshed"));
    },
    onError: (error: Error) => toast.error(error.message),
  });
  const data = query.data;
  const reasonText = (reason?: string) => (reason ? t(`accountModels.reasons.${reason}`, { defaultValue: reason }) : "");

  const renderItems = (items: AccountModelItemDTO[], withReason: boolean) => items.length === 0 ? (
    <p className="py-6 text-center text-sm text-muted-foreground">{t("accountModels.empty")}</p>
  ) : (
    <div className="max-h-[50vh] overflow-auto rounded-md border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
          <tr>
            <th className="px-3 py-2">{t("accountModels.publicId")}</th>
            <th className="px-3 py-2">{t("accountModels.upstreamModel")}</th>
            <th className="px-3 py-2">{t("accountModels.capability")}</th>
            <th className="px-3 py-2">{t("accountModels.contextWindow")}</th>
            {withReason ? <th className="px-3 py-2">{t("accountModels.reason")}</th> : null}
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={`${item.id}-${item.upstreamModel}`} className="border-t">
              <td className="px-3 py-1.5 font-mono text-xs">{item.id}</td>
              <td className="px-3 py-1.5 font-mono text-xs text-muted-foreground">{item.upstreamModel}</td>
              <td className="px-3 py-1.5">{item.capability ?? "-"}</td>
              <td className="px-3 py-1.5">{formatWindow(item.contextWindow)}</td>
              {withReason ? (
                <td className="px-3 py-1.5" title={item.detail}>
                  {reasonText(item.reason)}
                  {item.until ? <span className="ml-1 text-xs text-muted-foreground">{t("accountModels.until", { time: formatDateTime(item.until, i18n.language) })}</span> : null}
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );

  return (
    <Dialog open={target !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("accountModels.title", { name: target?.name ?? "" })}</DialogTitle>
          <DialogDescription>{t("accountModels.description")}</DialogDescription>
        </DialogHeader>
        {query.isLoading || !data ? (
          <div className="flex justify-center py-10">{query.error ? <p className="text-sm text-destructive">{(query.error as Error).message}</p> : <Spinner />}</div>
        ) : (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>{data.fetchedAt ? t("accountModels.fetchedAt", { time: formatDateTime(data.fetchedAt, i18n.language) }) : t("accountModels.neverSynced")}</span>
              <Button size="sm" variant="outline" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
                <RefreshCw className={refresh.isPending ? "animate-spin" : undefined} />{t("accountModels.refresh")}
              </Button>
            </div>
            {data.lastError ? <p className="text-xs text-destructive">{t("accountModels.lastError", { error: data.lastError })}</p> : null}
            {data.accountBlockers.length > 0 ? (
              <p className="rounded-md bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
                {t("accountModels.accountBlocked", { reasons: data.accountBlockers.map(reasonText).join("、") })}
              </p>
            ) : null}
            <Tabs defaultValue="usable">
              <TabsList>
                <TabsTrigger value="usable">{t("accountModels.usableTab", { count: data.usable.length })}</TabsTrigger>
                <TabsTrigger value="unavailable">{t("accountModels.unavailableTab", { count: data.unavailable.length })}</TabsTrigger>
                <TabsTrigger value="all">{t("accountModels.allTab", { count: data.all.length })}</TabsTrigger>
              </TabsList>
              <TabsContent value="usable">{renderItems(data.usable, false)}</TabsContent>
              <TabsContent value="unavailable">{renderItems(data.unavailable, true)}</TabsContent>
              <TabsContent value="all">
                {data.all.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">{t("accountModels.empty")}</p> : (
                  <div className="max-h-[50vh] overflow-auto rounded-md border">
                    <table className="w-full text-sm">
                      <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
                        <tr>
                          <th className="px-3 py-2">{t("accountModels.upstreamModel")}</th>
                          <th className="px-3 py-2">{t("accountModels.contextWindow")}</th>
                          <th className="px-3 py-2">{t("accountModels.mappedTo")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {data.all.map((item) => (
                          <tr key={item.id} className="border-t">
                            <td className="px-3 py-1.5 font-mono text-xs">{item.id}</td>
                            <td className="px-3 py-1.5">{formatWindow(item.contextWindow)}</td>
                            <td className="px-3 py-1.5">
                              {item.publicIds.length === 0 ? <span className="text-xs text-muted-foreground">{t("accountModels.unmapped")}</span> : (
                                <div className="flex flex-wrap gap-1">{item.publicIds.map((id) => <Badge key={id} variant="secondary" className="font-mono text-[11px]">{id}</Badge>)}</div>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </TabsContent>
            </Tabs>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
