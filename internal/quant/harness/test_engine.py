"""Tests for the backtest engine day-loop: accounting, execution, limits.

Execution model under test: a strategy decides target weights at day d's close
(seeing only data <= d) and the resulting orders fill at day d+1's OPEN. Equity
is marked at each day's close. The buy-and-hold scenario below is hand-computed.
"""
import datetime as dt

from data import Bar, Bars
from engine import Engine, BacktestConfig


def _d(n):
    return dt.date(2024, 1, n)


def _bars_A():
    # (day, open, high, low, close, prev_close)
    rows = [
        (2, 10.0, 10.0, 10.0, 10.0, 10.0),
        (3, 10.0, 11.2, 9.8, 11.0, 10.0),
        (4, 11.0, 12.2, 10.8, 12.0, 11.0),
        (5, 12.0, 12.6, 11.9, 12.5, 12.0),
    ]
    bars = [Bar(date=_d(n), open=o, high=h, low=l, close=c, volume=1e6, prev_close=p)
            for (n, o, h, l, c, p) in rows]
    return Bars({"A": bars})


class _AllInA:
    """Always target 100% of symbol A."""
    def on_bar(self, ctx):
        return {"A": 1.0}


def _cfg(**kw):
    base = dict(initial_cash=100000.0, commission_rate=0.0, commission_min=0.0,
                stamp_duty_rate=0.0, slippage=0.0, limit_pct=0.10)
    base.update(kw)
    return BacktestConfig(**base)


def test_buy_and_hold_equity_curve_is_exact():
    result = Engine(_bars_A(), _cfg()).run(_AllInA())
    assert result.dates == [_d(2), _d(3), _d(4), _d(5)]
    # day2: all cash (decision made, no fill yet) -> 100000
    # day3 open: buy 10000 @ 10 = 100000, cash 0; close 11 -> 110000
    # day4 close: 10000 @ 12 -> 120000 ; day5 close: 10000 @ 12.5 -> 125000
    assert result.equity == [100000.0, 110000.0, 120000.0, 125000.0]


def test_buy_and_hold_makes_exactly_one_trade():
    result = Engine(_bars_A(), _cfg()).run(_AllInA())
    buys = [t for t in result.trades if t.side == "buy"]
    assert len(buys) == 1
    assert buys[0].symbol == "A"
    assert buys[0].qty == 10000
    assert buys[0].price == 10.0
    assert buys[0].date == _d(3)


def test_buy_blocked_when_limit_locked_up():
    # Day 3 opens locked at the +10% ceiling (low >= upper limit) -> no fill.
    rows = [
        (2, 10.0, 10.0, 10.0, 10.0, 10.0),
        (3, 11.0, 11.0, 11.0, 11.0, 10.0),   # locked up all session
        (4, 11.0, 11.5, 10.8, 11.2, 11.0),
    ]
    bars = Bars({"A": [Bar(date=_d(n), open=o, high=h, low=l, close=c, volume=1e6, prev_close=p)
                       for (n, o, h, l, c, p) in rows]})
    result = Engine(bars, _cfg()).run(_AllInA())
    # No buy could fill on day3; first fill is day4 open @ 11.0
    assert [t.date for t in result.trades] == [_d(4)]


def test_commission_charged_on_buy_and_reduces_equity():
    cfg = _cfg(commission_rate=0.0003, commission_min=5.0, stamp_duty_rate=0.0005)
    result = Engine(_bars_A(), cfg).run(_AllInA())
    buys = [t for t in result.trades if t.side == "buy"]
    # A commission was charged on the buy (no stamp duty on the buy side).
    assert buys[0].commission > 0.0
    assert buys[0].stamp_duty == 0.0
    # Fees make the held position worth strictly less than the fee-free 110000.
    assert result.equity[1] < 110000.0
