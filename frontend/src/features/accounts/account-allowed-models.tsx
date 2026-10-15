import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { allowedModelOptions } from "@/features/accounts/allowed-model-options";
import { getAccountModels, type AccountDTO } from "@/features/accounts/accounts-api";

export function AccountAllowedModelsBadge({ account, className }: { account: Pick<AccountDTO, "allowedModels">; className?: string }) {
  const { t } = useTranslation();
  const count = account.allowedModels?.length ?? 0;
  if (count === 0) return null;
  return (
    <span title={account.allowedModels!.join("\n")} className={className ?? "mt-1 block w-fit cursor-help rounded bg-sky-100 px-1.5 text-xs font-medium text-sky-800 dark:bg-sky-950 dark:text-sky-300"}>
      {t("accounts.allowedModels.badge", { count })}
    </span>
  );
}

export function AccountAllowedModelsField({ accountId, value, onChange }: { accountId: string; value: string[]; onChange: (next: string[]) => void }) {
  const { t } = useTranslation();
  const [search, setSearch] = useState("");
  const query = useQuery({ queryKey: ["account-models", accountId], queryFn: () => getAccountModels(accountId) });
  const options = useMemo(() => allowedModelOptions(query.data, value), [query.data, value]);
  const restricted = value.length > 0;
  const [restrictMode, setRestrictMode] = useState(restricted);
  const keyword = search.trim().toLowerCase();
  const visible = keyword ? options.filter((option) => option.id.toLowerCase().includes(keyword) || option.publicIds.some((id) => id.toLowerCase().includes(keyword))) : options;
  const selected = new Set(value);
  const toggle = (id: string, checked: boolean) => {
    const next = new Set(value);
    if (checked) next.add(id);
    else next.delete(id);
    onChange(options.map((option) => option.id).filter((option) => next.has(option)));
  };

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-4 rounded-md bg-muted/50 p-3">
        <div className="space-y-1">
          <Label htmlFor="account-restrict-models">{t("accounts.allowedModels.restrict")}</Label>
          <p className="text-xs text-muted-foreground">{t("accounts.allowedModels.description")}</p>
        </div>
        <Switch
          id="account-restrict-models"
          checked={restrictMode}
          onCheckedChange={(checked) => {
            setRestrictMode(checked);
            onChange(checked ? options.map((option) => option.id) : []);
          }}
        />
      </div>
      {restrictMode ? (
        query.isLoading ? (
          <div className="flex justify-center py-4"><Spinner /></div>
        ) : (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <Input className="h-8 min-w-0 flex-1" placeholder={t("accounts.allowedModels.search")} value={search} onChange={(event) => setSearch(event.target.value)} />
              <Button type="button" variant="outline" size="sm" onClick={() => onChange([...new Set([...value, ...visible.map((option) => option.id)])])}>{t("accounts.allowedModels.selectAll")}</Button>
              <Button type="button" variant="outline" size="sm" onClick={() => { const hidden = new Set(visible.map((option) => option.id)); onChange(value.filter((id) => !hidden.has(id))); }}>{t("accounts.allowedModels.selectNone")}</Button>
            </div>
            <p className="text-xs text-muted-foreground">{t("accounts.allowedModels.selected", { count: value.length, total: options.length })}</p>
            {options.length === 0 ? <p className="text-xs text-muted-foreground">{t("accounts.allowedModels.noOptions")}</p> : null}
            <div className="max-h-64 overflow-y-auto rounded-md border">
              {visible.map((option) => (
                <label key={option.id} className="flex cursor-pointer items-start gap-2 border-b px-3 py-2 text-sm last:border-b-0 hover:bg-muted/40">
                  <Checkbox className="mt-0.5" checked={selected.has(option.id)} onCheckedChange={(checked) => toggle(option.id, checked === true)} />
                  <span className="min-w-0 flex-1">
                    <span className="block break-all font-mono text-xs">{option.id}</span>
                    {option.publicIds.length ? (
                      <span className="mt-1 flex flex-wrap gap-1">
                        {option.publicIds.map((id) => <Badge key={id} variant="secondary" className="font-mono text-[10px]">{id}</Badge>)}
                      </span>
                    ) : null}
                  </span>
                </label>
              ))}
            </div>
            {value.length === 0 ? <p className="text-xs text-destructive">{t("accounts.allowedModels.emptyError")}</p> : null}
          </div>
        )
      ) : null}
    </div>
  );
}
