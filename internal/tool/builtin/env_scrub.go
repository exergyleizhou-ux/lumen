package builtin

import "strings"

// scrubSecrets returns env (KEY=VALUE entries) with credential-bearing variables
// removed. The bash tool runs model-supplied (and possibly prompt-injected)
// commands; without this every API key / token in the agent's own environment
// would be readable by `env`, `printenv`, or a `curl ...?k=$OPENAI_API_KEY`
// exfil. Non-secret vars (PATH, HOME, GOFLAGS, LANG, …) are preserved so builds
// and tools still work.
func scrubSecrets(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			out = append(out, kv) // malformed entry — pass through untouched
			continue
		}
		if isSecretName(kv[:i]) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isSecretName(name string) bool {
	u := strings.ToUpper(name)
	for _, s := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "_PAT"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}
