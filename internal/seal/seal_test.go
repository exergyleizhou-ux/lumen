package seal

import (
	"testing"
)

func TestSealUnseal(t *testing.T) {
	m := NewManager()
	m.GenerateKey()
	env, err := m.Seal([]byte("secret message"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := m.Unseal(env)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "secret message" {
		t.Error("unseal mismatch")
	}
}
func TestRotate(t *testing.T) {
	m := NewManager()
	k1, _ := m.GenerateKey()
	k2, err := m.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if k2.ID == k1.ID {
		t.Error("should be new key")
	}
}
func TestCurrent(t *testing.T) {
	m := NewManager()
	m.GenerateKey()
	k, ok := m.Current()
	if !ok || k == nil {
		t.Error("current should exist")
	}
}
