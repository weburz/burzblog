import { ref } from "vue";
import type { Post } from "../types";
import { errorMessage, request } from "../utils/api";

// "New post" from the button, the "n" key and the command palette: the server
// makes an untitled draft and the editor opens on it with the cursor in the
// title. One request at a time, so a double press does not make two drafts.
const creating = ref(false);

export const useCreatedPost = () => {
  const toast = useToast();
  const create = async () => {
    if (creating.value) return;
    creating.value = true;
    try {
      const post = await request<Post>("/api/posts", "POST", { untitled: true });
      await navigateTo(`/posts/${encodeURIComponent(post.slug)}`);
    } catch (cause) {
      toast.add({
        title: "Could not start a new post",
        description: errorMessage(cause),
        color: "error",
      });
    } finally {
      creating.value = false;
    }
  };
  return { creating, create };
};
