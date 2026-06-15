package notary
import ("testing";"time")
func TestGenerateAndSign(t *testing.T){n:=NewNotary();kp,err:=n.GenerateAndAddKey("test");if err!=nil{t.Fatal(err)};att,err:=n.Sign(kp.KeyID(),"file://test",map[string]any{"hash":"abc"},time.Hour);if err!=nil{t.Fatal(err)};if att.Subject!="file://test"{t.Error("subject")};ok,_:=n.Verify(att);if!ok{t.Error("verify")}}
func TestHashChainAppend(t *testing.T){hc:=NewHashChain();hc.Append("data1");hc.Append("data2");if hc.Length()!=2{t.Error("length")}}
func TestExpired(t *testing.T){n:=NewNotary();kp,_:=n.GenerateAndAddKey("exp");att,_:=n.Sign(kp.KeyID(),"x",nil,0);att.ExpiresAt=time.Now().Add(-time.Hour);ok,_:=n.Verify(att);if ok{t.Error("expired should reject")}}
