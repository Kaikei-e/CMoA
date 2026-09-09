---
title: "常駐面の梱包：monitor の 3999 はホストの全インタフェースに出す。Cursor のフォワードは loopback 専用の publish では拾わない"
status: superseded
date: 2026-09-08
supersedes: ["0015"]
depends-on: ["0012"]
---

# 0016: 常駐面の梱包：monitor の 3999 はホストの全インタフェースに出す。Cursor のフォワードは loopback 専用の publish では拾わない

## ステータス

Superseded by 0017

採択日: 2026-09-08

本記録は ADR-0015 を supersede する。0015 の構成（serve は host ネットワーク、monitor は
bridge、`CMOA_MONITOR_GATEWAY`、設定ファイルは一つ）は残す。改めるのは **ホスト側の bind** だけ
である。

## 日付

2026-09-08

## コンテキスト

0015 は `ports: 127.0.0.1:3999:3999` にした。`docker ps` には載る。ホスト上の
`curl http://127.0.0.1:3999` も 200 を返す。それでも Cursor のブラウザとポートフォワードは
3999 を開かない。IDE が自動フォワードするのは **全インタフェース（`0.0.0.0`）で待っている
ポート** で、loopback 専用の publish は対象外になる。

LAN に出したくないという 0015 の意図は残るが、既定を loopback にすると「画面を開く」が
成立しない。認証も TLS も無いのは 0011 どおりで、出す範囲を狭めるのは
`CMOA_MONITOR_BIND=127.0.0.1` を書いた人の判断にする。

## 決定

### D2″. monitor の publish 既定は `3999:3999`（`0.0.0.0`）

Compose の `ports` 既定は `${CMOA_MONITOR_PORT:-3999}:3999`。コンテナ内はこれまでどおり
`HOST=0.0.0.0`。ホストを loopback だけにするときは `CMOA_MONITOR_BIND=127.0.0.1` を付ける。
serve の `--allow-remote` は依然付けない。

## 検討した代替案

- **`127.0.0.1` と `[::1]` の両方に出す。** 不採用。IPv6 の localhost は拾えるが、Cursor の
  自動フォワードは依然として loopback 専用を無視する。
- **`.vscode` に `remote.portsAttributes` を書いて 127.0.0.1 のままにする。** 不採用。IDE 設定は
  リポジトリの契約ではなく、Compose の `ports` が見えて初めてフォワードされる。

## 影響とトレードオフ

- 得るもの：Cursor のポートフォワードとブラウザが 3999 を開く。
- 失うもの：同一 LAN の他ホストからも 3999 が見える。serve は loopback のまま。狭めるのは
  `CMOA_MONITOR_BIND`。
