package rewriter

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"kamioka.example.com/fake_snmp_exporter/internal/config"
)

type counterState struct {
	currentValue float64
	lastUpdate   time.Time
}

type gaugeState struct {
	currentValue float64
	lastUpdate   time.Time
}

// Rewriter は Prometheus テキスト形式のメトリクスを書き換えます。
type Rewriter struct {
	configs  []config.RewriteConfig
	mu       sync.Mutex
	counters map[string]*counterState
	gauges   map[string]*gaugeState
}

// New は新しい Rewriter を作成します。
//
// 引数:
//
//	configs - 書き換えルールの一覧
func New(configs []config.RewriteConfig) *Rewriter {
	return &Rewriter{
		configs:  configs,
		counters: make(map[string]*counterState),
		gauges:   make(map[string]*gaugeState),
	}
}

// Rewrite は Prometheus テキスト形式のレスポンスボディを書き換えて返します。
//
// 引数:
//
//	body        - upstream から受け取ったレスポンスボディ
//	contentType - レスポンスの Content-Type ヘッダー値
//
// 戻り値:
//
//	[]byte - 書き換え後のレスポンスボディ
//	error  - パースまたはエンコード時のエラー
func (r *Rewriter) Rewrite(body []byte, contentType string) ([]byte, error) {
	header := http.Header{"Content-Type": {contentType}}
	format := expfmt.ResponseFormat(header)
	if format == expfmt.FmtUnknown {
		format = expfmt.FmtText
	}

	families, err := decodeAll(bytes.NewReader(body), format)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	r.mu.Lock()
	for _, mf := range families {
		for _, m := range mf.Metric {
			for i := range r.configs {
				cfg := &r.configs[i]
				if mf.GetName() != cfg.Metric {
					continue
				}
				if !labelsMatch(m.Label, cfg.Labels) {
					continue
				}
				key := makeKey(mf.GetName(), m.Label)
				r.applyRewrite(m, cfg, key, now)
			}
		}
	}
	r.mu.Unlock()

	return encodeAll(families, format)
}

// applyRewrite は設定の Type に応じて counter または gauge の書き換えを適用します。
func (r *Rewriter) applyRewrite(m *dto.Metric, cfg *config.RewriteConfig, key string, now time.Time) {
	switch cfg.Type {
	case "counter":
		r.applyCounter(m, cfg, key, now)
	case "gauge":
		r.applyGauge(m, cfg, key, now)
	}
}

// applyCounter は counter 型の書き換えを適用します。
// 初回は元の値を記録し、以降は rate * elapsed * jitterFactor 分を加算します。
func (r *Rewriter) applyCounter(m *dto.Metric, cfg *config.RewriteConfig, key string, now time.Time) {
	current, ok := getValue(m)
	if !ok {
		return
	}

	state, exists := r.counters[key]
	if !exists {
		r.counters[key] = &counterState{
			currentValue: current,
			lastUpdate:   now,
		}
		return
	}

	elapsed := now.Sub(state.lastUpdate).Seconds()
	if elapsed > 0 && cfg.Rate > 0 {
		jitterFactor := 1.0
		if cfg.Jitter > 0 {
			jitterFactor = 1.0 + cfg.Jitter*(2*rand.Float64()-1)
		}
		increment := elapsed * cfg.Rate * jitterFactor
		if increment < 0 {
			increment = 0
		}
		state.currentValue += increment
		state.lastUpdate = now
	}

	setValue(m, math.Round(state.currentValue))
}

// applyGauge は gauge 型の書き換えを適用します。
// min_hold 秒経過するまで現在値を保持し、経過後に max_delta 以内でランダムに変動させます。
func (r *Rewriter) applyGauge(m *dto.Metric, cfg *config.RewriteConfig, key string, now time.Time) {
	current, ok := getValue(m)
	if !ok {
		return
	}

	state, exists := r.gauges[key]
	if !exists {
		r.gauges[key] = &gaugeState{
			currentValue: current,
			lastUpdate:   now,
		}
		setValue(m, math.Round(current))
		return
	}

	elapsed := now.Sub(state.lastUpdate).Seconds()
	if elapsed < cfg.MinHold {
		setValue(m, math.Round(state.currentValue))
		return
	}

	newVal := state.currentValue
	if cfg.MaxDelta > 0 {
		delta := cfg.MaxDelta * (2*rand.Float64() - 1)
		newVal += delta
	}

	// min/max が有効なら範囲内にクランプする
	if cfg.Max > cfg.Min {
		if newVal > cfg.Max {
			newVal = cfg.Max
		}
		if newVal < cfg.Min {
			newVal = cfg.Min
		}
	}

	state.currentValue = newVal
	state.lastUpdate = now
	setValue(m, math.Round(newVal))
}

// getValue は dto.Metric から現在値を取得します。
//
// 戻り値:
//
//	float64 - 現在値
//	bool    - 値が取得できた場合 true
func getValue(m *dto.Metric) (float64, bool) {
	if m.Counter != nil {
		return m.Counter.GetValue(), true
	}
	if m.Gauge != nil {
		return m.Gauge.GetValue(), true
	}
	if m.Untyped != nil {
		return m.Untyped.GetValue(), true
	}
	return 0, false
}

// setValue は dto.Metric に値を設定します。
func setValue(m *dto.Metric, val float64) {
	if m.Counter != nil {
		m.Counter.Value = &val
	} else if m.Gauge != nil {
		m.Gauge.Value = &val
	} else if m.Untyped != nil {
		m.Untyped.Value = &val
	}
}

// labelsMatch はメトリクスのラベルセットが設定ラベルをすべて含むか判定します。
func labelsMatch(metricLabels []*dto.LabelPair, configLabels map[string]string) bool {
	for name, value := range configLabels {
		found := false
		for _, lp := range metricLabels {
			if lp.GetName() == name && lp.GetValue() == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// makeKey はメトリクス名とラベルセットからユニークなキー文字列を生成します。
func makeKey(metricName string, labels []*dto.LabelPair) string {
	pairs := make([]string, len(labels))
	for i, lp := range labels {
		pairs[i] = lp.GetName() + "=" + lp.GetValue()
	}
	sort.Strings(pairs)
	return metricName + "{" + strings.Join(pairs, ",") + "}"
}

// decodeAll は io.Reader から全 MetricFamily をデコードして返します。
func decodeAll(r io.Reader, format expfmt.Format) ([]*dto.MetricFamily, error) {
	decoder := expfmt.NewDecoder(r, format)
	var families []*dto.MetricFamily
	for {
		mf := &dto.MetricFamily{}
		if err := decoder.Decode(mf); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("メトリクスのデコードエラー: %w", err)
		}
		families = append(families, mf)
	}
	return families, nil
}

// encodeAll は MetricFamily のスライスをエンコードして返します。
func encodeAll(families []*dto.MetricFamily, format expfmt.Format) ([]byte, error) {
	var buf bytes.Buffer
	encoder := expfmt.NewEncoder(&buf, format)
	for _, mf := range families {
		if err := encoder.Encode(mf); err != nil {
			return nil, fmt.Errorf("メトリクスのエンコードエラー: %w", err)
		}
	}
	if closer, ok := encoder.(io.Closer); ok {
		closer.Close() //nolint:errcheck
	}
	return buf.Bytes(), nil
}
