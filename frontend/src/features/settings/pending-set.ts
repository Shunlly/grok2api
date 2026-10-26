// withPending returns a new set with id added or removed, reusing the current
// set when nothing changes so React skips the re-render.
export function withPending(current: ReadonlySet<string>, id: string, pending: boolean): ReadonlySet<string> {
  if (current.has(id) === pending) return current;
  const next = new Set(current);
  if (pending) next.add(id);
  else next.delete(id);
  return next;
}
