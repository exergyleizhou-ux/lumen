package selector
import ("testing")
func TestParseMatch(t *testing.T) {
	s, err := Parse("env=prod,region in (us-east,us-west)")
	if err != nil { t.Fatal(err) }
	if !s.Matches(map[string]string{"env": "prod", "region": "us-east"}) { t.Error("should match") }
	if s.Matches(map[string]string{"env": "dev"}) { t.Error("should not match") }
}
func TestNotEqual(t *testing.T) {
	s, _ := Parse("env!=staging")
	if !s.Matches(map[string]string{"env": "prod"}) { t.Error("should match") }
}
func TestExists(t *testing.T) {
	s, _ := Parse("required-key")
	if !s.Matches(map[string]string{"required-key": "any"}) { t.Error("exists") }
	if s.Matches(map[string]string{"other": "v"}) { t.Error("should fail exists") }
}
func TestFilter(t *testing.T) {
	f := NewFilter()
	f.Add("a", map[string]string{"env": "prod"})
	f.Add("b", map[string]string{"env": "staging"})
	s, _ := Parse("env=prod")
	ids := f.Match(s)
	if len(ids) != 1 || ids[0] != "a" { t.Error("filter") }
}
