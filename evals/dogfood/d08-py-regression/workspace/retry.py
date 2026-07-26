def retry_with_backoff(fn, max_attempts=3):
    """Call fn until it stops raising, at most max_attempts times.

    BUG: an off-by-one — it performs max_attempts+1 calls, so a caller asking
    for 1 attempt actually gets a retry it never requested.
    """
    attempt = 0
    last = None
    while attempt <= max_attempts:
        try:
            return fn()
        except Exception as exc:
            last = exc
            attempt += 1
    raise last


def test_retries_then_succeeds():
    calls = {"n": 0}

    def flaky():
        calls["n"] += 1
        if calls["n"] < 3:
            raise RuntimeError("nope")
        return "ok"

    assert retry_with_backoff(flaky) == "ok"
    assert calls["n"] == 3


def test_attempt_count_is_exact():
    calls = {"n": 0}

    def always_fails():
        calls["n"] += 1
        raise RuntimeError("nope")

    try:
        retry_with_backoff(always_fails, max_attempts=2)
    except RuntimeError:
        pass
    assert calls["n"] == 2, f"max_attempts=2 must call fn exactly twice, got {calls['n']}"
