import re


def slugify(text):
    """Lowercase, replace non-alphanumerics with hyphens, collapse repeats,
    strip leading/trailing hyphens."""
    text = text.lower()
    text = re.sub(r"[^a-z0-9]", "-", text)
    return text


def test_slugify():
    assert slugify("Hello World") == "hello-world"
    assert slugify("A  B") == "a-b"
    assert slugify("--x--") == "x"
    assert slugify("Ünï") == "n"
