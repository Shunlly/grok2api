type ModelItem = { id: string; upstreamModel: string; reason?: string };
/** Structural subset of AccountModelsDTO (kept local so node tests do not load the API module). */
export type AllowedModelSource = { all: Array<{ id: string; publicIds: string[] }>; usable: ModelItem[]; unavailable: ModelItem[] };

export type AllowedModelOption = { id: string; publicIds: string[] };

/** Upstream models selectable for an account: the upstream capability list plus any routed upstream models. */
export function allowedModelOptions(data: AllowedModelSource | undefined, selected: string[]): AllowedModelOption[] {
  const byId = new Map<string, Set<string>>();
  const add = (id: string, publicId?: string) => {
    if (!id) return;
    const set = byId.get(id) ?? new Set<string>();
    if (publicId) set.add(publicId);
    byId.set(id, set);
  };
  for (const item of data?.all ?? []) {
    add(item.id);
    for (const publicId of item.publicIds) add(item.id, publicId);
  }
  for (const item of [...(data?.usable ?? []), ...(data?.unavailable ?? [])]) {
    if (item.reason === "routeDisabled") continue;
    add(item.upstreamModel, item.id);
  }
  for (const id of selected) add(id);
  return [...byId.entries()].map(([id, ids]) => ({ id, publicIds: [...ids].sort() })).sort((a, b) => a.id.localeCompare(b.id));
}
