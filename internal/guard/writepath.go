package guard

import (
	"path/filepath"
	"strings"
)

// Path SEGMENTS whose presence anywhere makes a write dangerous regardless of the
// root (~/.ssh, /home/u/.ssh, /root/.ssh all contain "/.ssh/").
var sensitiveWriteSegments = []struct {
	seg    string
	reason string
}{
	{"/.ssh/", "write into ~/.ssh (SSH key / authorized_keys injection)"},
	{"/.git/hooks/", "write a git hook (executes on git operations — RCE)"},
	{"/.aws/", "write AWS credentials/config"},
	{"/.kube/", "write kube config (cluster hijack)"},
	{"/.gnupg/", "write into GnuPG keyring"},
	{"/.config/fish/", "write fish shell config (persistence)"},
	{"/.config/autostart/", "write an autostart entry (persistence)"},
}

// Absolute path PREFIXES that are system locations an agent must not write.
var sensitiveWritePrefixes = []struct {
	prefix string
	reason string
}{
	{"/etc/", "write to a system config under /etc"},
	{"/usr/", "write under /usr (system binaries/libs)"},
	{"/bin/", "write under /bin"},
	{"/sbin/", "write under /sbin"},
	{"/boot/", "write under /boot"},
	{"/lib/", "write under /lib"},
	{"/lib64/", "write under /lib64"},
	{"/var/spool/cron", "write a cron job"},
}

// Shell rc / login files — dangerous only when they are the user's real dotfiles
// (home-anchored), so a repo file like templates/bashrc.tmpl is not flagged.
var shellRCBasenames = map[string]bool{
	".bashrc": true, ".bash_profile": true, ".bash_login": true, ".profile": true,
	".zshrc": true, ".zprofile": true, ".zshenv": true, ".zlogin": true,
	".kshrc": true, ".netrc": true, ".bash_aliases": true,
}

// CheckWritePath blocks write_file/edit_file targets that grant persistence or
// code execution — SSH keys, shell rc files, git hooks, cron, cred stores, and
// system dirs. It fires in ALL permission modes (like CheckBash): in bypass /
// headless mode writer tools are otherwise auto-approved with no path check, so a
// prompt-injected or compromised model could plant a backdoor.
func CheckWritePath(path string) CheckResult {
	p := StripHiddenChars(strings.TrimSpace(path))
	if p == "" {
		return CheckResult{Safe: true}
	}
	// Expand a leading $HOME/${HOME} to ~ so home-anchoring is uniform, then
	// collapse traversal so "a/../../.ssh/x" is judged on its effective target.
	p = strings.ReplaceAll(p, "${HOME}", "~")
	p = strings.ReplaceAll(p, "$HOME", "~")
	clean := filepath.ToSlash(filepath.Clean(p))
	lower := strings.ToLower(clean)

	// Segment matches: prepend "/" so a leading segment ("~/.ssh/...",
	// ".ssh/...") is matched uniformly as "/.ssh/".
	probe := lower
	if !strings.HasPrefix(probe, "/") {
		probe = "/" + probe
	}
	for _, s := range sensitiveWriteSegments {
		if strings.Contains(probe, s.seg) {
			return CheckResult{Safe: false, Reason: s.reason}
		}
	}
	for _, pre := range sensitiveWritePrefixes {
		if strings.HasPrefix(lower, pre.prefix) {
			return CheckResult{Safe: false, Reason: pre.reason}
		}
	}
	// Home-anchored shell rc / login files.
	if strings.HasPrefix(lower, "~/") || strings.HasPrefix(lower, "/root/") || strings.HasPrefix(lower, "/home/") {
		if shellRCBasenames[strings.ToLower(filepath.Base(clean))] {
			return CheckResult{Safe: false, Reason: "write to a shell startup file (persistence)"}
		}
	}
	return CheckResult{Safe: true}
}
