# DSML tool-use text leak (DeepSeek)

## Symptom
DeepSeek occasionally emits tool calls as plain text DSML markers (`<｜｜DSML｜｜tool_calls>…`). Claude Science treats them as text; tools never execute → session appears stuck (CSswitch issue #8).

## Root cause
Upstream model wire format leak; not Anthropic `tool_use` blocks.

## Fix in Lumen
`internal/science/proxy/dsml.go` + `dsml_stream.go`; modes `off` (default) / `detect` / `rewrite` via `LUMEN_TOOLUSE_SHIM` or config `tooluse_shim`.

## Evidence
- Unit: `dsml_test.go`, `dsml_e2e_test.go`
- CSswitch port: `v0.3.0-beta.2` changelog + `test_proxy_dsml_e2e.py`