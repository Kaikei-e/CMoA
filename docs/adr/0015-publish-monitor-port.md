---
title: "常駐面の梱包：monitor は 3999 を publish し、host.docker.internal 経由でホストの serve とフリートに届く"
status: superseded
date: 2026-09-08
supersedes: ["0014"]
depends-on: ["0012"]
---

# 0015: 常駐面の梱包：monitor は 3999 を publish し、host.docker.internal 経由でホストの serve とフリートに届く

## ステータス

Superseded by 0016

採択日: 2026-09-08

本記録は ADR-0014 を supersede する。0014 の D1（イメージは 2 つ）と D3（compose はチャットの
serve と観測面だけ）は引き継ぐ。改めるのは D2 のうち **monitor のネットワーク** だけである。
`cmoa serve` は引き続き host ネットワークで、既存の loopback `cmoa.json` を書き換えない。

## 日付

2026-09-08

## コンテキスト

0014 は両方を `network_mode: host` にした。host ネットワークでは Compose の `ports:` が効かず、
Docker も Cursor のポートフォワードも 3999 を公開しない。モニターは `HOST=127.0.0.1` で
コンテナ＝ホストの loopback にだけ載るので、ツールが期待する `127.0.0.1:3999->3999` が無い。

`cmoa serve` まで bridge に載せると `proposers[].base_url` の `127.0.0.1` がコンテナ自身になり、
設定の二重管理か `--allow-remote` が要る。モニターだけを publish すれば、画面に必要な 3999 だけが
フォワード対象になり、serve は loopback のままである。

## 決定

### D2′. `cmoa serve` は host ネットワーク。monitor は `127.0.0.1:3999:3999` を publish する

- `cmoa` サービスは 0014 どおり host ネットワーク。`--allow-remote` は付けない。
- `monitor` サービスは bridge ネットワーク、`HOST=0.0.0.0`、`ports: 127.0.0.1:3999:3999`。
  ホスト側を LAN に出すのは `CMOA_MONITOR_BIND=0.0.0.0` を書いた人の判断。
- コンテナからホストの loopback へ届けるため `extra_hosts: host.docker.internal:host-gateway` と
  `CMOA_MONITOR_GATEWAY=host.docker.internal` を付ける。モニターは `cmoa.json` の
  `base_url` と `serve.listen` を書き換えず、**outbound のときだけ** loopback ホストを
  gateway に差し替える（0012 D3 の「設定ファイルは一つ」）。画面に出す listen は元の値。
- vault と `runs_dir` の bind（`CMOA_BIND`、UID/GID）は 0014 のまま。

## 検討した代替案

- **両方 host ネットワークのまま、`HOST=0.0.0.0` だけにする。** 不採用。ホストの 3999 には載るが
  `ports:` は依然無効で、フォワード一覧に出ない。
- **両方を bridge にして `cmoa.json` を書き換える。** 不採用。0014 が拒んだ理由と同じ。
- **モニター専用の第二設定ファイル。** 不採用。0012 D3。

## 影響とトレードオフ

- 得るもの：`docker ps` と Cursor のポートフォワードが 3999 を見る。ブラウザは
  `http://127.0.0.1:3999`。
- 代償：monitor は Docker のポートプロキシ経由になる。serve とフリートへのプローブは
  `host.docker.internal` に依存する（Linux の `host-gateway`）。
