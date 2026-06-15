package cloud

import ("strings";"testing")

func TestMockProvider(t *testing.T) {
	mp := NewMockProvider("aws", []string{"us-east-1", "us-west-2"})
	regions, _ := mp.Regions()
	if len(regions) != 2 { t.Error("regions") }
	d, err := mp.Deploy(DeployConfig{Name: "test", Region: "us-east-1", Image: "ami-123", Resources: ResourceSpec{CPU: "2", Memory: "4Gi"}})
	if err != nil || d.Status != "running" { t.Error("deploy") }
	st, _ := mp.Status(d.ID)
	if st.ID != d.ID { t.Error("status") }
	mp.Destroy(d.ID)
	_, err = mp.Status(d.ID)
	if err == nil { t.Error("should be gone") }
}
func TestMultiCloud(t *testing.T) {
	mc := NewMultiCloud()
	mc.Register(NewMockProvider("aws", []string{"us-east-1"}))
	mc.Register(NewMockProvider("gcp", []string{"us-central1"}))
	if len(mc.Providers()) != 2 { t.Error("providers") }
	d, _ := mc.Deploy("aws", DeployConfig{Name: "svc", Region: "us-east-1", Image: "img"})
	if d.Provider != "aws" { t.Error("provider not set") }
	list := mc.List()
	if len(list) != 1 { t.Error("list") }
	mc.Destroy(d.ID)
	if len(mc.List()) != 0 { t.Error("should be empty") }
}
func TestEstimateCost(t *testing.T) {
	e := EstimateCost("aws", "us-east-1", ResourceSpec{CPU: "2", Memory: "4Gi"})
	if e.TotalMonthly <= 0 { t.Error("cost") }
	s := FormatEstimation(e)
	if !strings.Contains(s, "Monthly") { t.Error("format") }
}
func TestOptimizeResources(t *testing.T) {
	opt := OptimizeResources(ResourceSpec{CPU: "4", Memory: "8Gi"}, 10, 10)
	if opt == nil { t.Error("should suggest optimization") }
}
