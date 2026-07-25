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
      revision: "2fb3271e7a0f3c0f5e1d8a9b4c6e7f8a9b0c1d2e"
  license "Apache-2.0"
  version "0.1.250"
  head "https://github.com/exergyleizhou-ux/lumen.git", branch: "main"

  depends_on "rust" => :build
  depends_on "git"
  depends_on "make"

  # Optional: for science features
  depends_on "python@3.11" => :optional
  depends_on "tesseract" => :optional

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

      For science features, install with --with-python@3.11.
    EOS
  end

  test do
    system "#{bin}/lumen", "--version"
  end

  # Service support (macOS LaunchAgent)
  service do
    run [opt_bin/"lumen-science", "serve"]
    keep_alive true
    log_path var/"log/lumen-science.log"
    error_log_path var/"log/lumen-science.err.log"
    working_dir var/"lumen"
    environment_variables PATH: std_service_path_env
  end
end
