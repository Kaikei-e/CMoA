---
title: "verifier は Task の compose に委ね、候補ごとに固有プロジェクト名で docker compose run する"
status: accepted
date: 2026-09-04
depends-on: [0002]
---

# 0005: verifier は Task の compose に委ね、候補ごとに固有プロジェクト名で docker compose run する

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### 選択器が実行を必要とする

コーディング面の選択器はテスト実行である（ADR-0002 D1）。つまり CMoA は、**信頼していないモデルが
書いたコードを実行する**。4〜9B のローカルモデルに悪意は想定しないが、誤りは想定する：無限ループ、
ディスクを埋めるテスト、ネットワークに出る依存解決、`rm -rf` を含むテストヘルパ。

### 何を隔離の単位にするか

自作のサンドボックス（namespace、seccomp、cgroups）は、書けはするが**維持できない**。カーネルの
更新、ディストリの差、GPU デバイスのパススルー、ボリュームの権限——どれも CMoA の責務 4 点の
どれにも入らない。一方 Docker Compose は参照機に既にあり、GPU デバイスのパススルーも実績がある。

さらに compose ファイルを **Task 側**に置くと、「何で採点されるか」がタスクの定義に含まれる。
提案者にそのコマンドを見せる（ADR-0004 D5）ことができ、AHE の「編集を反証可能な契約にする」思想の
最小版になる。

### 並行実行が壊すもの

候補を同時に検証すると、compose のプロジェクト名が同じである限り、コンテナ名・ネットワーク・
名前付きボリュームを共有する。`down` が他の候補のコンテナを巻き込む。したがって隔離の単位は
**候補 1 つ = プロジェクト 1 つ**でなければならない。

### 「候補が落ちた」と「verifier が動かなかった」

docker が入っていない、compose ファイルが読めない、デーモンが死んでいる——これらは候補の性質では
ない。混ぜると uzushio の統計（提案者ごとの通過率、β の推定）が汚染される。型で分ける必要がある。

### 満たすべき要件

- R1 候補コードの実行は CMoA のプロセス外・ホストのファイルシステム外で行う
- R2 サンドボックスを自作しない
- R3 候補どうしが同時に走っても、コンテナ・ネットワーク・ボリュームを共有しない
- R4 実行後に資源が残らない（タイムアウトや異常終了の後も）
- R5 タイムアウトで確実に止まり、その事実がトレースに残る
- R6 verifier 基盤の失敗と候補の失敗を型で区別する
- R7 Docker 無しで `go test ./...` が通る（ADR-0002 R5）

## 決定

### D1. 実行コマンドは `docker compose run --rm --no-deps -T`

```
docker compose -f <compose-file> -p <project> run --rm --no-deps -T --quiet-pull <service>
```

環境変数 `CMOA_CANDIDATE_DIR=<候補 worktree の絶対パス>` を付けて実行する。終了コード 0 が pass。

各フラグの理由：

| フラグ | 理由 |
| --- | --- |
| `-f <file>` | compose ファイルは Task ディレクトリのもの（`task.json` の `verify.compose_file`、既定 `compose.yaml`） |
| `-p <project>` | 候補ごとに固有（D2）。既定のディレクトリ名由来では候補間で衝突する |
| `run` | サービスを 1 回実行して終わる。`up` のように依存を立ち上げ続けない |
| `--rm` | 実行後にコンテナを消す |
| `--no-deps` | `depends_on` を起動しない。verifier は 1 サービスで閉じているべきで、依存の起動失敗を候補の失敗と誤解しない |
| `-T` | TTY を割り当てない。CMoA は非対話で stdout/stderr を捕まえる |
| `--quiet-pull` | pull の進捗がトレースの stderr を埋めるのを防ぐ |

### D2. プロジェクト名は候補ごとに固有にする

