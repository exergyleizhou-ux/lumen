// Package jupyter provides interactive notebook support for the science lab.
// When Jupyter is available (via conda or system), notebook cells can be
// created, executed, and read from the lab UI. Otherwise, a read-only JSON
// notebook viewer is provided.
package jupyter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Cell represents a notebook cell.
type Cell struct {
	CellType string   `json:"cell_type"` // code | markdown
	Source   []string `json:"source"`
	Outputs  []Output `json:"outputs,omitempty"`
}

// Output is one cell execution output.
type Output struct {
	OutputType string `json:"output_type"` // stream | execute_result | error
	Text       string `json:"text,omitempty"`
	Name       string `json:"name,omitempty"` // stdout | stderr
}

// Notebook represents an .ipynb file.
type Notebook struct {
	Metadata map[string]any `json:"metadata"`
	Nbformat int            `json:"nbformat"`
	NbformatMinor int       `json:"nbformat_minor"`
	Cells    []Cell         `json:"cells"`
	Path     string         `json:"path,omitempty"`
}

// New creates an empty notebook.
func New(title string) *Notebook {
	return &Notebook{
		Metadata: map[string]any{
			"kernelspec": map[string]any{
				"display_name": "Python 3",
				"language":     "python",
				"name":         "python3",
			},
			"title": title,
		},
		Nbformat:       4,
		NbformatMinor:  5,
		Cells:          []Cell{{CellType: "markdown", Source: []string{"# " + title}}},
	}
}

// Load reads a .ipynb file.
func Load(path string) (*Notebook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var nb Notebook
	if err := json.Unmarshal(data, &nb); err != nil {
		return nil, err
	}
	nb.Path = path
	return &nb, nil
}

// Save writes to a .ipynb file.
func (nb *Notebook) Save(path string) error {
	data, err := json.MarshalIndent(nb, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// AddCode appends a code cell.
func (nb *Notebook) AddCode(source string) {
	nb.Cells = append(nb.Cells, Cell{CellType: "code", Source: []string{source}})
}

// AddMarkdown appends a markdown cell.
func (nb *Notebook) AddMarkdown(source string) {
	nb.Cells = append(nb.Cells, Cell{CellType: "markdown", Source: []string{source}})
}

// IsAvailable checks if jupyter is on PATH or via common science conda envs.
func IsAvailable() bool {
	if _, err := exec.LookPath("jupyter"); err == nil {
		return true
	}
	// common Lumen science conda env
	candidates := []string{
		os.Getenv("LUMEN_JUPYTER"),
		filepath.Join(os.Getenv("HOME"), ".lumen/science/sandbox/home/.claude-science/conda/envs/operon-mcp/bin/jupyter"),
		"/root/.lumen/science/sandbox/home/.claude-science/conda/envs/operon-mcp/bin/jupyter",
		"/usr/local/bin/jupyter",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// JupyterBin returns the jupyter executable path if available.
func JupyterBin() string {
	if p, err := exec.LookPath("jupyter"); err == nil {
		return p
	}
	candidates := []string{
		os.Getenv("LUMEN_JUPYTER"),
		filepath.Join(os.Getenv("HOME"), ".lumen/science/sandbox/home/.claude-science/conda/envs/operon-mcp/bin/jupyter"),
		"/root/.lumen/science/sandbox/home/.claude-science/conda/envs/operon-mcp/bin/jupyter",
		"/usr/local/bin/jupyter",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// Execute runs all code cells using jupyter nbconvert --execute.
func (nb *Notebook) Execute(path, python string) error {
	if python == "" {
		python = "python3"
	}
	// Save first
	if err := nb.Save(path); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if jbin := JupyterBin(); jbin != "" {
		// Prefer explicit jupyter binary (conda env)
		cmd = exec.Command(jbin, "nbconvert", "--to", "notebook",
			"--execute", "--inplace", "--ExecutePreprocessor.timeout=120", path)
	} else {
		cmd = exec.Command(python, "-m", "jupyter", "nbconvert", "--to", "notebook",
			"--execute", "--inplace", "--ExecutePreprocessor.timeout=120", path)
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nbconvert: %w (output: %s)", err, string(out))
	}
	// Reload with outputs
	reloaded, err := Load(path)
	if err != nil {
		return err
	}
	nb.Cells = reloaded.Cells
	nb.Path = reloaded.Path
	return nil
}

// ToMarkdown renders notebook as markdown for chat display.
func (nb *Notebook) ToMarkdown() string {
	var b strings.Builder
	for _, c := range nb.Cells {
		switch c.CellType {
		case "markdown":
			for _, s := range c.Source {
				b.WriteString(s)
			}
			b.WriteString("\n\n")
		case "code":
			b.WriteString("```python\n")
			for _, s := range c.Source {
				b.WriteString(s)
			}
			b.WriteString("\n```\n")
			for _, o := range c.Outputs {
				if o.Text != "" {
					b.WriteString("```\n" + o.Text + "\n```\n")
				}
			}
		}
	}
	return b.String()
}

// NotebookInfo is metadata returned by the lab API.
type NotebookInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Cells    int       `json:"cells"`
}

// ListNotebooks returns .ipynb files under a workspace directory.
func ListNotebooks(workspace string) ([]NotebookInfo, error) {
	dir := filepath.Join(workspace, "notebooks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []NotebookInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ipynb") {
			continue
		}
		info, _ := e.Info()
		ni := NotebookInfo{Name: e.Name(), Path: filepath.Join(dir, e.Name())}
		if info != nil {
			ni.Size = info.Size()
			ni.Modified = info.ModTime()
		}
		// Quick cell count
		if nb, err := Load(ni.Path); err == nil {
			ni.Cells = len(nb.Cells)
		}
		out = append(out, ni)
	}
	return out, nil
}
