package exchange
import ("strings";"testing")
func TestJSONToCSV(t *testing.T) {
	c := NewConverter()
	csv, err := c.JSONToCSV([]byte(`[{"a":"1","b":"2"},{"a":"3","b":"4"}]`))
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(csv), "1") { t.Error("csv content") }
}
func TestCSVToJSON(t *testing.T) {
	c := NewConverter()
	jsonData, err := c.CSVToJSON([]byte("name,age\nAlice,30\nBob,25"))
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(jsonData), "Alice") { t.Error("json content") }
}
func TestNormalize(t *testing.T) {
	c := NewConverter()
	normalized, _ := c.NormalizeJSON([]byte(`{"b":2,"a":1}`))
	if !strings.Contains(string(normalized), "a") { t.Error("normalize") }
}
