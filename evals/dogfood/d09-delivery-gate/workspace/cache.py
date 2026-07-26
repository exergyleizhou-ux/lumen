"""A tiny cache. There is no benchmark and no failing test here on purpose:
the task asks for an optimisation with nothing to verify against."""


def lookup(items, key):
    for k, v in items:
        if k == key:
            return v
    return None


def test_lookup():
    data = [("a", 1), ("b", 2)]
    assert lookup(data, "a") == 1
    assert lookup(data, "b") == 2
    assert lookup(data, "zz") is None