`verify.ProjectName(taskID, runID, candidateID)` が `cmoa-<task>-<run>-<candidate>` を作り、
小文字化して `[^a-z0-9_-]` を `-` に置換する。Compose のプロジェクト名は小文字英数・ハイフン・
アンダースコアに限られるためで、生成後に `^[a-z0-9][a-z0-9_-]*$` で検証し、外れたら
`*verify.RunnerError` を返す（実行しない）。

`taskID` と `candidateID`（= 提案者 ID）は既に検証済み newtype で、`runID` も固定形式なので、
実際には置換が働く余地はほぼ無い。それでも検証するのは、プロジェクト名が
`docker` に渡る引数だからである。

### D3. 候補ディレクトリは `CMOA_CANDIDATE_DIR` で渡す

compose ファイル側の規約：

```yaml
services:
  verify:
    image: golang:1.27
    working_dir: /work
    volumes:
      - ${CMOA_CANDIDATE_DIR:?set by cmoa select}:/work
      - gomodcache:/go/pkg/mod
    environment:
      GOTOOLCHAIN: local
      GOFLAGS: -mod=mod
      CGO_ENABLED: "0"
    command: ["go", "test", "./..."]
volumes:
  gomodcache:
```

`:?` を付けるのは、変数が無いまま人が手で `docker compose run` した場合に、
ホストのカレントディレクトリを黙ってマウントさせないためである。

CMoA は compose ファイルの中身を解釈しない。サービス名（既定 `verify`）とファイルのパスだけを見る。
どのイメージで、何を実行し、どんな制限（`mem_limit`、`network_mode: none`、`read_only`）を掛けるかは
Task の作者が書く。

### D4. 終了後は必ず `down -v --remove-orphans`

`run` の結果にかかわらず、`defer` ではなく実行直後に必ず teardown を走らせる。
`-v` は名前付きボリュームも消す（候補間でキャッシュが漏れない）。`--remove-orphans` は
compose ファイルから消えたサービスの残骸を掃除する。

teardown は**親コンテキストから切り離した独自の 60 秒デッドライン**で走る
（`context.WithoutCancel` ＋ `WithTimeout`）。run がタイムアウトで死んだ後も掃除は走らなければ
ならず、かつ、デーモンが固まったときに `select` 全体を巻き込んではならない。

### D5. タイムアウトは `verify.timeout_seconds`（既定 600）

`exec.CommandContext` に期限付き context を渡し、`Cmd.Cancel` で先に SIGINT を送って docker に
きれいに止まる機会を与え、`Cmd.WaitDelay`（既定 15 秒）を過ぎたらプロセスを殺す。

タイムアウトした候補は `verify/<id>/result.json` に `status: "timeout"`、`exit_code: -1` で記録する。
これは候補の失敗であって基盤の失敗ではない（無限ループを書いたのは候補である）。

### D6. 並列度は `verify.max_parallel`、既定 1

`select` は semaphore で並列度を絞る。既定を 1 にする理由：

- 候補は同じホストで同じテストスイートを回す。並列にすると CPU とディスクを取り合い、
  タイムアウトの意味が候補ごとに変わる（10 分は他に何が走っているかに依存する）。
- 既定は決定性を優先する。並列にしたい人は設定で上げられる。
- D2 のプロジェクト名分離により、上げても衝突はしない。

### D7. 基盤の失敗は `*verify.RunnerError`、選択結果は `VerifierFailed`

```go
type Runner interface { Run(ctx context.Context, s Spec) (*Result, error) }
type ComposeRunner struct { Docker string; KillAfter time.Duration }
type RunnerError struct { Stage string; Stderr string; Err error }
```

`RunnerError` の `Stage` は `"project name"` / `"compose file"` / `"docker binary"` / `"run"` の
いずれか。docker が PATH に無い、compose ファイルが `stat` できない、docker の起動そのものが
失敗した——これらは候補について何も語らない。`select` はこれを受けたとき
**`VerifierFailed` を選択結果とする**（ADR-0006 D3）。

