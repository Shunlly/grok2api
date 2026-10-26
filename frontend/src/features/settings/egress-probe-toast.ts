import type { TFunction } from "i18next";
import { toast } from "sonner";

import { summarizeProbe } from "@/features/settings/egress-probe-summary";
import type { EgressProbeResultDTO } from "@/features/settings/settings-api";

export function showProbeToast(result: EgressProbeResultDTO, t: TFunction) {
  const summary = summarizeProbe(result);
  const family = (value: string) => t(value === "healthy" ? "settings.egress.familyHealthy" : value === "unhealthy" ? "settings.egress.familyUnhealthy" : "settings.egress.familyUnknown");
  if (summary.kind === "error") {
    toast.error(t("settings.egress.testFailed", { error: summary.error || t("settings.egress.operationFailed") }));
  } else if (summary.kind === "warning") {
    toast.warning(t("settings.egress.testedPartialFamily", { ipv4: family(summary.ipv4), ipv6: family(summary.ipv6), exitIp: summary.exitIp || "-" }));
  } else {
    toast.success(summary.exitIp ? t("settings.egress.testedOneDetail", { exitIp: summary.exitIp }) : t("settings.egress.testedOne"));
  }
}
