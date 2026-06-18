package guard

import "testing"

// TestCheckWritePath: writer tools (write_file/edit_file) are auto-approved in
// bypass/headless mode with no path inspection, so a prompt-injected model could
// plant persistence (shell rc, SSH authorized_keys, git hooks) or clobber system
// files. CheckWritePath must block those targets — including via ~/$HOME and
// path-traversal — while leaving normal project writes alone.
func TestCheckWritePath(t *testing.T) {
	dangerous := []string{
		"~/.ssh/authorized_keys",
		"~/.ssh/id_rsa",
		"/home/dev/.ssh/authorized_keys",
		".git/hooks/pre-commit",
		"repo/sub/.git/hooks/post-checkout",
		"~/.bashrc",
		"~/.zshrc",
		"$HOME/.bash_profile",
		"/root/.profile",
		"/etc/cron.d/evil",
		"/etc/sudoers",
		"/usr/local/bin/x",
		"~/.aws/credentials",
		"~/.config/fish/config.fish",
		"foo/../../../.ssh/authorized_keys", // traversal
	}
	for _, p := range dangerous {
		if r := CheckWritePath(p); r.Safe {
			t.Errorf("sensitive write path not blocked: %q", p)
		}
	}
	safe := []string{
		"src/main.go",
		"./internal/foo/bar.go",
		"README.md",
		"docs/design.md",
		"config/app.yaml",
		"pkg/ssh/client.go",   // "ssh" in a dir name, not ~/.ssh
		"templates/bashrc.tmpl", // not a real rc file
	}
	for _, p := range safe {
		if r := CheckWritePath(p); !r.Safe {
			t.Errorf("normal project write wrongly blocked: %q (%s)", p, r.Reason)
		}
	}
}
