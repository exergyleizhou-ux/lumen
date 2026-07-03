package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lumen/internal/config"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/gui"
	"lumen/internal/science/guard"
	"lumen/internal/science/migrate"
	"lumen/internal/science/paths"
	"lumen/internal/science/proxy"
	"lumen/internal/science/runtime"
)

func runScience(args []string) {
	if len(args) == 0 {
		printScienceUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "proxy":
		runScienceProxy(args[1:])
	case "start":
		runScienceStart(args[1:])
	case "stop":
		runScienceStop(args[1:])
	case "status":
		runScienceStatus()
	case "doctor":
		runScienceDoctor()
	case "verify":
		runScienceVerify()
	case "logs":
		runScienceLogs(args[1:])
	case "watch":
		runScienceWatch()
	case "mode":
		runScienceMode(args[1:])
	case "official":
		runScienceOfficial()
	case "config":
		runScienceConfig(args[1:])
	case "bio", "research":
		runScienceResearch(args[1:])
	case "gui":
		runScienceGUI(args[1:])
	case "migrate":
		runScienceMigrate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown science subcommand: %s\n", args[0])
		printScienceUsage()
		os.Exit(1)
	}
}

func printScienceUsage() {
	fmt.Print(`lumen science — Claude Science third-party model bridge

Usage:
  lumen science start [--provider P] [--no-browser]   一键：代理 + 沙箱 + 浏览器
  lumen science stop [--all]                          停沙箱（默认）；--all 同时停代理
  lumen science status                              代理/沙箱/缓存命中率
  lumen science verify                              验证 API key 是否可用
  lumen science doctor                              环境诊断（只读）
  lumen science logs [proxy|sandbox]                查看日志尾部
  lumen science watch                               实时 DeepSeek 缓存命中率
  lumen science mode official|proxy                 切换官方/第三方模式
  lumen science official                            打开官方 Claude Science
  lumen science proxy [flags]                       仅启动代理
  lumen science research list|verify|reseed        全部科研 MCP/数据库/技能清单与体检
  lumen science gui [--port N] [--no-browser]    Grok Build 风格控制面板（默认 :18990）
  lumen science migrate [--force]                从 CSswitch 导入配置与 API key
  lumen science config show|set-provider|set-key|set-port|set-cache-boost

Proxy flags:
  --provider <name>   deepseek | qwen | moonshot | zhipu
  --port <n>          listen port (default 18991)
  --addr <host:port>  full listen address
  --auth-secret <s>   path-prefix secret (auto-generated if omitted)
  --api-key <key>     upstream API key
  --upstream-url <u>  override upstream endpoint
  --log <path>        log file path

Config lives in ~/.lumen/science/config.json (0600). API keys also read from lumen.toml providers.
`)
}

func scienceDir() string {
	dir, err := sciconfig.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "science: %v\n", err)
		os.Exit(1)
	}
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func lumenCfg() *config.File {
	cfg, err := config.LoadWithEnv(config.FindConfig(), config.FindDotEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func runScienceStart(args []string) {
	provider := ""
	openBrowser := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				fatalScienceFlag("--provider")
			}
			provider = args[i+1]
			i++
		case "--no-browser":
			openBrowser = false
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			os.Exit(1)
		}
	}
	dir := scienceDir()
	if provider != "" {
		if err := runtime.SetProvider(dir, provider); err != nil {
			fmt.Fprintf(os.Stderr, "science start: %v\n", err)
			os.Exit(1)
		}
	}
	mgr, err := runtime.New(dir, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science start: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.RunForeground(openBrowser); err != nil {
		fmt.Fprintf(os.Stderr, "science start: %v\n", err)
		os.Exit(1)
	}
}

