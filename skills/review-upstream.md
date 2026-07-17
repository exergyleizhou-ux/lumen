# Lumen Upstream Review Skill

当 Grok Build 上游 (xai-org/grok-build) 有新版本时，按此流程审核。

## 流程

### Step 1: Fetch 上游
```bash
cd /Users/lei/code/lumen && git fetch upstream --no-tags
```

### Step 2: 查看上游变更
```bash
cd /Users/lei/code/lumen
# 上游最新提交
git log upstream/main -10 --oneline
# 变更文件列表
git diff --stat upstream/main^^..upstream/main
```

### Step 3: 分类审核
对每个上游变更文件，判断：

| 分类 | 处理方式 |
|------|---------|
| 我们已有的优化（功能重复）| ❌ 跳过 |
| 与我们的改动有冲突 | ⚠️ 手动合并 |
| 纯新功能、无冲突 | ✅ 吸收 |
| 安全修复 | ✅ 优先级吸收 |
| xAI 品牌/市场相关 | ❌ 跳过 |
| 模型 catalog 更新 | ⚠️ 只吸收非 xAI 部分 |

### Step 4: 冲突检测
检查上游变更是否触及我们修改过的文件：
```bash
cd /Users/lei/code/lumen
# 我们的改动文件列表
OUR_FILES=$(git diff --name-only 853a305..HEAD)
# 上游改动文件列表  
UP_FILES=$(git diff --name-only upstream/main^^..upstream/main)
# 交集 = 可能冲突
comm -12 <(echo "$OUR_FILES" | sort) <(echo "$UP_FILES" | sort)
```

### Step 5: 辩证吸收
- 上游的好功能 → 手动 patch 到对应位置
- 上游路径 `crates/codegen/xxx` → 我们路径 `agent/crates/codegen/xxx`
- 我们的 lumen-guard/discipline/verify 三层**绝不让上游覆盖**
- 品牌文件（logo, welcome text）**绝不让上游覆盖**

### Step 6: 验证
```bash
cd /Users/lei/code/lumen/agent && cargo test -p lumen-guard -p lumen-discipline -p lumen-verify 2>&1
cd /Users/lei/code/lumen && LUMEN_ALLOW_DIRTY=1 ./scripts/install-local.sh
```

### Step 7: 记录
更新 `~/.lumen/self-update-memory.json` 的 `upstream_reviews`：
```json
{
  "upstream_commit": "<hash>",
  "reviewed_at": "<ISO时间>",
  "files_changed": 42,
  "absorbed": ["file1.rs", "file2.rs"],
  "skipped": ["file3.rs - 品牌冲突", "file4.rs - 已有优化"],
  "conflicts": []
}
```

## 我们的保护区（绝不让上游覆盖）
- `agent/crates/codegen/lumen-guard/`
- `agent/crates/codegen/lumen-discipline/`
- `agent/crates/codegen/lumen-verify/`
- `agent/crates/codegen/xai-grok-pager/assets/logo/logo05.txt`
- `agent/crates/codegen/xai-grok-pager/assets/logo/logo07.txt`
- `agent/crates/codegen/xai-grok-agent/src/prompt/context.rs` (DEFAULT_SYSTEM_PROMPT_LABEL)
- `agent/crates/codegen/xai-grok-pager/src/views/welcome/hero_box.rs` (HERO_SUBTITLE)
