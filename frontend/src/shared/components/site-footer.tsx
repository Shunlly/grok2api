export function SiteFooter() {
  return (
    <footer className="mx-auto flex h-10 w-fit max-w-[calc(100vw-2rem)] lg:fixed lg:right-0 lg:bottom-0 lg:z-20 lg:mx-0 items-center justify-end gap-1.5 whitespace-nowrap px-5 text-right text-[11px] text-muted-foreground sm:px-6">
      <a className="transition-colors hover:text-foreground" href="https://github.com/Shunlly/grok2api" target="_blank" rel="noreferrer">Grok2API</a>
      <span>© 2026</span>
      <span aria-hidden="true">·</span>
      <span>Built by</span>
      <a className="transition-colors hover:text-foreground" href="https://blog.cheny.me/" target="_blank" rel="noreferrer">Chenyme</a>
    </footer>
  );
}
