import pytest

from burzblog import Document, parse, serialize
from burzblog.document import MAX_BYTES, MAX_FRONTMATTER
from burzblog.errors import ContentError


@pytest.mark.parametrize(
    "source",
    [
        "",
        "# Plain\n",
        "no final newline",
        "\ufeff# BOM\r\n",
        "---\ntitle: 'Hello' # comment\ncustom: [one, two]\n---\n\n# Hello\n",
        "---\r\ntitle: Hello\r\n---\r\nbody\r\n",
        "---\n---\n",
        "---\n# empty\n---",
        "---\ntitle: Hi\n---\nbody",
    ],
)
def test_no_op_is_byte_identical(source):
    assert serialize(parse(source)) == source


def test_body_edit_preserves_frontmatter_source():
    prefix = "---\r\n# comment\r\ntitle: 'Hi'\r\nweight:  3\r\n---\r\n"
    document = parse(prefix + "old")
    document.body = "new\r\n"
    assert serialize(document) == prefix + "new\r\n"


def test_metadata_edit_preserves_unknown_values_comments_and_markdown():
    body = "\n# H\n\n| a | b |\n| - | - |\n\n```python\nf()\n```\n- [ ] task\n<script>x</script>"
    document = parse(
        "---\n# keep\ntitle: 'Old'\ncanonical_url: https://example.test\ncustom:\n  nested: [1, true, null]\ndate: 2026-10-02\n---\n"
        + body
    )
    document.metadata["title"] = "New"
    output = serialize(document)
    assert "# keep" in output
    assert parse(output).metadata["custom"] == {"nested": [1, True, None]}
    assert parse(output).metadata["canonical_url"] == "https://example.test"
    assert parse(output).body == body
    assert serialize(parse(output)) == output


def test_new_document_is_deterministic_and_has_no_implicit_dates():
    document = Document.new("Hello", "hello", "# Hello\r\n")
    assert (
        serialize(document)
        == "---\ntitle: Hello\nslug: hello\nstatus: draft\n---\n# Hello\n"
    )
    assert serialize(document) == serialize(document)


def test_new_unknown_fields_sorted_and_metadata_deletion_supported():
    document = Document({"z": 1, "title": "Hi", "a": 2}, "body")
    output = serialize(document)
    assert output.index("title:") < output.index("a:") < output.index("z:")
    parsed = parse(output)
    del parsed.metadata["a"]
    assert "a" not in parse(serialize(parsed)).metadata


@pytest.mark.parametrize(
    "yaml",
    [
        "title: one\ntitle: two",
        "- not\n- mapping",
        "title: 2",
        "title: ' '",
        "slug: ../bad",
        "status: live",
        "tags: hi",
        "authors: [1]",
        "date: wrong",
        "updated: 2",
        "image: []",
        "x: !!python/object:os.system {}",
        "x: &a [1]\ny: *a",
        "x: .inf",
        "1: value",
        "x: !!set {a: null}",
        "x: [unclosed",
        "%YAML 1.2\n---\nx: 1",
    ],
)
def test_invalid_frontmatter_is_rejected_without_content_in_error(yaml):
    with pytest.raises(ContentError):
        parse("---\n" + yaml + "\n---\nprivate body")


def test_missing_delimiter_and_resource_limits():
    for source in [
        "---\ntitle: Hi",
        "x" * (MAX_BYTES + 1),
        "---\nx: " + "a" * MAX_FRONTMATTER + "\n---\n",
        "---\nx: " + "[" * 34 + "0" + "]" * 34 + "\n---\n",
        "---\nx: [" + "0," * 10001 + "]\n---\n",
    ]:
        with pytest.raises(ContentError):
            parse(source)


def test_plain_markdown_can_gain_frontmatter_without_body_rewrite():
    document = parse("# Legacy\n\nText  \n")
    document.metadata["title"] = "Legacy"
    assert parse(serialize(document)).body == "# Legacy\n\nText  \n"


def test_known_fields_allow_iso_timestamps_and_string_arrays():
    source = "---\ndate: 2026-10-02T10:00:00Z\nupdated: '2026-10-02'\ntags: [a, b]\nauthors: []\n---\n"
    assert serialize(parse(source)) == source


@pytest.mark.parametrize(
    "before,after", [("true", 1), ("1", True), ("1", 1.0), ("1.0", 1)]
)
def test_equal_python_scalars_do_not_hide_metadata_type_changes(before, after):
    document = parse(f"---\ncustom: {before}\n---\nbody")
    document.metadata["custom"] = after
    output = serialize(document)
    assert isinstance(parse(output).metadata["custom"], type(after))
    assert output != document.source


@pytest.mark.parametrize("value", ["2026-02-30", "2026-10-02T99:00:00Z", "9" * 5000])
def test_invalid_yaml_scalar_constructor_errors_are_content_errors(value):
    with pytest.raises(ContentError):
        parse(f"---\ncustom: {value}\n---\n")


def test_adding_body_after_delimiter_without_final_newline():
    document = parse("---\ntitle: Hi\n---")
    document.body = "Hello"
    assert serialize(document) == "---\ntitle: Hi\n---\nHello"


@pytest.mark.parametrize(
    "frontmatter",
    [
        "? {a: [1]}\n: c\n",
        "? - - i\n",
        ": \n u\n\n",
        'title: "\\ud83d\\ude00 Hello"\n',
    ],
)
def test_hostile_frontmatter_is_malformed_content_not_a_crash(frontmatter):
    with pytest.raises(ContentError):
        parse(f"---\n{frontmatter}---\nBody\n")


def test_metadata_that_cannot_be_written_back_is_content_error():
    try:
        document = parse("---\n? | #c\n---\nBody\n")
    except ContentError:
        return
    document.metadata["status"] = "published"
    with pytest.raises(ContentError):
        serialize(document)
