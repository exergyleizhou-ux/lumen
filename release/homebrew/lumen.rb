# Lumen Homebrew Formula
#
# Install: brew install exergyleizhou-ux/lumen/lumen
# Or:      brew tap exergyleizhou-ux/lumen && brew install lumen
#
# This formula builds Lumen from source using the official repository.
# Pre-built bottles are not yet available.

class Lumen < Formula
  desc "AI-powered coding assistant running on the Grok Build platform"
  homepage "https://github.com/exergyleizhou-ux/lumen"
  url "https://github.com/exergyleizhou-ux/lumen.git",
      tag:      "v0.1.250-macos",
      revision: "15050e3a4ef3f1c14a723a8a44b53eecae6d1b41"
  license "Apache-2.0"
  version "0.1.250"
  head "https://github.com/exergyleizhou-ux/lumen.git", branch: "main"

  depends_on "rust" => :build
  depends_on "git"

  def install
    # Build from the agent subdirectory (monorepo layout)
    cd "agent" do
      system "cargo", "install", "--path", "crates/codegen/xai-grok-pager-bin",
             "--root", prefix
    end

    # Install scripts
    bin.install Dir["scripts/lumen-*.sh"]
    bin.install Dir["scripts/check-vacuous-e2e.sh"]

    # Create symlink for convenience
    bin.install_symlink bin/"lumen" => "lm"
  end

  def caveats
    <<~EOS
      Lumen is installed! Start with:

        lumen

      Configuration files:
        ~/.lumen/config.toml  (primary, authoritative)
        ~/.grok/config.toml   (override layer)

      Set your API keys before first use:
        export DEEPSEEK_API_KEY=your-key
        export KIMI_CODE_API_KEY=your-key

      Science features ship separately (lumen-science); see docs/science.
    EOS
  end

  test do
    system "#{bin}/lumen", "--version"
  end

end
