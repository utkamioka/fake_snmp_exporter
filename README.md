# fake_snmp_exporter

[snmp_exporter](https://github.com/prometheus/snmp_exporter) のフリをして動く偽物の exporter です。
upstream の snmp_exporter からメトリクスを取得し、counter は単調増加・gauge は上下変動するように値を書き換えて返します。

## 背景

snmp_simulator によるデータは固定値のため、Prometheus でグラフを描くと常に同じ値になり、グラフ機能の確認ができません。
fake_snmp_exporter はメトリクスの値を時間経過とともに変化させることで、実運用に近いグラフ動作を再現します。

## インストール

```bash
go build -o fake_snmp_exporter .
```

Linux・Windows 両対応です。

## 設定ファイル

実行ファイルと同じディレクトリに `fake_snmp_exporter.yml` を配置するか、環境変数 `FAKE_SNMP_EXPORTER_CONFIG` でパスを指定します。

```yaml
upstream:
  # manage: true の場合、snmp_exporter を子プロセスとして起動します
  # manage: false の場合、起動済みの snmp_exporter に接続します
  manage: false

  # manage: true 時: snmp_exporter バイナリのパス（省略時は PATH から探索）
  # binary: /usr/local/bin/snmp_exporter

  # upstream snmp_exporter のホスト名（manage: false 時）
  host: localhost

  # upstream snmp_exporter のポート番号
  # manage: true  の場合: このポートで snmp_exporter を起動する
  # manage: false の場合: このポートに接続する
  port: 9117

rewrites:
  - metric: ifHCInOctets       # 書き換え対象のメトリクス名
    type: counter               # counter: 単調増加
    rate: 1000                  # 1秒あたりの増分
    jitter: 0.3                 # ランダム性（0.0〜1.0、±30% の場合 0.3）

  - metric: entSensorValue
    labels:                     # ラベルで絞り込む（省略時は全時系列が対象）
      entPhysicalName: "Switch 1 - Inlet Temp Sensor"
    type: gauge                 # gauge: 上下変動
    min: 20                     # 最小値
    max: 35                     # 最大値
    max_delta: 1                # 1回の最大変化量
    min_hold: 3600              # 変動間隔（秒）
```

### counter の書き換え式

```math
V(t) = v + Σ( rate * Δt * (1 + jitter * rand(-1, 1)) )
```

- `v`: 起動時点の snmp_exporter の出力値
- `rate`: 毎秒の増分（例: `rate: 1000` → 毎秒 1000 前後増加）
- `jitter`: ランダム性の割合（例: `jitter: 0.3` → ±30%）

`rate(ifHCInOctets[5m])` のようなクエリで上下変動するグラフが得られます。

### gauge の書き換え式

1. 起動時は snmp_exporter の出力値をそのまま保持します
2. `min_hold` 秒ごとに `max_delta` 以内のランダム変化を加えます
3. 値は常に `[min, max]` の範囲にクランプされます

## 起動方法

fake_snmp_exporter は snmp_exporter と同じフラグをすべて受け付けます。
既存の snmp_exporter の起動コマンドをそのまま置き換えて使用できます。

```bash
# snmp_exporter の代わりに起動する（設定ファイルはそのまま流用可能）
./fake_snmp_exporter --config.file=snmp.yml --web.listen-address=:9116
```

### upstream として起動済みの snmp_exporter を使う場合

```yaml
# fake_snmp_exporter.yml
upstream:
  manage: false
  host: localhost
  port: 9117   # 起動済み snmp_exporter のポート
```

fake_snmp_exporter がポート 9116 を使用するため、snmp_exporter は別のポートで起動しておきます。

```bash
# snmp_exporter を別ポートで起動しておく
./snmp_exporter --config.file=snmp.yml --web.listen-address=:9117

# fake_snmp_exporter を起動する
./fake_snmp_exporter --config.file=snmp.yml
```

### snmp_exporter を内部で起動する場合

```yaml
# fake_snmp_exporter.yml
upstream:
  manage: true
  binary: /usr/local/bin/snmp_exporter
  port: 9117   # 内部で起動する snmp_exporter に割り当てるポート
```

```bash
# fake_snmp_exporter が snmp_exporter を自動的に起動・管理します
./fake_snmp_exporter --config.file=snmp.yml
```

CTRL-C で fake_snmp_exporter を停止すると、内部の snmp_exporter も同時に終了します。

## メトリクスの取得

snmp_exporter と同じ URL でメトリクスを取得できます。

```bash
curl 'http://localhost:9116/snmp?target=10.0.0.1:161&module=if_mib'
```

## 設定ファイルのパス

| 優先順位 | 探索先 |
| -------- | ------ |
| 1 | 環境変数 `FAKE_SNMP_EXPORTER_CONFIG` で指定したパス |
| 2 | 実行ファイルと同じディレクトリの `fake_snmp_exporter.yml` |
| 3 | カレントディレクトリの `fake_snmp_exporter.yml` |
