package keys

import (
	"testing"
)

func TestGenerate(t *testing.T) {
	m := NewManager()
	k, _, err := m.Generate("signing", "sig")
	if err != nil {
		t.Fatal(err)
	}
	if len(k.ID) != 24 {
		t.Error("id length")
	}
}
func TestGet(t *testing.T) {
	m := NewManager()
	k, _, _ := m.Generate("enc", "enc")
	got, ok := m.Get(k.ID)
	if !ok || got.Label != "enc" {
		t.Error("get")
	}
}
func TestRotate(t *testing.T) {
	m := NewManager()
	k, _, _ := m.Generate("rot", "r")
	newK, _, err := m.Rotate(k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newK.ID == k.ID {
		t.Error("should be new key")
	}
}
func TestExpired(t *testing.T) {
	m := NewManager()
	m.rotationInterval = 0
	k, _, _ := m.Generate("exp", "e")
	_ = k
	expired := m.Expired()
	if len(expired) == 0 {
		t.Error("should be expired")
	}
}
