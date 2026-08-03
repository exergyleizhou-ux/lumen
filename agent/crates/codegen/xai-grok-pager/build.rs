use std::{
    env, fs,
    path::{Path, PathBuf},
    process::Command,
};

fn main() {
    let repo_root = repo_root();
    emit_git_rerun_paths(&repo_root);
    println!("cargo:rerun-if-env-changed=GROK_VERSION");

    let commit = Command::new("git")
        .args([
            "-C",
            repo_root.to_str().unwrap_or("."),
            "rev-parse",
            "--short",
            "HEAD",
        ])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .and_then(|o| String::from_utf8(o.stdout).ok())
        .map(|s| s.trim().to_string())
        .unwrap_or_else(|| "unknown".to_string());

    let version = std::env::var("GROK_VERSION")
        .or_else(|_| std::env::var("CARGO_PKG_VERSION"))
        .unwrap_or_else(|_| "0.0.0".to_string());

    println!(
        "cargo:rustc-env=VERSION_WITH_COMMIT={} ({})",
        version, commit
    );
}

fn repo_root() -> PathBuf {
    PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR is set"))
        .ancestors()
        .nth(4)
        .expect("pager manifest is nested below repository root")
        .to_path_buf()
}

fn emit_git_rerun_paths(repo_root: &Path) {
    let marker = repo_root.join(".git");
    println!("cargo:rerun-if-changed={}", marker.display());

    let git_dir = match fs::read_to_string(&marker) {
        Ok(contents) => contents
            .strip_prefix("gitdir: ")
            .map(str::trim)
            .map(PathBuf::from)
            .map(|path| {
                if path.is_absolute() {
                    path
                } else {
                    marker.parent().expect(".git has a parent").join(path)
                }
            })
            .unwrap_or(marker),
        Err(_) => marker,
    };
    let head = git_dir.join("HEAD");
    println!("cargo:rerun-if-changed={}", head.display());
    if let Ok(contents) = fs::read_to_string(&head)
        && let Some(reference) = contents.strip_prefix("ref: ")
    {
        println!(
            "cargo:rerun-if-changed={}",
            git_dir.join(reference.trim()).display()
        );
    }
    println!(
        "cargo:rerun-if-changed={}",
        git_dir.join("packed-refs").display()
    );
}
