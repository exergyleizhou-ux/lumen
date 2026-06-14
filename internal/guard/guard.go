// Package guard provides bash command safety checks adapted from Ember attack
// findings and claw-code's bash_validation.rs. It detects:
//
// 1. Data exfiltration (curl/wget -d to external hosts)
// 2. Sensitive file reads (/.env, /.ssh, /etc/passwd, /etc/shadow)
// 3. Post-exploitation reconnaissance (ps aux, netstat, lsof -i)
// 4. Destructive commands (rm -rf, dd, mkfs, :(){ :|:& };:)
// 5. Encoded/obfuscated payloads (base64 -d, eval, xxd -r)
package guard

import (
	"regexp"
	"strings"
)

// CheckResult reports whether a bash command is safe and why.
type CheckResult struct {
	Safe   bool
	Reason string
}

// CheckBash analyzes a shell command for dangerous patterns. It returns
// Safe=false when the command should be blocked regardless of permission mode.
func CheckBash(command string) CheckResult {
	// Normalize: strip empty-string quote obfuscation (e.g. sh''adow -> shadow),
	// collapse whitespace, lowercase for pattern matching.
	unquoted := strings.NewReplacer("'", "", "\"", "").Replace(command)
	normalized := strings.ToLower(strings.Join(strings.Fields(unquoted), " "))

	// ── 1. Data exfiltration ──────────────────────────
	if r := checkExfiltration(normalized); !r.Safe {
		return r
	}

	// ── 2. Sensitive file reads ──────────────────────
	if r := checkSensitiveReads(normalized); !r.Safe {
		return r
	}

	// ── 3. Post-exploitation reconnaissance ──────────
	if r := checkReconnaissance(normalized); !r.Safe {
		return r
	}

	// ── 4. Destructive commands ───────────────────────
	if r := checkDestructive(normalized); !r.Safe {
		return r
	}
	if r := checkDestructiveRm(normalized); !r.Safe {
		return r
	}

	// ── 5. Encoded/obfuscated payloads ────────────────
	if r := checkEncoded(normalized); !r.Safe {
		return r
	}

	return CheckResult{Safe: true}
}

// ── Exfiltration patterns ──────────────────────────────────

var exfilPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`curl\s+.*(-d\s*@|--data(-binary|-raw)?\s*@)`), "curl data exfiltration (reading local files and sending via POST)"},
	{regexp.MustCompile(`wget\s+.*--post-file`), "wget data exfiltration (posting local files)"},
	{regexp.MustCompile(`curl\s+.*\s+-o\s+/dev/null.*\s+-d\s+@`), "silent curl exfiltration"},
	{regexp.MustCompile(`nc\s+.*\s+<\s+/`), "netcat file exfiltration"},
	{regexp.MustCompile(`scp\s+`), "scp file transfer (potential exfiltration)"},
	{regexp.MustCompile(`rsync\s+.*\s+\w+@`), "rsync to remote host"},
	{regexp.MustCompile(`curl\s+.*evil\.com|curl\s+.*exfil|curl\s+.*attacker|curl\s+.*\.ngrok|curl\s+.*webhook`), "curl to known-malicious/exfiltration host pattern"},
}

func checkExfiltration(cmd string) CheckResult {
	for _, p := range exfilPatterns {
		if p.pattern.MatchString(cmd) {
			return CheckResult{Safe: false, Reason: p.reason}
		}
	}
	return CheckResult{Safe: true}
}

// ── Sensitive file reads ──────────────────────────────────

var sensitivePaths = []string{
	"/etc/passwd", "/etc/shadow", "/etc/master.passwd",
	"/etc/ssl/private", "/etc/ssh/ssh_host",
	"/root/.ssh", "/root/.bash_history",
	".env", ".env.local", ".env.production", ".env.staging",
	"credentials", "secrets", "id_rsa", "id_ed25519",
	".aws/credentials", ".gcloud/", ".config/gcloud",
	".kube/config", ".docker/config.json",
	"keychain", "login.keychain",
}

func checkSensitiveReads(cmd string) CheckResult {
	for _, path := range sensitivePaths {
		if strings.Contains(cmd, path) {
			return CheckResult{Safe: false, Reason: "attempting to read sensitive file: " + path}
		}
	}
	// Also catch: find . -name ".env" -exec cat {} \;
	if strings.Contains(cmd, ".env") && (strings.Contains(cmd, "-exec cat") || strings.Contains(cmd, "-exec grep")) {
		return CheckResult{Safe: false, Reason: "mass .env file harvesting via find -exec"}
	}
	return CheckResult{Safe: true}
}

// ── Reconnaissance patterns ────────────────────────────────

var reconPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`ps\s+(aux|auxwww|ef|af)`), "process enumeration (post-exploitation recon)"},
	{regexp.MustCompile(`netstat\s+-[a-z]*[ntlp]`), "network connection enumeration"},
	{regexp.MustCompile(`ss\s+-[a-z]*[ntlp]`), "socket enumeration"},
	{regexp.MustCompile(`lsof\s+-i`), "open port enumeration"},
	{regexp.MustCompile(`find\s+/.*-name.*\.env.*-exec\s+cat`), "mass credential harvesting"},
	{regexp.MustCompile(`find\s+/.*-name.*\.pem.*-exec\s+cat`), "private key harvesting"},
	{regexp.MustCompile(`history\s*\|`), "shell history extraction"},
	{regexp.MustCompile(`lastlog|last\s+-`), "login history enumeration"},
	{regexp.MustCompile(`who\s+-a|w\s+-`), "active session enumeration"},
}

