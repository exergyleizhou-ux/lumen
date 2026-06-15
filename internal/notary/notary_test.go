package notary
import ("testing";"time")
func TestGenerateAndSign(t *testing.T) {
	n := NewNotary()
	kp, err := n.GenerateAndAddKey("test-key")
	if err != nil { t.Fatal(err) }
	att, err := n.Sign(kp.KeyID(), "file://test.txt", map[string]any{"sha256": "abc123"}, time.Hour)
	if err != nil { t.Fatal(err) }
	if att.Subject != "file://test.txt" { t.Error("subject") }
	ok, reason := n.Verify(att)
	if !ok { t.Errorf("verify failed: %s", reason) }
}
func TestHashChain(t *testing.T) {
	hc := NewHashChain()
	hc.Append("data1")
	hc.Append("data2")
	if hc.Length() != 2 { t.Error("length") }
	ok, issues := hc.VerifyChain()
	if !ok { t.Errorf("chain broken: %v", issues) }
}
func TestExpiredAttestation(t *testing.T) {
	n := NewNotary()
	kp, _ := n.GenerateAndAddKey("exp-key")
	att, _ := n.Sign(kp.KeyID(), "temp", nil, 0)
	att.ExpiresAt = time.Now().Add(-time.Hour)
	ok, _ := n.Verify(att)
	if ok { t.Error("should reject expired") }
}
