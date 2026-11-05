import { onScopeDispose, ref } from "vue";
import type { MediaItem, MediaKind, MediaList } from "../types";
import { ApiError, errorMessage, request } from "../utils/api";

const pageSize = 48;
// How long typing in the search box waits before asking the server.
const searchDelay = 250;

// The uploads library: a page at a time, newest first, filtered by the search
// text and kind, plus upload, alt text edits and delete.
// Each call gets its own state, so the Media page and the picker do not share
// a half-loaded list.
export const useUploadedMedia = () => {
  const items = ref<MediaItem[]>([]);
  const total = ref(0);
  const loading = ref(false);
  const uploading = ref(false);
  const error = ref("");
  // One entry per file the server refused, so a batch reports every failure.
  const failures = ref<string[]>([]);
  const query = ref("");
  const kind = ref<MediaKind>("");
  // Only the newest request may fill the list, so a slow answer to an earlier
  // search cannot replace a later one.
  let latest = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  onScopeDispose(() => clearTimeout(timer));

  const load = async (more = false) => {
    const mine = ++latest;
    loading.value = true;
    error.value = "";
    try {
      const offset = more ? items.value.length : 0;
      const params = new URLSearchParams({
        limit: String(pageSize),
        offset: String(offset),
      });
      if (query.value.trim()) params.set("q", query.value.trim());
      if (kind.value) params.set("type", kind.value);
      const page = await request<MediaList>(`/api/media?${params}`);
      if (mine !== latest) return;
      items.value = more ? [...items.value, ...page.items] : page.items;
      total.value = page.total;
    } catch (cause) {
      if (mine === latest) error.value = errorMessage(cause);
    } finally {
      if (mine === latest) loading.value = false;
    }
  };

  // Reloads the first page for the current search text and kind. Typing waits
  // a moment (delay), so a fast typist sends one request, not one per key.
  const refine = (delay = 0) => {
    clearTimeout(timer);
    if (delay) timer = setTimeout(() => void load(), delay);
    else void load();
  };
  const typeAhead = () => refine(searchDelay);

  // Uploads files one at a time and returns the records, in order. A repeated
  // upload comes back as the existing record and is not listed twice.
  const upload = async (files: File[]): Promise<MediaItem[]> => {
    uploading.value = true;
    failures.value = [];
    const done: MediaItem[] = [];
    for (const file of files) {
      const form = new FormData();
      form.append("file", file, file.name);
      try {
        const item = await request<MediaItem>("/api/media", "POST", form);
        done.push(item);
        if (!items.value.some((existing) => existing.id === item.id)) {
          items.value = [item, ...items.value];
          total.value += 1;
        }
      } catch (cause) {
        failures.value.push(`${file.name}: ${errorMessage(cause)}`);
      }
    }
    uploading.value = false;
    return done;
  };

  // Saves an upload's alt text. message is empty on success.
  const saveAlt = async (item: MediaItem, alt: string): Promise<string> => {
    try {
      const saved = await request<MediaItem>(`/api/media/${item.id}`, "PATCH", { alt });
      items.value = items.value.map((existing) =>
        existing.id === saved.id ? saved : existing,
      );
      return "";
    } catch (cause) {
      return errorMessage(cause);
    }
  };

  // Deletes an upload. Without force the server refuses while posts use it
  // (inUse), and its message names them. message is empty on success.
  const remove = async (
    item: MediaItem,
    force = false,
  ): Promise<{ message: string; inUse: boolean }> => {
    try {
      await request(`/api/media/${item.id}${force ? "?force=true" : ""}`, "DELETE");
      items.value = items.value.filter((existing) => existing.id !== item.id);
      total.value = Math.max(0, total.value - 1);
      return { message: "", inUse: false };
    } catch (cause) {
      return {
        message: errorMessage(cause),
        inUse: cause instanceof ApiError && cause.code === "in_use",
      };
    }
  };

  return {
    items,
    total,
    loading,
    uploading,
    error,
    failures,
    query,
    kind,
    load,
    refine,
    typeAhead,
    upload,
    saveAlt,
    remove,
  };
};
