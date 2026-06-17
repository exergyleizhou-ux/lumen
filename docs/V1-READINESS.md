# Lumen v1.0 Readiness Checklist

> 2026-06-17 · Phase 3「入轨」— v1.0 发布前最终验证

## 出厂验收 (15/15 ✅)

- [x] go build / vet / test -race 常绿
- [x] verify-after-edit 全链路 (Go + Python + JS)
- [x] LSP 诊断喂模型 (gopls → verify → FormatFeedback)
- [x] TUI 多面板 + 状态条 + verify 实时显示
- [x] 行编辑 47 tests (CJK/emoji/多行/回删/Ctrl+K/Ctrl+W)
- [x] lumen chat + tui + run + oasis
- [x] 首次 5 分钟跑通
- [x] dogfood ≥4 轮 (Ctrl+K, config, stats, multi-lang tests)
- [x] C2D Docker runner (--network none, digest pinned, Ed25519)
- [x] 回归 fixture 自动积累 (RegressionStore)
- [x] 故障归零 (2x verify 失败 → git checkout)
- [x] 密钥轮换 (泄漏 key 已从 shell 清除)
- [x] 会话持久化 (JSONL + 自动恢复)
- [x] 月可靠性报告 (lumen reliability)
- [x] Marketplace compute 全链路 (algo→job→runner→attest)

## 发布前必须 (3 项)

- [ ] 3 个 C2D 算法在 marketplace 真 Docker 执行并出凭证
- [ ] CI 门 (GitHub Actions: go build + vet + test -race)
- [ ] CHANGELOG.md (v0.1 → v1.0 变更摘要)

## 发布后推进 (Phase 3 未做项)

- [ ] 包数 53 → 30 (结构性合并)
- [ ] 外部用户首次使用 ≤5 分钟验证
- [ ] 连续 30 天可靠性面板全绿
- [ ] 至少 1 次外部用户购买 C2D 算法执行
