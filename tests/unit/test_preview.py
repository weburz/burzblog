import random

from markdown_it import MarkdownIt

from burzblog.render import _renderer, render_html


def test_linear_fragment_join_renders_exactly_like_markdown_it():
    original = MarkdownIt("commonmark", {"html": False}).enable("table")
    patched = _renderer()
    pieces = ["_", "*", "a", " ", "\n", "~", "[", "]", "(", ")", "`", "**", "__"]
    pieces += ["x_y", "\n\n", "# ", "> ", "- ", "|", "\\", "&amp;", "!", "<"]
    generator = random.Random(7)  # noqa: S311 - reproducible test input
    for _ in range(3000):
        source = "".join(
            generator.choice(pieces) for _ in range(generator.randint(0, 60))
        )
        assert patched.render(source) == original.render(source)


def test_many_emphasis_delimiters_stay_small():
    import resource

    before = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    assert render_html("_a" * 40000).startswith("<p>")
    grown_kib = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss - before
    assert grown_kib < 400 * 1024