func runScienceStop(args []string) {
	stopAll := false
	for _, a := range args {
		if a == "--all" {
			stopAll = true
		}
	}
	dir := scienceDir()
	mgr, err := runtime.New(dir, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science stop: %v\n", err)
		os.Exit(1)
	}
	var err2 error
	if stopAll {
		err2 = mgr.StopAll()
		fmt.Println("已停止沙箱与代理。真实实例（8765）未受影响。")
	} else {
		err2 = mgr.StopSandboxOnly()
		fmt.Println("已停止沙箱。代理若在其它终端运行需自行结束。")
	}
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "science stop: %v\n", err2)
		os.Exit(1)
	}
}

func runScienceStatus() {
	dir := scienceDir()
	mgr, err := runtime.New(dir, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science status: %v\n", err)
		os.Exit(1)
	}
	st := mgr.Status()
	cfg, _ := sciconfig.Load(dir)
	fmt.Println("Science 桥接状态")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("  线路:      %v (%v)\n", st["provider"], st["mode"])
	fmt.Printf("  代理:      %s :%v\n", statusDot(st["proxy_healthy"]), st["proxy_port"])
	fmt.Printf("  沙箱:      %s :%v\n", statusDot(st["sandbox_running"]), st["sandbox_port"])
	fmt.Printf("  上游:      %s\n", statusDot(st["upstream_reachable"]))
	if cfg.CacheBoostEnabled() {
		fmt.Printf("  缓存增强:  开启 (system/tools ephemeral)\n")
	}
	if pct, ok := st["cache_session_hit_pct"].(int64); ok {
		fmt.Printf("  会话命中:  %d%%\n", pct)
	}
	if pct, ok := st["cache_last_hit_pct"].(int64); ok {
		fmt.Printf("  上次命中:  %d%%\n", pct)
	}
	if hits, ok := st["cache_hit_tokens"].(int64); ok && hits > 0 {
		fmt.Printf("  命中 token: %d\n", hits)
	}
	if u, _ := st["url"].(string); u != "" {
		fmt.Printf("  访问:      %s\n", u)
	}
}

func statusDot(v any) string {
	if b, ok := v.(bool); ok && b {
		return "● 正常"
	}
	return "○ 未运行"
}

func runScienceResearch(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	dir := scienceDir()
	switch sub {
	case "list":
		cat := runtime.ListResearchCatalog(dir)
		data, _ := json.MarshalIndent(cat, "", "  ")
		fmt.Println(string(data))
	case "verify":
		rep, err := runtime.ResearchReport(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "science research verify: %v\n", err)
			fmt.Println("提示: 先执行 lumen science start，从 ~/.claude-science 克隆全部 runtime 资产。")
			os.Exit(1)
		}
		fmt.Printf("runtime:       %s\n", rep.RuntimeVersion)
		fmt.Printf("clone assets:  %s\n", strings.Join(rep.CloneAssets, ", "))
		fmt.Printf("mcp servers:   %d top-level\n", len(rep.MCPServers))
		fmt.Printf("bio-tools:     %d clients, %d domain MCP servers\n", rep.BioLibPackages, rep.DomainMCPServers)
		fmt.Printf("domains:       %d domains, %d tools\n", len(rep.Domains), rep.TotalDomainTools)
		fmt.Printf("skills:        %d\n", len(rep.Skills))
		if len(rep.SeedExamples) > 0 {
			fmt.Printf("seed examples: %s\n", strings.Join(rep.SeedExamples, ", "))
		}
		fmt.Printf("org pack:      %v (%d workspaces)\n", rep.OrgPackSeeded, rep.Workspaces)
		if len(rep.MissingSkills) > 0 {
			fmt.Printf("missing:       %s\n", strings.Join(rep.MissingSkills, ", "))
		}
		if rep.Healthy() {
			fmt.Println("✓ 全部科研资源包就绪（本地 MCP 替代 Anthropic 托管远程服务）")
		} else {
			fmt.Println("✗ 资源不完整 — 请确认已安装 Claude Science 并执行 start")
			os.Exit(1)
		}
	case "reseed":
		if err := runtime.ReseedResearchPack(dir); err != nil {
			fmt.Fprintf(os.Stderr, "science research reseed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("已重新播种 org 工作区与全部 bundled MCP 自动批准（不覆盖已有 preferences.json）。")
	default:
		fmt.Fprintln(os.Stderr, "usage: lumen science research list|verify|reseed")
		os.Exit(1)
	}
}

func runScienceGUI(args []string) {
	port := gui.DefaultPort
	addr := ""
	openPanel := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 >= len(args) {
				fatalScienceFlag("--port")
			}
			p, err := strconv.Atoi(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid port: %v\n", err)
				os.Exit(1)
			}
			port = p
			i++
		case "--addr":
			if i+1 >= len(args) {
				fatalScienceFlag("--addr")
			}
			addr = args[i+1]
			i++
		case "--no-browser":
			openPanel = false
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			os.Exit(1)
		}
	}
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	dir := scienceDir()
	srv, err := gui.New(gui.Config{
		SciDir:    dir,
		LumenCfg:  lumenCfg(),
		Addr:      addr,
		Version:   version,
		OpenPanel: openPanel,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "science gui: %v\n", err)
		os.Exit(1)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		fmt.Fprintln(os.Stderr, "\nscience gui: 已退出")
		os.Exit(0)
	}()
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "science gui: %v\n", err)
		os.Exit(1)
	}
}