対して、コンテナが立ち上がって非 0 で終わったのは候補の失敗で、`status: "fail"` である。
両者を混ぜないことが R6 であり、uzushio が β を数えるための前提になる。

`Runner` をインターフェースにしてあるのは、`select` のテストが偽の runner を差し込めるようにする
ためである（R7）。`ComposeRunner` 自身のテストは PATH に偽 `docker` スクリプトを置いて引数を検査する。
実 Docker を使う e2e は環境変数（`CMOA_E2E_DOCKER=1`）で明示的に有効にしたときだけ走る。

### D8. サンドボックスは自作しない。verifier は編集不可

隔離の実装を CMoA は持たない（R2）。加えて verifier は自己改善ループの**読み取り専用**
コンポーネントである（ADR-0008 D3）。ループが verifier を編集できるなら、ループは合格の定義を
書き換えられる。AHE の実装がまさにこの制約を置いている。

## 根拠（調査結果・出典）

### A. Docker の一次情報

- `docker compose run` のリファレンス（`--rm`、`--no-deps`、`-T`、`--quiet-pull` の意味と、
  `run` が単一サービスを 1 回実行する挙動）—
  https://docs.docker.com/reference/cli/docker/compose/run/
- `docker compose down` のリファレンス（`-v` は名前付きボリュームを削除、`--remove-orphans` は
  compose ファイルに無いサービスのコンテナを削除）—
  https://docs.docker.com/reference/cli/docker/compose/down/
- プロジェクト名の規則（小文字英数、ハイフン、アンダースコア。`-p` / `COMPOSE_PROJECT_NAME` で
  指定し、既定はディレクトリ名）— https://docs.docker.com/compose/how-tos/project-name/
- Compose の環境変数の補間と `${VAR:?err}` 記法 —
  https://docs.docker.com/reference/compose-file/interpolation/

### B. Go の子プロセス制御

- `exec.CommandContext` ＋ `Cmd.Cancel` ＋ `Cmd.WaitDelay`（Go 1.20 以降）が
  「タイムアウトで子プロセスを確実に殺す」正攻法である。`os/exec` は Go 1.26 / 1.27 の
  リリースノートで変更されていない。 https://pkg.go.dev/os/exec
- `context.WithoutCancel`（Go 1.21）で teardown を親のキャンセルから切り離す。

### C. 設計の上位方針

- Agentic Harness Engineering（arXiv 2604.25850）§3.3 逐語：
  「Evolve Agent はハーネスワークスペースの内側にのみ書き込み、**runs ディレクトリ、tracer、verifier、
  LLM 設定は読み取り専用**であり、種となる system prompt は削除不可と印付けされる」。
  https://arxiv.org/abs/2604.25850
- Lilian Weng「Harness Engineering for Self-Improvement」(2026-07-04)：評価器と権限制御は
  ハーネスを進化させるループの外に置く。
- ADR-0004 の誤り C（実測）：`git apply --ignore-whitespace` は文脈行しか緩めないため、
  追加行のインデント誤りは適用に成功して残る。verifier のコマンド列に `gofmt -l` / `go vet` を
  1 段目に置くことが推奨される（ADR-0004 D6）。

### D. 2026-09-04 に採択された決定

| 決定 | 理由 |
| --- | --- |
| verifier は `docker compose run` を必須とし、サンドボックスは自作しない | 隔離の維持は CMoA の責務 4 点のどれでもない。compose は既に環境にあり、`/dev/dri` のパススルーも実績がある |
| 検証並列度は `max_parallel`、既定 1 | 並列にするとタイムアウトの意味が同時実行数に依存する |

## 検討した代替案

- **A. namespace / seccomp / cgroups でサンドボックスを自作する。** 不採用（D8）。
  維持コストが CMoA の責務 4 点のどれにも属さない。Docker が既にある環境で二重実装になる。
