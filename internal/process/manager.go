package process

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kamioka.example.com/fake_snmp_exporter/internal/config"
)

// Manager は起動した upstream snmp_exporter プロセスの情報を保持します。
type Manager struct {
	url string
}

// Start は upstream snmp_exporter を子プロセスとして起動します。
// コンテキストがキャンセルされると、子プロセスも終了します。
//
// 引数:
//
//	ctx        - プロセスのライフタイムを制御するコンテキスト
//	cfg        - upstream の設定（バイナリパス、ポート番号など）
//	parentArgs - 親プロセスに渡された引数（--web.listen-address を除いて転送）
//
// 戻り値:
//
//	*Manager - 起動したプロセスの情報
//	error    - 起動失敗時のエラー
func Start(ctx context.Context, cfg config.UpstreamConfig, parentArgs []string) (*Manager, error) {
	port := cfg.Port
	if port == 0 {
		port = 9117
	}

	binary := expandPath(cfg.Binary)
	if binary == "" {
		binary = "snmp_exporter"
	}

	// --web.listen-address を除いて、upstream 用のポートを追加する
	args := filterListenAddress(parentArgs)
	args = expandArgsHomedir(args)
	args = append(args, fmt.Sprintf("--web.listen-address=:%d", port))

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s の起動に失敗しました: %w", binary, err)
	}

	log.Printf("upstream snmp_exporter を起動しました (PID: %d, port: %d)", cmd.Process.Pid, port)

	// プロセスの終了を監視する
	go func() {
		if err := cmd.Wait(); err != nil {
			if ctx.Err() == nil {
				log.Printf("upstream snmp_exporter が予期せず終了しました: %v", err)
			}
		}
	}()

	// snmp_exporter の起動完了を待つ
	time.Sleep(500 * time.Millisecond)

	return &Manager{
		url: fmt.Sprintf("http://localhost:%d", port),
	}, nil
}

// URL は upstream snmp_exporter の URL を返します。
func (m *Manager) URL() string {
	return m.url
}

// expandArgsHomedir は引数リスト中の値部分に含まれる ~ をホームディレクトリに展開します。
// "--flag=~/path" 形式は = 以降を、それ以外はそのまま expandPath で展開します。
func expandArgsHomedir(args []string) []string {
	result := make([]string, len(args))
	for i, arg := range args {
		if idx := strings.IndexByte(arg, '='); idx >= 0 {
			result[i] = arg[:idx+1] + expandPath(arg[idx+1:])
		} else {
			result[i] = expandPath(arg)
		}
	}
	return result
}

// expandPath は先頭の ~ をホームディレクトリに展開します。
func expandPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// filterListenAddress は引数リストから --web.listen-address 関連の引数を除去します。
func filterListenAddress(args []string) []string {
	result := make([]string, 0, len(args))
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		// "--web.listen-address=VALUE" 形式
		if strings.HasPrefix(arg, "--web.listen-address=") ||
			strings.HasPrefix(arg, "-web.listen-address=") {
			continue
		}
		// "--web.listen-address VALUE" 形式（次の引数が値）
		if arg == "--web.listen-address" || arg == "-web.listen-address" {
			skipNext = true
			continue
		}
		result = append(result, arg)
	}
	return result
}