func runScienceMigrate(args []string) {
	force := false
	for _, a := range args {
		if a == "--force" {
			force = true
		} else if strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
			os.Exit(1)
		}
	}
	path, exists, busy := migrate.Detect()
	if !exists {
		fmt.Fprintln(os.Stderr, "未找到 CSswitch 配置 (~/.csswitch/config.json)")
		os.Exit(1)
	}
	if busy && !force {
		fmt.Fprintf(os.Stderr, "CSswitch 端口仍占用 — 请先停止 CSswitch，或使用 --force\n")
		os.Exit(1)
	}
	dir := scienceDir()
	rep, err := migrate.Import(dir, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "science migrate: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已从 CSswitch 导入配置\n")
	fmt.Printf("  来源:   %s\n", path)
	if rep.Provider != "" {
		fmt.Printf("  线路:   %s\n", rep.Provider)
	}
	if rep.ProxyPort > 0 {
		fmt.Printf("  代理:   %d\n", rep.ProxyPort)
	}
	if rep.SandboxPort > 0 {
		fmt.Printf("  沙箱:   %d\n", rep.SandboxPort)
	}
	if rep.Mode != "" {
		fmt.Printf("  模式:   %s\n", rep.Mode)
	}
	if rep.SecretImported {
		fmt.Println("  secret: 已导入")
	}
	if len(rep.KeysImported) > 0 {
		fmt.Printf("  keys:   %s\n", strings.Join(rep.KeysImported, ", "))
	}
	fmt.Println("下一步: lumen science start  或  lumen science gui")
}

func runScienceWatch() {
	dir := scienceDir()
	mgr, err := runtime.New(dir, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science watch: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.WatchCache(2 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "science watch: %v\n", err)
		os.Exit(1)
	}
}

func runScienceVerify() {
	dir := scienceDir()
	mgr, err := runtime.New(dir, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science verify: %v\n", err)
		os.Exit(1)
	}
	ok, hint, err := mgr.VerifyKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "science verify: %v\n", err)
		os.Exit(1)
	}
	if ok {
		fmt.Printf("✓ %s\n", hint)
		return
	}
	fmt.Printf("✗ %s\n", hint)
	os.Exit(1)
}

func runScienceLogs(args []string) {
	which := "proxy"
	if len(args) > 0 {
		which = args[0]
	}
	dir := scienceDir()
	var path string
	switch which {
	case "proxy":
		path = paths.ProxyLog(dir)
	case "sandbox":
		path = paths.SandboxLog(dir)
	default:
		fmt.Fprintf(os.Stderr, "usage: lumen science logs [proxy|sandbox]\n")
		os.Exit(1)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "science logs: %v\n", err)
		os.Exit(1)
	}
	if len(data) > 4000 {
		data = data[len(data)-4000:]
	}
	fmt.Print(string(data))
}

