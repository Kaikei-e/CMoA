---
title: "常駐面の梱包：monitor も host ネットワークで 0.0.0.0:3999 を待つ。Docker の ports ではホスト loopback の serve に届かない"
status: accepted
date: 2026-09-08
supersedes: ["0016"]
depends-on: ["0012"]
---

# 0017: 常駐面の梱包：monitor も host ネットワークで 0.0.0.0:3999 を待つ。Docker の ports ではホスト loopback の serve に届かない

## ステータス

Accepted

採択日: 2026-09-08

本記録は ADR-0016 を supersede する。0014 の D1 と D3、イメージ 2 つ、serve の host
ネットワーク、`--allow-remote` を付けない、は残す。改めるのは **monitor の置き場** である。
`CMOA_MONITOR_GATEWAY` による loopback 書き換えは、必要な人が付ける観測用の環境変数として
コードに残してよいが、compose の既定では使わない。

## 日付

2026-09-08

## コンテキスト

0015 / 0016 は monitor を bridge に載せ、`3999:3999` を publish した。Cursor のブラウザは
ページを開く。しかし `host.docker.internal`（`host-gateway` = `docker0` の `172.17.0.1`）へ
向けたプローブは、ホストの **`127.0.0.1:8095` には届かない**。host ネットワークの `cmoa serve`
も、`127.0.0.1:8081` に publish した提案者も、bridge の gateway IP では ECONNREFUSED になる。
画面は開き、FLEET は 0、COMM は `SERVE OFFLINE` になる。

Docker の `ports:` はコンテナのポートをホストに出す装置で、host ネットワークのプロセスには
適用できない。逆に、ホストで `0.0.0.0:3999` を待っているプロセスは、Compose の `ports` が無くても
Cursor の自動フォワードの対象になる。0014 が `HOST=127.0.0.1` にしたのが、フォワードされ
なかった直接の理由である。

## 決定

### D2‴. 両方 host ネットワーク。monitor の既定は `HOST=0.0.0.0` `PORT=3999`

`cmoa` と `monitor` は `network_mode: host`。monitor は `HOST=0.0.0.0`、`PORT=3999`。
loopback だけに載せるのは `CMOA_MONITOR_HOST=127.0.0.1` を書いた人の判断。Compose の
`ports:` は付けない。`CMOA_MONITOR_GATEWAY` は compose では設定しない。

## 検討した代替案

- **bridge + `host.docker.internal`。** 不採用。ホスト loopback の serve / フリートに届かない。
- **serve を `0.0.0.0` にして `--allow-remote`。** 不採用。0011 の明示を compose 既定で破る。
- **3999 だけ socat でホストに出す。** 不採用。プロセスが三つになり、host ネットワークで
  `0.0.0.0` を待つより弱い。

## 影響とトレードオフ

- 得るもの：ブラウザが 3999 を開き、同じ画面から serve とフリートに届く。
- 失うもの：`docker ps` の PORTS 欄は空のまま。LAN から 3999 が見える。serve は loopback。
