package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

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

	return &cfg, nil
}
