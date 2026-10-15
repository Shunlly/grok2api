const RELOAD_KEY = "grok2api:chunk-reload-at";
const RELOAD_GUARD_MS = 30_000;

/** isChunkLoadError 判断错误是否为部署后旧页面加载已失效的动态模块。 */
export function isChunkLoadError(error: unknown): boolean {
  const message = error instanceof Error ? `${error.name} ${error.message}` : typeof error === "string" ? error : "";
  return /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|ChunkLoadError|Loading chunk [\w-]+ failed|Unable to preload CSS/i.test(message);
}

/** reloadOnceForChunkError 在保护时间内只自动刷新一次，避免文件真缺失时无限刷新。 */
export function reloadOnceForChunkError(): boolean {
  try {
    const last = Number(window.sessionStorage.getItem(RELOAD_KEY) ?? "0");
    if (Number.isFinite(last) && Date.now() - last < RELOAD_GUARD_MS) {
      return false;
    }
    window.sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
  } catch {
    return false;
  }
  window.location.reload();
  return true;
}

export function installChunkReloadHandler() {
  window.addEventListener("vite:preloadError", (event) => {
    if (reloadOnceForChunkError()) {
      event.preventDefault();
    }
  });
}
