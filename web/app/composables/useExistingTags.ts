import { ref } from "vue";
import type { TagCount } from "../types";
import { request } from "../utils/api";

// Every tag in use, for suggestions in the tags field. The editor loads it when
// it opens and again after each save, so a tag added to one post is offered on
// the next. A failed load leaves the last list: suggestions are optional.
const existing = ref<TagCount[]>([]);

export const useExistingTags = () => {
  const refresh = async () => {
    try {
      existing.value = (await request<{ items: TagCount[] }>("/api/tags")).items;
    } catch {
      /* the field still takes new tags */
    }
  };
  return { existing, refresh };
};
