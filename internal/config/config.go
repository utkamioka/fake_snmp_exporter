package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration は time.Duration を YAML 文字列形式（例: "500ms", "2s"）でデシリアライズできる型です。
type Duration struct {
	time.Duration
}

// UnmarshalYAML は "500ms" や "2s" のような文字列を time.Duration にパースします。
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("時間のパースエラー %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// Config は fake_snmp_exporter.yml の全設定を保持します。
type Config struct {
	Upstream UpstreamConfig  `yaml:"upstream"`
	Rewrites []RewriteConfig `yaml:"rewrites"`
}

// UpstreamConfig は upstream snmp_exporter の接続設定です。
type UpstreamConfig struct {
	// Manage が true の場合、snmp_exporter を子プロセスとして起動します。
	Manage bool `yaml:"manage"`
	// Binary は manage: true 時の snmp_exporter 実行ファイルのパスです。
	Binary string `yaml:"binary"`
	// Host は upstream snmp_exporter のホスト名です（manage: false 時）。
	Host string `yaml:"host"`
	// Port は upstream snmp_exporter のポート番号です。
	Port int `yaml:"port"`
	// StartupTimeout は manage: true 時に snmp_exporter の起動完了を待つ時間です（デフォルト: "500ms"）。
	StartupTimeout Duration `yaml:"startup_timeout"`
}

// URL は upstream snmp_exporter の URL を返します。
func (u *UpstreamConfig) URL() string {
	host := u.Host
	if host == "" {
		host = "localhost"
	}
	port := u.Port
	if port == 0 {
		port = 9117
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// RewriteConfig は1つのメトリクス書き換えルールを定義します。
type RewriteConfig struct {
	// Metric は書き換え対象のメトリクス名です。
	Metric string `yaml:"metric"`
	// Target は書き換えを適用する SNMP 機器の target 文字列です（省略時は全機器に適用）。
	// curl の target= パラメータと同じ形式（例: "10.118.65.181:1161"）。
	Target string `yaml:"target"`
	// Labels は絞り込みに使うラベルの条件マップです（すべてが一致した場合のみ書き換えます）。
	Labels map[string]string `yaml:"labels"`
	// Type は書き換え挙動の種別です（"counter" または "gauge"）。
	Type string `yaml:"type"`

	// counter 向け設定
	//
	// Rate は1秒あたりの増分（例: 1000 = 毎秒1000増加）です。
	Rate float64 `yaml:"rate"`
	// Jitter はランダム性の割合（0.0〜1.0、例: 0.3 = ±30%）です。
	Jitter float64 `yaml:"jitter"`

	// gauge 向け設定
	//
	// Min は gauge 値の下限です。
	Min float64 `yaml:"min"`
	// Max は gauge 値の上限です。
	Max float64 `yaml:"max"`
	// MaxDelta は1回の変動で許容される最大変化量です。
	MaxDelta float64 `yaml:"max_delta"`
	// MinHold は値を維持する最低保持時間（秒）です。
	MinHold float64 `yaml:"min_hold"`
}

// Load は指定したパスの YAML 設定ファイルを読み込んで返します。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("設定ファイルの読み込みエラー: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルのパースエラー: %w", err)
	}

	if cfg.Upstream.Port == 0 {
		cfg.Upstream.Port = 9117
	}
	if cfg.Upstream.StartupTimeout.Duration == 0 {
		cfg.Upstream.StartupTimeout.Duration = 500 * time.Millisecond
	}

	return &cfg, nil
}
