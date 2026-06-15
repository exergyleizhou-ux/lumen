package orphan

import ("testing";"time")

func TestDetectorScan(t *testing.T) {
	d := NewDetector()
	d.RegisterResource(&Resource{ID: "r1", Type: "session", CreatedAt: time.Now().Add(-2 * time.Hour), LastUsed: time.Now().Add(-2 * time.Hour)})
	d.AddPolicy(Policy{Name: "stale", Type: "session", MaxIdle: 1 * time.Hour, MaxAge: 3 * time.Hour})
	orphans := d.Scan()
	if len(orphans) != 1 { t.Error("should find orphan") }
}
func TestDetectorNoOrphans(t *testing.T) {
	d := NewDetector()
	d.RegisterResource(&Resource{ID: "r2", Type: "session", CreatedAt: time.Now(), LastUsed: time.Now()})
	d.AddPolicy(Policy{Name: "stale", Type: "session", MaxIdle: 1 * time.Hour})
	orphans := d.Scan()
	if len(orphans) != 0 { t.Error("should not find orphans") }
}
func TestDetectorAutoClean(t *testing.T) {
	d := NewDetector()
	cleaned := 0
	d.SetCleanup(func(r *Resource) error { cleaned++; return nil })
	d.RegisterResource(&Resource{ID: "r3", Type: "session", CreatedAt: time.Now().Add(-3 * time.Hour), LastUsed: time.Now().Add(-2 * time.Hour)})
	d.AddPolicy(Policy{Name: "stale", Type: "session", MaxIdle: 30 * time.Minute, AutoClean: true})
	orphans := d.Scan()
	c, _ := d.Clean(orphans)
	if c != 1 { t.Error("should clean 1") }
}
func TestTouch(t *testing.T) {
	d := NewDetector()
	d.RegisterResource(&Resource{ID: "t1", Type: "cache", CreatedAt: time.Now().Add(-2 * time.Hour), LastUsed: time.Now().Add(-2 * time.Hour)})
	d.Touch("t1")
	d.AddPolicy(Policy{Name: "expired", Type: "cache", MaxIdle: 1 * time.Hour})
	orphans := d.Scan()
	if len(orphans) != 0 { t.Error("touch should prevent orphan") }
}
