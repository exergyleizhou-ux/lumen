package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"lumen/internal/quota"
	"lumen/internal/science/lab"
)

func runScienceLab(args []string) {
	port := lab.DefaultPort
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
			fmt.Fprintf(os.Stderr, "unknown lab flag: %s\n", args[i])
			os.Exit(1)
		}
	}
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	var quotaStore quota.Store
	var err error
	if os.Getenv("LUMEN_HOSTED") == "true" {
		quotaStore, err = quota.NewHTTPStore(os.Getenv("WORKBENCH_CONTROL_PLANE_URL"), os.Getenv("WORKBENCH_RUNTIME_INGEST_SECRET"), nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "quota: %v\n", err)
			os.Exit(1)
		}
	}
	srv, err := lab.New(lab.Config{
		SciDir:             scienceDir(),
		LumenCfg:           lumenCfg(),
		Addr:               addr,
		Version:            resolveVersion(),
		OpenPanel:          openPanel,
		Hosted:             os.Getenv("LUMEN_HOSTED") == "true",
		WorkbenchJWTSecret: os.Getenv("WORKBENCH_JWT_SECRET"),
		Quota:              quotaStore,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "science lab: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "science lab: %v\n", err)
		os.Exit(1)
	}
}
