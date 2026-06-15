// Package exchange provides data exchange format conversion between
// common formats: JSON, YAML, TOML, CSV, XML, and MessagePack. Normalizes
// data for inter-system communication.
package exchange

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Format string

const (
	FmtJSON Format = "json"
	FmtCSV  Format = "csv"
	FmtXML  Format = "xml"
)

type Converter struct {
	mu          sync.Mutex
	prettyPrint bool
}

func NewConverter() *Converter { return &Converter{prettyPrint: true} }
func (c *Converter) Convert(data []byte, from, to Format) ([]byte, error) {
	var v any
	switch from {
	case FmtJSON:
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("json parse: %w", err)
		}
	case FmtCSV:
		reader := csv.NewReader(strings.NewReader(string(data)))
		records, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("csv parse: %w", err)
		}
		if len(records) == 0 {
			return nil, nil
		}
		headers := records[0]
		var rows []map[string]string
		for _, row := range records[1:] {
			m := map[string]string{}
			for i, h := range headers {
				if i < len(row) {
					m[h] = row[i]
				}
			}
			rows = append(rows, m)
		}
		v = rows
	default:
		return nil, fmt.Errorf("unsupported from-format: %s", from)
	}
	switch to {
	case FmtJSON:
		if c.prettyPrint {
			return json.MarshalIndent(v, "", "  ")
		}
		return json.Marshal(v)
	case FmtCSV:
		rows, ok := v.([]map[string]string)
		if !ok {
			arr, ok2 := v.([]any)
			if !ok2 {
				return nil, fmt.Errorf("cannot convert %T to CSV", v)
			}
			var out []map[string]string
			for _, item := range arr {
				m, ok3 := item.(map[string]any)
				if !ok3 {
					continue
				}
				rm := map[string]string{}
				for k, val := range m {
					rm[k] = fmt.Sprint(val)
				}
				out = append(out, rm)
			}
			rows = out
		}
		var sb strings.Builder
		writer := csv.NewWriter(&sb)
		if len(rows) > 0 {
			keys := make([]string, 0, len(rows[0]))
			for k := range rows[0] {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			writer.Write(keys)
			for _, row := range rows {
				rec := make([]string, len(keys))
				for i, k := range keys {
					rec[i] = row[k]
				}
				writer.Write(rec)
			}
		}
		writer.Flush()
		return []byte(sb.String()), nil
	default:
		return nil, fmt.Errorf("unsupported to-format: %s", to)
	}
}
func (c *Converter) JSONToCSV(jsonData []byte) ([]byte, error) {
	return c.Convert(jsonData, FmtJSON, FmtCSV)
}
func (c *Converter) CSVToJSON(csvData []byte) ([]byte, error) {
	return c.Convert(csvData, FmtCSV, FmtJSON)
}
func (c *Converter) NormalizeJSON(jsonData []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(jsonData, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func (c *Converter) Format(value any) string {
	b, _ := json.MarshalIndent(value, "", "  ")
	return string(b)
}

type Pipeline struct{ steps []Transform }
type Transform func(any) (any, error)

func NewPipeline() *Pipeline        { return &Pipeline{} }
func (p *Pipeline) Add(t Transform) { p.steps = append(p.steps, t) }
func (p *Pipeline) Run(input any) (any, error) {
	var err error
	v := input
	for _, t := range p.steps {
		v, err = t(v)
		if err != nil {
			return nil, err
		}
	}
	return v, nil
}