func checkReconnaissance(cmd string) CheckResult {
	for _, p := range reconPatterns {
		if p.pattern.MatchString(cmd) {
			return CheckResult{Safe: false, Reason: p.reason}
		}
	}
	return CheckResult{Safe: true}
}

// ── Destructive commands ──────────────────────────────────

var destructivePatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`rm\s+-rf\s+/`), "recursive root removal — catastrophic"},
	{regexp.MustCompile(`rm\s+-rf\s+~`), "home directory removal"},
	{regexp.MustCompile(`rm\s+-rf\s+\*`), "wildcard recursive removal"},
	{regexp.MustCompile(`mkfs\.|mke2fs|newfs`), "filesystem formatting"},
	{regexp.MustCompile(`dd\s+if=/dev/zero\s+of=/dev/`), "disk zeroing"},
	{regexp.MustCompile(`>\s*/dev/(sd[a-z]|nvme|hd[a-z]|disk)`), "raw device overwrite"},
	{regexp.MustCompile(`chmod\s+-r\s+777\s+/`), "world-writable root"},
	{regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`), "fork bomb"},
}

func checkDestructive(cmd string) CheckResult {
	for _, p := range destructivePatterns {
		if p.pattern.MatchString(cmd) {
			return CheckResult{Safe: false, Reason: p.reason}
		}
	}
	return CheckResult{Safe: true}
}

var (
	rmPresent         = regexp.MustCompile(`(^|[;&|]|\s)rm\s`)
	rmRecursive       = regexp.MustCompile(`\s-[a-z]*r`)
	rmDangerousTarget = regexp.MustCompile(`\s(/|~|\*|/\*)(\s|$)`)
)

// checkDestructiveRm catches recursive rm of a dangerous target across flag
// spellings the literal patterns miss: -rf, -fr, --recursive --force, and
// especially --no-preserve-root (the form that actually removes /). cmd is the
// normalized command (lowercased, single-spaced, quotes stripped).
func checkDestructiveRm(cmd string) CheckResult {
	if !rmPresent.MatchString(" " + cmd + " ") {
		return CheckResult{Safe: true}
	}
	recursive := rmRecursive.MatchString(" "+cmd) || strings.Contains(cmd, "--recursive")
	if !recursive {
		return CheckResult{Safe: true}
	}
	if strings.Contains(cmd, "--no-preserve-root") || rmDangerousTarget.MatchString(cmd) {
		return CheckResult{Safe: false, Reason: "recursive removal of a dangerous target (root / home / wildcard)"}
	}
	return CheckResult{Safe: true}
}

// ── Encoded/obfuscated payload detection ──────────────────

var encodedPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`base64\s+-d.*\|.*sh\b`), "base64-decoded shell execution (obfuscation)"},
	{regexp.MustCompile(`base64\s+--decode.*\|.*bash\b`), "base64-decoded bash execution"},
	{regexp.MustCompile(`xxd\s+-r\s+-p.*\|.*sh\b`), "hex-decoded shell execution"},
	{regexp.MustCompile(`eval\s+`), "eval of dynamic content (potential code injection)"},
	{regexp.MustCompile(`\$\(.*curl|` + "`" + `.*curl` + "`" + `\)`), "command substitution with curl"},
	{regexp.MustCompile(`python.*-c\s+.*import\s+(base64|subprocess|os|socket|requests)`), "Python obfuscated execution"},
	{regexp.MustCompile(`perl\s+-e\s+.*system`), "Perl system call"},
	{regexp.MustCompile(`ruby\s+-e\s+.*exec|ruby\s+-e\s+.*system`), "Ruby exec/system call"},
}

func checkEncoded(cmd string) CheckResult {
	for _, p := range encodedPatterns {
		if p.pattern.MatchString(cmd) {
			return CheckResult{Safe: false, Reason: p.reason}
		}
	}
	return CheckResult{Safe: true}
}

// ── Zero-width & invisible character detection ─────────────

// ContainsHiddenChars detects zero-width characters and Unicode control
// characters commonly used in indirect injection attacks.
func ContainsHiddenChars(s string) bool {
	for _, r := range s {
		switch {
		case r == '\u200B': // zero-width space
			return true
		case r == '\u200C': // zero-width non-joiner
			return true
		case r == '\u200D': // zero-width joiner
			return true
		case r == '\uFEFF': // BOM / zero-width no-break space
			return true
		case r == '\u200E': // left-to-right mark
			return true
		case r == '\u200F': // right-to-left mark
			return true
		case r == '\u202A': // left-to-right embedding
			return true
		case r == '\u202B': // right-to-left embedding
			return true
		case r == '\u202C': // pop directional formatting
			return true
		case r == '\u202D': // left-to-right override
			return true
		case r == '\u202E': // right-to-left override
			return true
		case r == '\u2060': // word joiner
			return true
		case r == '\u2061': // function application
			return true
		case r == '\u2062': // invisible times
			return true
		case r == '\u2063': // invisible separator
			return true
		case r == '\u2064': // invisible plus
			return true
		}
	}
	return false
}

// StripHiddenChars removes zero-width and invisible Unicode characters.
func StripHiddenChars(s string) string {
	var out []rune
	for _, r := range s {
		switch {
		case r == '\u200B', r == '\u200C', r == '\u200D', r == '\uFEFF':
			// skip
		case r == '\u200E', r == '\u200F':
			// skip
		case r >= '\u202A' && r <= '\u202E':
			// skip
		case r == '\u2060', r == '\u2061', r == '\u2062', r == '\u2063', r == '\u2064':
			// skip
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
