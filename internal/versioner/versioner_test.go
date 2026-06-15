package versioner
import ("testing")
func TestParse(t *testing.T){v,err:=Parse("1.2.3-beta+build");if err!=nil{t.Fatal(err)};if v.Major!=1||v.Minor!=2||v.Patch!=3{t.Error("parse")};if v.PreRelease!="beta"||v.Build!="build"{t.Error("pre/build")}}
func TestCompare(t *testing.T){a,_:=Parse("1.0.0");b,_:=Parse("2.0.0");if a.Compare(b)>=0{t.Error("compare")}}
func TestBump(t *testing.T){v,_:=Parse("1.2.3");if v.BumpMajor().String()!="2.0.0"{t.Error("major")};if v.BumpMinor().String()!="1.3.0"{t.Error("minor")};if v.BumpPatch().String()!="1.2.4"{t.Error("patch")}}
func TestConstraint(t *testing.T){v,_:=Parse("1.5.0");c,_:=ParseConstraint(">=1.0.0");if!c.Satisfies(v){t.Error("should satisfy")};c2,_:=ParseConstraint("^1.0.0");if!c2.Satisfies(v){t.Error("should satisfy ^")}}
func TestRepository(t *testing.T){r:=NewRepository();v1,_:=Parse("1.0.0");v2,_:=Parse("2.0.0");r.Register(v1);r.Register(v2);if r.Latest().String()!="2.0.0"{t.Error("latest")}}
