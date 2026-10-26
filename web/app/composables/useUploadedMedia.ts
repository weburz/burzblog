import { ref } from "vue";
import type { MediaItem, MediaList } from "../types";
import { ApiError, errorMessage, request } from "../utils/api";

const pageSize = 48;

// The uploads library: a page at a time, newest first, plus upload and delete.
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

  const load = async (more = false) => {
    loading.value = true;
    error.value = "";
    try {
      const offset = more ? items.value.length : 0;
      const page = await request<MediaList>(
        `/api/media?limit=${pageSize}&offset=${offset}`,
      );
      items.value = more ? [...items.value, ...page.items] : page.items;
      total.value = page.total;
    } catch (cause) {
      error.value = errorMessage(cause);
    } finally {
      loading.value = false;
    }
  };

  // Uploads files one at a time and returns the records, in order. A repeated
  // upload comes back as the existing record and is not listed twice.
  const upload = async (files: File[], alt = ""): Promise<MediaItem[]> => {
    uploading.value = true;
    failures.value = [];
    const done: MediaItem[] = [];
    for (const file of files) {
      const form = new FormData();
      form.append("file", file, file.name);
      if (alt) form.append("alt", alt);
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

  return { items, total, loading, uploading, error, failures, load, upload, remove };
};
