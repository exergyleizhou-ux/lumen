package connector

import (
	"fmt"
	"testing"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(&Connector{Name: "api", Type: "rest", Endpoint: "http://localhost"})
	c, ok := r.Get("api")
	if !ok || c.Name != "api" {
		t.Error("register/get")
	}
	r.Remove("api")
	if _, ok := r.Get("api"); ok {
		t.Error("remove")
	}
}
func TestHealthCheck(t *testing.T) {
	r := NewRegistry()
	r.Register(&Connector{Name: "svc", Type: "rest"})
	r.SetHealthCheck(func(c *Connector) error { return nil })
	results := r.HealthCheckAll()
	if len(results) != 1 || !results[0].Healthy {
		t.Error("health")
	}
}
func TestCircuitBreaker(t *testing.T) {
	r := NewRegistry()
	r.maxFailures = 2
	r.Register(&Connector{Name: "cb", Type: "rest"})
	r.SetHealthCheck(func(c *Connector) error { return fmt.Errorf("fail") })
	r.HealthCheckAll()
	r.HealthCheckAll()
	if !r.CircuitBroken("cb") {
		t.Error("circuit should be open")
	}
}
func TestRESTConnector(t *testing.T) {
	rc := NewRESTConnector("http://example.com")
	rc.SetHeader("X-Test", "1")
	if rc.BaseURL != "http://example.com" {
		t.Error("baseurl")
	}
}
