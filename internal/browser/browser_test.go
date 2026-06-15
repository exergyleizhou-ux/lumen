package browser

import "testing"

func TestAvailable(t *testing.T) {
	avail := Available()
	t.Logf("chrome available: %v", avail)
}

func TestExistingClient(t *testing.T) {
	c := ExistingClient("http://localhost:9222")
	if c == nil {
		t.Error("ExistingClient should not be nil")
	}
}

func TestStripHTML(t *testing.T) {
	input := "<html><body><p>Hello</p></body></html>"
	result := stripHTML(input)
	if result != "Hello" {
		t.Errorf("stripHTML: got %q", result)
	}
}

func TestFindChrome(t *testing.T) {
	chrome := findChrome()
	t.Logf("chrome path: %q", chrome)
}
