// Package i18n provides simple internationalization for Lumen's user-facing
// strings. Translations are stored as Go maps (no external dependency).
// The default locale is detected from the LANG environment variable.
// Adapted from Reasonix's i18n/ package.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Lang represents a locale identifier (e.g. "en", "zh-CN", "ja").
type Lang string

const (
	EN   Lang = "en"
	ZHCN Lang = "zh-CN"
	JA   Lang = "ja"
)

// DefaultLang returns the detected system locale, defaulting to English.
func DefaultLang() Lang {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	if lang == "" {
		return EN
	}
	lang = strings.Split(lang, ".")[0] // strip encoding
	switch {
	case strings.HasPrefix(lang, "zh"):
		return ZHCN
	case strings.HasPrefix(lang, "ja"):
		return JA
	default:
		return EN
	}
}

// TrFunc is a translation function. Call Tr("key") to get the localized string.
type TrFunc func(key string, args ...any) string

// Catalog holds translations for one locale.
type Catalog map[string]string

var (
	mu      sync.RWMutex
	current Lang = DefaultLang()
	dicts   map[Lang]Catalog
)

func init() {
	dicts = map[Lang]Catalog{
		EN:   enCatalog,
		ZHCN: zhCNCatalog,
		JA:   jaCatalog,
	}
}

// SetLang changes the active locale.
func SetLang(l Lang) {
	mu.Lock()
	current = l
	mu.Unlock()
}

// GetLang returns the active locale.
func GetLang() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Tr returns a translation function for the given locale.
func Tr(lang Lang) TrFunc {
	catalog, ok := dicts[lang]
	if !ok {
		catalog = enCatalog
	}
	return func(key string, args ...any) string {
		if s, ok := catalog[key]; ok {
			if len(args) > 0 {
				return strings.NewReplacer(buildReplacements(args...)...).Replace(s)
			}
			return s
		}
		return key
	}
}

// T returns the translation for the current locale.
func T(key string, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()
	return Tr(current)(key, args...)
}

func buildReplacements(args ...any) []string {
	out := make([]string, 0, len(args)*2)
	for i, a := range args {
		out = append(out, fmt.Sprintf("{%d}", i), fmt.Sprint(a))
	}
	return out
}

// ── English catalog ────────────────────────────────────────

var enCatalog = Catalog{
	"agent.running":        "Running",
	"agent.planning":       "Planning",
	"agent.executing":      "Executing",
	"agent.done":           "Done",
	"agent.error":          "Error",
	"agent.cancelled":      "Cancelled",
	"cache.hot":            "Excellent — prefix cache is hot and stable.",
	"cache.warm":           "Warning — cache is partially warm.",
	"cache.cold":           "Poor — cache is cold.",
	"permission.blocked":   "blocked by permission policy",
	"permission.plan_mode": "plan mode is read-only — approve the plan first",
	"tool.approval_needed": "⚠ Approve {0}?",
	"doctor.checking":      "Lumen doctor — checking configuration...",
	"doctor.all_ok":        "All checks passed.",
	"doctor.failed":        "Some checks failed — review above.",
	"rewind.complete":      "{0} file(s) restored.",
	"timeline.title":       "Session Timeline",
	"changes.title":        "Changed files this session",
}

// ── Simplified Chinese catalog ─────────────────────────────

var zhCNCatalog = Catalog{
	"agent.running":        "运行中",
	"agent.planning":       "规划中",
	"agent.executing":      "执行中",
	"agent.done":           "完成",
	"agent.error":          "错误",
	"agent.cancelled":      "已取消",
	"cache.hot":            "✅ 优秀 — 前缀缓存完全命中。",
	"cache.warm":           "⚠ 警告 — 缓存部分命中。",
	"cache.cold":           "❌ 差 — 缓存未命中。",
	"permission.blocked":   "权限策略已阻止",
	"permission.plan_mode": "规划模式为只读 — 请先批准计划",
	"tool.approval_needed": "⚠ 批准 {0}？",
	"doctor.checking":      "Lumen 健康检查中...",
	"doctor.all_ok":        "所有检查通过。",
	"doctor.failed":        "部分检查未通过 — 请查看上方详情。",
	"rewind.complete":      "已恢复 {0} 个文件。",
	"timeline.title":       "会话时间线",
	"changes.title":        "本次会话修改的文件",
}

// ── Japanese catalog ───────────────────────────────────────

var jaCatalog = Catalog{
	"agent.running":        "実行中",
	"agent.planning":       "計画中",
	"agent.executing":      "実行中",
	"agent.done":           "完了",
	"agent.error":          "エラー",
	"agent.cancelled":      "キャンセル",
	"cache.hot":            "✅ 優秀 — プレフィックスキャッシュが完全にヒットしています。",
	"cache.warm":           "⚠ 警告 — キャッシュが部分的にヒットしています。",
	"cache.cold":           "❌ 不良 — キャッシュがヒットしていません。",
	"permission.blocked":   "権限ポリシーによりブロックされました",
	"permission.plan_mode": "計画モードは読み取り専用です — まず計画を承認してください",
	"tool.approval_needed": "⚠ {0} を承認しますか？",
	"doctor.checking":      "Lumen 健康チェック中...",
	"doctor.all_ok":        "すべてのチェックに合格しました。",
	"doctor.failed":        "一部のチェックに失敗しました — 上記を確認してください。",
	"rewind.complete":      "{0} ファイルを復元しました。",
	"timeline.title":       "セッションタイムライン",
	"changes.title":        "このセッションで変更されたファイル",
}
