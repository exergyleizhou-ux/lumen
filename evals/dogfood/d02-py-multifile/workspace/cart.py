from pricing import round_money


def apply_discount(price, percent_off, coupon_off):
    """Apply the percentage first, then the flat coupon.
    BUG: applies the coupon before the percentage."""
    price = price - coupon_off
    price = price * (1 - percent_off / 100)
    return round_money(price)


def test_apply_discount():
    assert apply_discount(100.0, 10, 5) == 85.0
    assert apply_discount(20.0, 50, 0) == 10.0
    assert apply_discount(2.675, 0, 0) == 2.68
