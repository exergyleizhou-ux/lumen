package template
import ("strings";"testing";"time")
func TestEngineRender(t *testing.T) {
	e := NewEngine()
	e.LoadFile("hello", "Hello {{.Name}}!")
	result, err := e.Render("hello", map[string]string{"Name": "Lumen"})
	if err != nil { t.Fatal(err) }
	if !strings.Contains(result, "Hello Lumen!") { t.Error("render") }
}
func TestRenderString(t *testing.T) {
	e := NewEngine()
	result, _ := e.RenderString("Version: {{.V}}", map[string]string{"V": "1.0"})
	if result != "Version: 1.0" { t.Error("inline render") }
}
func TestPromptBuilder(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection("System", "You are helpful.", 0)
	pb.AddSection("User", "Hello.", 1)
	s := pb.Build()
	if !strings.Contains(s, "## System") { t.Error("prompt build") }
}
func TestGenerateReport(t *testing.T) {
	r := GenerateReport(ReportData{Title: "Test", Date: time.Now(), Summary: "A report", Sections: []ReportSection{{Title: "Section", Body: "Body", Metrics: map[string]float64{"cpu": 45.5}}}})
	if !strings.Contains(r, "# Test") { t.Error("report") }
}
