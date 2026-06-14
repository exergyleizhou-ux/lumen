---
name: security-review
description: Security audit — injection, auth, secrets, crypto, path traversal.
runAs: subagent
allowed-tools: read_file, grep, glob, ls, lsp_definition, lsp_references, web_fetch
---
# Security Review
Audit code for security vulnerabilities:

1. **Injection**: SQL, command, template, regex injection vectors.
2. **Authentication & Authorization**: Missing auth checks, token validation gaps.
3. **Secrets**: Hardcoded keys, passwords in logs, env var exposure.
4. **Deserialization**: Untrusted input deserialization, prototype pollution.
5. **Path traversal**: File access outside intended directories.
6. **Cryptography**: Weak algorithms, nonce reuse, timing attacks.

Tag each finding with severity: 🔴 critical / 🟠 high / 🟡 medium / 🟢 low.
Include file:line and remediation guidance.
