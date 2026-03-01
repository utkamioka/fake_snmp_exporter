package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"kamioka.example.com/fake_snmp_exporter/internal/config"
	"kamioka.example.com/fake_snmp_exporter/internal/process"
	"kamioka.example.com/fake_snmp_exporter/internal/proxy"
	"kamioka.example.com/fake_snmp_exporter/internal/rewriter"
)

// stringSliceFlag は複数回指定可能な文字列フラグです。
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// boolFlagNames は --[no-]X 形式をサポートするブールフラグ名の一覧です。
var boolFlagNames = []string{
	"snmp.wrap-large-counters",
	"dry-run",
	"snmp.debug-packets",
	"config.expand-environment-variables",
	"web.systemd-socket",
	"version",
}

// preprocessArgs は --no-X 形式の引数を --X=false に変換します。
func preprocessArgs(args []string) []string {
	boolSet := make(map[string]bool, len(boolFlagNames))
	for _, name := range boolFlagNames {
		boolSet[name] = true
	}

	result := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "--no-") {
			name := arg[5:]
			if boolSet[name] {
				result = append(result, "--"+name+"=false")
				continue
			}
		}
		result = append(result, arg)
	}
	return result
}

// findConfigFile は設定ファイルのパスを探索します。
//
// 探索順序:
//  1. 環境変数 FAKE_SNMP_EXPORTER_CONFIG
//  2. 実行ファイルと同じディレクトリの fake_snmp_exporter.yml
//  3. カレントディレクトリの fake_snmp_exporter.yml
func findConfigFile() string {
	if path := os.Getenv("FAKE_SNMP_EXPORTER_CONFIG"); path != "" {
		return path
	}
	if exe, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(exe), "fake_snmp_exporter.yml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if _, err := os.Stat("fake_snmp_exporter.yml"); err == nil {
		return "fake_snmp_exporter.yml"
	}
	return ""
}

func main() {
	var (
		listenAddresses stringSliceFlag
		configFiles     stringSliceFlag
		wrapLargeCounters bool
		dryRun            bool
		moduleConcurrency int
		debugPackets      bool
		expandEnvVars     bool
		telemetryPath     string
		systemdSocket     bool
		webConfigFile     string
		logLevel          string
		logFormat         string
		showVersion       bool
		sourceAddress     string
	)

	// snmp_exporter と同じフラグを定義する（ほとんどは無視するが、パースエラーを防ぐ）
	flag.Var(&listenAddresses, "web.listen-address", "Addresses on which to expose metrics.")
	flag.Var(&configFiles, "config.file", "Path to configuration file.")
	flag.BoolVar(&wrapLargeCounters, "snmp.wrap-large-counters", false, "Wrap 64-bit counters.")
	flag.StringVar(&sourceAddress, "snmp.source-address", "", "Source address for SNMP.")
	flag.BoolVar(&dryRun, "dry-run", false, "Only verify configuration.")
	flag.IntVar(&moduleConcurrency, "snmp.module-concurrency", 1, "Concurrent modules per scrape.")
	flag.BoolVar(&debugPackets, "snmp.debug-packets", false, "Debug SNMP packets.")
	flag.BoolVar(&expandEnvVars, "config.expand-environment-variables", false, "Expand environment variables.")
	flag.StringVar(&telemetryPath, "web.telemetry-path", "/metrics", "Telemetry path.")
	flag.BoolVar(&systemdSocket, "web.systemd-socket", false, "Use systemd socket.")
	flag.StringVar(&webConfigFile, "web.config.file", "", "Web config file.")
	flag.StringVar(&logLevel, "log.level", "info", "Log level.")
	flag.StringVar(&logFormat, "log.format", "logfmt", "Log format.")
	flag.BoolVar(&showVersion, "version", false, "Show version.")

	processedArgs := preprocessArgs(os.Args[1:])
	if err := flag.CommandLine.Parse(processedArgs); err != nil {
		log.Fatalf("フラグのパースに失敗しました: %v", err)
	}

	// 未使用変数の参照（コンパイルエラー回避）
	_ = wrapLargeCounters
	_ = dryRun
	_ = moduleConcurrency
	_ = debugPackets
	_ = expandEnvVars
	_ = telemetryPath
	_ = systemdSocket
	_ = webConfigFile
	_ = logLevel
	_ = logFormat
	_ = sourceAddress
	_ = configFiles

	if showVersion {
		fmt.Println("fake_snmp_exporter, version dev (https://github.com/prometheus/snmp_exporter compatible)")
		return
	}

	configPath := findConfigFile()
	if configPath == "" {
		log.Fatal("設定ファイルが見つかりません。FAKE_SNMP_EXPORTER_CONFIG 環境変数を設定するか、実行ファイルと同じディレクトリに fake_snmp_exporter.yml を配置してください。")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("設定ファイルの読み込みに失敗しました: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	sigs := append([]os.Signal{os.Interrupt}, additionalSignals()...)
	signal.Notify(sigCh, sigs...)
	go func() {
		<-sigCh
		log.Println("シャットダウンします...")
		cancel()
	}()

	var upstreamURL string
	if cfg.Upstream.Manage {
		mgr, err := process.Start(ctx, cfg.Upstream, os.Args[1:])
		if err != nil {
			log.Fatalf("upstream snmp_exporter の起動に失敗しました: %v", err)
		}
		upstreamURL = mgr.URL()
	} else {
		upstreamURL = cfg.Upstream.URL()
	}

	listenAddr := ":9116"
	if len(listenAddresses) > 0 {
		listenAddr = listenAddresses[0]
	}

	rw := rewriter.New(cfg.Rewrites)
	handler := proxy.New(upstreamURL, rw)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	log.Printf("fake_snmp_exporter を %s で起動しました。upstream: %s", listenAddr, upstreamURL)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP サーバーエラー: %v", err)
	}
}
