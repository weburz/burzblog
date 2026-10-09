import { describe, expect, it } from "vitest";
import { defaultList, parseRoute, routeSearch, sameRoute } from "../../app/utils/route";

describe("routes in the query string", () => {
  it("round-trips every view", () => {
    for (const search of [
      "",
      "?view=repository",
      "?post=posts%2Fa+b.md",
      "?status=draft&q=garden&sort=path&page=3",
    ])
      expect(routeSearch(parseRoute(search))).toBe(search);
  });
  it("defaults the list and drops unknown or unsafe values", () => {
    expect(parseRoute("")).toEqual({ name: "posts", list: defaultList });
    expect(parseRoute("?status=archived&sort=size&page=-4&view=other")).toEqual({
      name: "posts",
      list: defaultList,
    });
    expect(parseRoute(`?q=${"x".repeat(500)}`)).toMatchObject({
      list: { q: "x".repeat(200) },
    });
  });
  it("opens a post by path and prefers it over other parameters", () => {
    expect(parseRoute("?post=notes%2Fx.md&view=repository")).toEqual({
      name: "post",
      path: "notes/x.md",
    });
  });
  it("compares routes by what the address shows", () => {
    expect(
      sameRoute(
        { name: "posts", list: { ...defaultList, q: " garden " } },
        { name: "posts", list: { ...defaultList, q: "garden" } },
      ),
    ).toBe(true);
    expect(sameRoute({ name: "repository" }, { name: "post", path: "a.md" })).toBe(false);
  });
});
