---
title: "常駐面の梱包：cmoa serve と Monitor を別イメージにし、host ネットワークで既存の cmoa.json のまま起動する"
status: superseded
date: 2026-09-08
depends-on: ["0012"]
---

# 0014: 常駐面の梱包：cmoa serve と Monitor を別イメージにし、host ネットワークで既存の cmoa.json のまま起動する

## ステータス

Superseded by 0015

採択日: 2026-09-08

## 日付

2026-09-08

## コンテキスト

0011 D2 はコマンドを 6 つに限り、常駐するのは `cmoa serve` だけだと決めた。0012 D1 は観測面を
その 6 コマンドの外に置き、`monitor/` の別プロセスにした。どちらも起動手順はホストの Go と
Node に依存し、設定は loopback の提案者・審判・`serve.listen` を書いた既存の `cmoa.json` である。
CMoA はモデルサーバを起動しない。

「コンテナで一度に立ち上げる」は検証器を持たない梱包の話なので roadmap の step には入らない。
それでも起動経路を足すなら、0011 の「loopback 以外は `--allow-remote`」、0012 の「モニター専用の
設定ファイルは作らない」「Go にモニターを埋め込まない」を崩さない形に限る。

## 決定

### D1. イメージは 2 つ。7 つ目のコマンドも、Go への埋め込みもしない

`deploy/cmoa.Dockerfile` は `cmoa` と、run が vault を記録するための `git` / `docdag`（CI と同じ
v0.4.1）を入れる。`monitor/Dockerfile` は `monitor/` の adapter-node ビルドである。
リポジトリ根の `compose.yaml` が両方を起動する。`cmoa monitor` サブコマンドは作らず、静的資産を
`go:embed` もしない（0012 D1 と同じ境界）。イメージはモデルを含まない。

### D2. 既定は host ネットワークと、ホストと同じパスへの bind

提案者・審判・`serve.listen` は既定で loopback である。bridge ネットワークに載せると
`127.0.0.1` はコンテナ自身になり、設定を書き換えなければフリートに届かない。モニターは
`serve.listen` をクライアント URL として使うので、`0.0.0.0:8095` では中継できない。

よって compose の既定は `network_mode: host` である。vault と `serve.runs_dir` は設定ファイルからの
相対パスなので、ホストのディレクトリを**同じ絶対パス**で載せる。載せる根は `CMOA_BIND`（省略時は
`$HOME`）。これで手元の `cmoa.json` を書き換えずに済み、`--allow-remote` も既定では渡さない。
bind の外に vault があるときは `CMOA_BIND` を共通の親にする。トレースの所有者はホストの UID/GID
（`CMOA_UID` / `CMOA_GID`）に合わせる。

モニターの HTTP は既定 `127.0.0.1:3999`。LAN に出すのは `CMOA_MONITOR_HOST=0.0.0.0` を書いた人の
判断で、serve の `--allow-remote` と同じ種類の明示である。

### D3. compose が上げるのはチャット面の serve と観測面だけ

コーディング面の verifier はタスクの compose を `docker compose run` する（0005）。その Docker を
CMoA イメージに入れることも、`docker.sock` を渡すことも、モデルサーバを compose に足すことも
しない。`serve` はチャット面専用のまま（0011）。

## 検討した代替案

- **bridge ネットワーク + `host.docker.internal`。** 不採用。`cmoa.json` の `base_url` と
  `serve.listen` をコンテナ用に書き換えるか、モニター側に第二の listen を足すことになり、0012 D3
  の「設定は `cmoa.json` 一つ」を崩す。
- **単一イメージに monitor の `build/` を載せる。** 不採用。プロセスは結局 2 つ必要で、Go の
  イメージに Node の実行が混ざる。0012 D1。
- **モデルサーバも compose に含める。** 不採用。CMoA はエンドポイントのクライアントであり、
  モデルの起動は範囲外（README の前提）。
- **`docker.sock` を渡してコーディング面もコンテナから回す。** 不採用。verifier の隔離はタスクの
  compose の責任（0005）で、常駐面の梱包がそれを肩代わりすると RunnerError と候補の fail の境が
  ぼやける。
- **非 loopback に bind して `--allow-remote` を既定で付ける。** 不採用。host ネットワークなら
  既存の loopback 設定がそのまま正しく、認証のない serve を明示なしに開ける理由がない。

## 影響とトレードオフ

- 得るもの：既存の `cmoa.json` とローカルフリートを、`make up` または `./deploy/up.sh` 1 行で
  serve とモニターまで上げられる。
- 失うもの：host ネットワークは Linux 向けである。Docker Desktop の VM では loopback の届き方が
  違い、その環境はホストで `cmoa serve` と `node build` を直接走らせる。
- リスク：`CMOA_BIND` の既定はホームディレクトリなので、コンテナからホームが見える。狭めるのは
  `CMOA_BIND` を設定する側の判断。serve は相変わらず認証も TLS も持たない。