func runScienceMode(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: lumen science mode official|proxy")
		os.Exit(1)
	}
	dir := scienceDir()
	switch args[0] {
	case "official":
		if err := runtime.SwitchToOfficial(dir, lumenCfg()); err != nil {
			fmt.Fprintf(os.Stderr, "science mode: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("已切换到官方 Claude Science（已停桥接，打开真实客户端）。")
	case "proxy":
		if err := runtime.SetMode(dir, "proxy"); err != nil {
			fmt.Fprintf(os.Stderr, "science mode: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("已切换到第三方模型模式。执行: lumen science start")
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", args[0])
		os.Exit(1)
	}
}

func runScienceDoctor() {
	dir := scienceDir()
	results, warns, fails := runtime.RunDoctor(dir, lumenCfg())
	fmt.Println("lumen science doctor (read-only)")
	for _, r := range results {
		switch r.Level {
		case "pass":
			fmt.Printf("  ✓ %s\n", r.Message)
		case "warn":
			fmt.Printf("  ⚠ %s\n", r.Message)
		case "fail":
			fmt.Printf("  ✗ %s\n", r.Message)
		}
	}
	fmt.Printf("----\nwarnings %d, failures %d\n", warns, fails)
	if fails > 0 {
		os.Exit(1)
	}
}

func runScienceOfficial() {
	if err := runtime.SwitchToOfficial(scienceDir(), lumenCfg()); err != nil {
		fmt.Fprintf(os.Stderr, "science official: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("已打开官方 Claude Science（不经代理，使用真实订阅）。")
}

func runScienceConfig(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lumen science config show|set-provider|set-key")
		os.Exit(1)
	}
	dir := scienceDir()
	switch args[0] {
	case "show":
		cfg, err := sciconfig.Load(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("provider:     %s\n", cfg.Provider)
		fmt.Printf("mode:         %s\n", cfg.Mode)
		fmt.Printf("proxy_port:   %d\n", cfg.ProxyPort)
		fmt.Printf("sandbox_port: %d\n", cfg.SandboxPort)
		if cfg.CacheBoostEnabled() {
			fmt.Printf("cache_boost:  on\n")
		} else {
			fmt.Printf("cache_boost:  off\n")
		}
		if cfg.Secret != "" {
			fmt.Printf("secret:       %s…\n", cfg.Secret[:scienceMin(8, len(cfg.Secret))])
		}
		for name, p := range cfg.Providers {
			if p.Key != "" {
				fmt.Printf("key[%s]:      %s\n", name, sciconfig.MaskKey(p.Key))
			}
		}
	case "set-provider":
		if len(args) < 2 {
			fatalScienceFlag("set-provider <name>")
		}
		if err := runtime.SetProvider(dir, args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("provider set to %s\n", args[1])
	case "set-key":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: lumen science config set-key <provider> <key>")
			os.Exit(1)
		}
		if err := runtime.SaveProviderKey(dir, args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("key saved for %s (%s)\n", args[1], sciconfig.MaskKey(args[2]))
		mgr, _ := runtime.New(dir, lumenCfg())
		if mgr != nil {
			if ok, hint, err := mgr.VerifyKey(); err == nil {
				if ok {
					fmt.Printf("✓ verify: %s\n", hint)
				} else {
					fmt.Printf("⚠ verify: %s\n", hint)
				}
			}
		}
	case "set-port":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: lumen science config set-port <proxy|sandbox> <port>")
			os.Exit(1)
		}
		port, err := strconv.Atoi(args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid port: %v\n", err)
			os.Exit(1)
		}
		if err := guard.AssertPortSafe(port); err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		target := args[1]
		if target != "proxy" && target != "sandbox" {
			fmt.Fprintln(os.Stderr, "port target must be proxy or sandbox")
			os.Exit(1)
		}
		if _, err := sciconfig.Update(dir, func(c *sciconfig.File) {
			if target == "proxy" {
				c.ProxyPort = port
			} else {
				c.SandboxPort = port
			}
		}); err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("port %s set to %d\n", args[1], port)
	case "set-cache-boost":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: lumen science config set-cache-boost on|off")
			os.Exit(1)
		}
		on := args[1] == "on" || args[1] == "true" || args[1] == "1"
		if args[1] == "off" || args[1] == "false" || args[1] == "0" {
			on = false
		} else if args[1] != "on" && args[1] != "true" && args[1] != "1" {
			fmt.Fprintln(os.Stderr, "usage: lumen science config set-cache-boost on|off")
			os.Exit(1)
		}
		v := on
		if _, err := sciconfig.Update(dir, func(c *sciconfig.File) { c.CacheBoost = &v }); err != nil {
			fmt.Fprintf(os.Stderr, "science config: %v\n", err)
			os.Exit(1)
		}
		if on {
			fmt.Println("缓存增强已开启（system/tools 注入 ephemeral cache_control）")
		} else {
			fmt.Println("缓存增强已关闭")
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runScienceProxy(args []string) {
	provider := "deepseek"
	port := 18991
	addr := ""
	authSecret := ""
	apiKey := ""
	upstreamURL := ""
	logPath := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				fatalScienceFlag("--provider")
			}
			provider = args[i+1]
			i++
		case "--port":
			if i+1 >= len(args) {
				fatalScienceFlag("--port")
			}
			p, _ := strconv.Atoi(args[i+1])
			port = p
			i++
		case "--addr":
			if i+1 >= len(args) {
				fatalScienceFlag("--addr")
			}
			addr = args[i+1]
			i++
		case "--auth-secret":
			if i+1 >= len(args) {
				fatalScienceFlag("--auth-secret")
			}
			authSecret = args[i+1]
			i++
		case "--api-key":
			if i+1 >= len(args) {
				fatalScienceFlag("--api-key")
			}
			apiKey = args[i+1]
			i++
		case "--upstream-url":
			if i+1 >= len(args) {
				fatalScienceFlag("--upstream-url")
			}
			upstreamURL = args[i+1]
			i++
		case "--log":
			if i+1 >= len(args) {
				fatalScienceFlag("--log")
			}
			logPath = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			printScienceUsage()
			os.Exit(1)
		}
	}

	dir := scienceDir()
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	if authSecret == "" {
		cfg, _ := sciconfig.Load(dir)
		if cfg.Secret != "" {
			authSecret = cfg.Secret
		} else {
			var err error
			authSecret, err = proxy.GenerateAuthSecret()
			if err != nil {
				fmt.Fprintf(os.Stderr, "science proxy: %v\n", err)
				os.Exit(1)
			}
			_, _ = sciconfig.Update(dir, func(c *sciconfig.File) {
				c.Secret = authSecret
				c.Provider = provider
				c.ProxyPort = port
			})
		}
	}
	if logPath == "" {
		logPath = paths.ProxyLog(dir)
	}

	cfg, err := proxy.BuildConfig(provider, apiKey, addr, authSecret, upstreamURL, logPath, lumenCfg())
	if err != nil {
		fmt.Fprintf(os.Stderr, "science proxy: %v\n", err)
		os.Exit(1)
	}
	sciCfg, _ := sciconfig.Load(dir)
	cfg.CacheBoost = sciCfg.CacheBoostEnabled()
	srv, err := proxy.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "science proxy: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Science proxy ready\n")
	fmt.Printf("  listen:   http://%s/%s\n", cfg.Addr, authSecret)
	fmt.Printf("  provider: %s\n", provider)
	fmt.Printf("  upstream: %s\n", cfg.Provider.URL)
	fmt.Printf("  log:      %s\n", logPath)
	fmt.Printf("\nANTHROPIC_BASE_URL=http://%s/%s\n\n", cfg.Addr, authSecret)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\nscience proxy: shutting down")
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "science proxy: %v\n", err)
		os.Exit(1)
	}
}

func fatalScienceFlag(name string) {
	fmt.Fprintf(os.Stderr, "science: %s requires a value\n", name)
	os.Exit(1)
}

func scienceMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}