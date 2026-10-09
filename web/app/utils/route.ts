// The view lives in the query string: the production shell is served only at "/".
export type ListFilter = "all" | "draft" | "published";
export type ListSort = "modified" | "path";
export interface ListState {
  filter: ListFilter;
  q: string;
  sort: ListSort;
  offset: number;
}
export type Route =
  | { name: "posts"; list: ListState }
  | { name: "repository" }
  | { name: "post"; path: string };

export const pageSize = 100;
export const defaultList: ListState = {
  filter: "all",
  q: "",
  sort: "modified",
  offset: 0,
};

export const parseRoute = (search: string): Route => {
  const query = new URLSearchParams(search);
  const post = query.get("post");
  if (post) return { name: "post", path: post };
  if (query.get("view") === "repository") return { name: "repository" };
  const status = query.get("status");
  const page = Number.parseInt(query.get("page") ?? "1", 10);
  return {
    name: "posts",
    list: {
      filter: status === "draft" || status === "published" ? status : "all",
      q: (query.get("q") ?? "").slice(0, 200),
      sort: query.get("sort") === "path" ? "path" : "modified",
      offset: Number.isFinite(page) && page > 1 ? (page - 1) * pageSize : 0,
    },
  };
};

export const routeSearch = (route: Route): string => {
  const query = new URLSearchParams();
  if (route.name === "post") query.set("post", route.path);
  else if (route.name === "repository") query.set("view", "repository");
  else {
    const { filter, q, sort, offset } = route.list;
    if (filter !== "all") query.set("status", filter);
    if (q.trim()) query.set("q", q.trim());
    if (sort !== "modified") query.set("sort", sort);
    if (offset > 0) query.set("page", String(Math.floor(offset / pageSize) + 1));
  }
  const text = query.toString();
  return text ? `?${text}` : "";
};

export const sameRoute = (left: Route, right: Route): boolean =>
  routeSearch(left) === routeSearch(right);