- **B. ホストで直接テストを走らせる。** 不採用。候補は信頼していないコードであり、
  ホストのファイルシステムとネットワークに触れさせる理由がない。`git worktree` で隔離されるのは
  ソースだけで、実行は隔離されない。
- **C. `docker run` を直接呼ぶ（compose を経由しない）。** 不採用。イメージ・ボリューム・環境変数・
  資源制限をすべて CMoA のコード内かフラグで表現することになり、Task 側に「何で採点されるか」を
  置けなくなる。compose ファイルはそれ自体が Task の一部である。
- **D. `docker compose up -d` ＋ `exec` にする。** 不採用。サービスを常駐させるとライフサイクルが
  run より長くなり、掃除の責任が曖昧になる。`run --rm` は 1 回実行して消える。
- **E. プロジェクト名を Task ごとに固定する。** 不採用。同じ Task の候補が並列に走ると
  コンテナ名とボリュームを共有し、`down -v` が他の候補を巻き込む。
- **F. `--rm` を付けず、失敗したコンテナを残して調査に使う。** 不採用。ログは
  `verify/<id>/stdout.txt` と `stderr.txt` に残り、コードは worktree ではなく `.diff` から再現できる。
  コンテナを残すと `down -v` の意味が消え、ディスクが埋まる。
- **G. 最初の pass が出たら残りの検証を打ち切る。** 不採用（ADR-0006 D2）。
  `also_passed` と β の測定のために全候補を検証し切る。
- **H. 並列度の既定を提案者数（3）にする。** 不採用（D6）。タイムアウトの意味が負荷に依存する。

## 影響とトレードオフ

**得るもの**

- 隔離の実装を持たない。Docker と compose の挙動が変わっても、CMoA が直すのは引数の並びだけである。
- Task が「何で採点されるか」を自分で宣言する。提案者にそれを見せられる。
- `RunnerError` / `VerifierFailed` と `fail` の分離により、「docker が動いていなかった夜」の run が
  提案者の成績として集計されない。

**失うもの・リスク**

- **Docker への依存が実行時要件として固定される。** `select` は Docker 無しでは動かない。
  ユニットテストは偽 `docker` で通るので R7 は満たすが、実際に候補を検証するには Docker が要る。
- **資源制限は Task 任せである。** `mem_limit` や `pids_limit` を書かない compose を渡されると、
  暴走した候補がホストを圧迫しうる。CMoA が持つ防御はタイムアウトだけである。
- **`down -v --remove-orphans` は名前付きボリュームを毎回消す。** モジュールキャッシュを
  ボリュームに置くと候補ごとに再取得になり、`golang` イメージでの `go test` が遅くなる。
  例題（`examples/task-hello`）は `gomodcache` ボリュームを宣言しているが、`-v` で毎回消えるため
  キャッシュとしては効かない — **この点は未検証で、e2e の実測で遅ければ D4 を見直す。**
- **並列度 1 が既定なので、候補 3 体の検証は逐次で 3 倍の時間がかかる。** 夜間バッチを前提に
  許容する。上げたい人は `max_parallel` を上げられる。
- **compose のプロジェクト名は 1 run 1 候補で一意だが、run を跨いだ残骸は掃除されない。**
  `down` が失敗した場合（デーモン再起動など）、`cmoa-*` のプロジェクトが残る。
  掃除コマンドは v0 では提供しない。

## 関連ADR

- 0002（v0 のスコープ）— 依存ゼロと「外部プロセスは `os/exec`」の方針
- 0004（候補の表現）— 適用済み worktree を渡す側。誤り C を拾うのが本記録の verifier
- 0006（選択規則）— `RunnerError` を `VerifierFailed` に写す規則、および全候補を検証し切る決定
- 0007（トレース）— `verify/<id>/result.json` の形
- 0008（編集可能面）— verifier が読み取り専用コンポーネントである理由
