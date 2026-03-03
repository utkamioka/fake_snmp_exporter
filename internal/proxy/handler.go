package proxy

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"kamioka.example.com/fake_snmp_exporter/internal/rewriter"
)

// hopByHopHeaders はプロキシ転送時に除去すべきホップバイホップヘッダーの一覧です。
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// Handler は upstream snmp_exporter へのリバースプロキシとして動作します。
// /snmp エンドポイントへのレスポンスはメトリクス書き換えを適用します。
type Handler struct {
	upstreamBase string
	rw           *rewriter.Rewriter
	client       *http.Client
}

// New は新しい Handler を作成します。
//
// 引数:
//
//	upstreamBase - upstream snmp_exporter のベース URL（例: "http://localhost:9117"）
//	rw           - メトリクス書き換え処理を担う Rewriter
func New(upstreamBase string, rw *rewriter.Rewriter) *Handler {
	return &Handler{
		upstreamBase: strings.TrimRight(upstreamBase, "/"),
		rw:           rw,
		client:       &http.Client{},
	}
}

// ServeHTTP はリクエストを upstream に転送し、/snmp エンドポイントのレスポンスを書き換えます。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upstreamURL, err := url.Parse(h.upstreamBase)
	if err != nil {
		http.Error(w, "内部エラー", http.StatusInternalServerError)
		return
	}
	upstreamURL.Path = r.URL.Path
	upstreamURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), r.Body)
	if err != nil {
		http.Error(w, "リクエスト作成エラー", http.StatusInternalServerError)
		return
	}

	// ヘッダーをコピーする（ホップバイホップは除く）
	for k, vv := range r.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	// 圧縮レスポンスを受け取らないようにする（書き換え処理を簡素化するため）
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := h.client.Do(req)
	if err != nil {
		http.Error(w, "upstream エラー: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "upstream レスポンス読み込みエラー", http.StatusBadGateway)
		return
	}

	// /snmp エンドポイントかつ Prometheus テキスト形式のレスポンスを書き換える
	contentType := resp.Header.Get("Content-Type")
	if isSnmpPath(r.URL.Path) && isPrometheusText(contentType) && resp.StatusCode == http.StatusOK {
		target := r.URL.Query().Get("target")
		rewritten, err := h.rw.Rewrite(body, contentType, target)
		if err != nil {
			log.Printf("メトリクス書き換えエラー（元のレスポンスをそのまま返します）: %v", err)
		} else {
			body = rewritten
		}
	}

	// レスポンスヘッダーをコピーする
	for k, vv := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	// 書き換えにより Content-Length が変わる可能性があるため再設定する
	w.Header().Del("Content-Length")

	w.WriteHeader(resp.StatusCode)
	w.Write(body) //nolint:errcheck
}

func isSnmpPath(path string) bool {
	return path == "/snmp" || strings.HasPrefix(path, "/snmp?") || strings.HasPrefix(path, "/snmp/")
}

func isPrometheusText(contentType string) bool {
	return strings.Contains(contentType, "text/plain") ||
		strings.Contains(contentType, "application/openmetrics-text")
}

func isHopByHop(header string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(header, h) {
			return true
		}
	}
	return false
}
