def parse_duration(text):
    """See SPEC.md for the rules."""
    raise NotImplementedError


def test_parse_duration():
    assert parse_duration("1h30m") == 5400
    assert parse_duration("45s") == 45
    assert parse_duration("2h") == 7200
    assert parse_duration("90m") == 5400
    assert parse_duration("") == 0
    try:
        parse_duration("abc")
    except ValueError:
        pass
    else:
        raise AssertionError("expected ValueError")
