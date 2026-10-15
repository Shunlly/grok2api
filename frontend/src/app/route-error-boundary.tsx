import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useRouteError } from "react-router-dom";

import { isChunkLoadError, reloadOnceForChunkError } from "@/app/chunk-reload";
import { Button } from "@/components/ui/button";

export function RouteErrorBoundary() {
  const { t } = useTranslation();
  const error = useRouteError();
  const chunkError = isChunkLoadError(error);
  useEffect(() => {
    if (chunkError) {
      reloadOnceForChunkError();
    }
  }, [chunkError]);
  return (
    <div className="flex min-h-[60vh] items-center justify-center p-6">
      <div className="max-w-md space-y-3 text-center">
        <h1 className="text-lg font-semibold">{t(chunkError ? "chunkError.title" : "chunkError.genericTitle")}</h1>
        <p className="text-sm text-muted-foreground">{t(chunkError ? "chunkError.description" : "chunkError.genericDescription")}</p>
        <Button onClick={() => window.location.reload()}>{t("chunkError.reload")}</Button>
      </div>
    </div>
  );
}
