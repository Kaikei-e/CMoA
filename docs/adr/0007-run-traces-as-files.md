---
title: "トレースは run ディレクトリのファイルとして原子的に書き、CMoA は読み返さない"
status: accepted
date: 2026-09-04
depends-on: [0002, 0006]
---

# 0007: トレースは run ディレクトリのファイルとして原子的に書き、CMoA は読み返さない

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### トレースは誰のためにあるか

CMoA の第 3 の責務は「トレースをファイルとして出す」ことである。読み手は 3 者いる：

1. **uzushio** — 失敗を `pattern` に落とし、β と通過率を数え、`edit` の効果を判定する
2. **人** — なぜその候補が選ばれたのかを後から読む
3. **CMoA 自身** — 実は読まない（例外は ADR-0006 D7 の 1 つだけ）

3 番目が重要である。CMoA がトレースを読み返して振る舞いを変えると、run の結果が
「過去にどんな run があったか」に依存し、1 run の記述が閉じなくなる。

### ファイルシステムを記憶にする先行例

Meta-Harness（2603.28052）は「ソースコード、スコア、過去候補の実行トレースをファイルシステム経由で
提案エージェントに見せる」。AHE（2604.25850）は runs ディレクトリを読み取り専用として据える。
どちらも「候補と履歴はディレクトリである」という同じ形を採っている。

### 何をもって run を記述したと言えるか

初期の設計は「トレースがあれば再現できる」と書いていたが、これは守れない。llama.cpp のビット単位決定的推論は
PR #16016 として 2025-09 に起票されたまま draft で、しかも CUDA 限定である。参照フリートは Vulkan なので
対象外。加えて非決定性の主因はバッチサイズによって reduction の分割が変わるカーネルであり、
同一プロンプトを 8 スロットに投げると 5〜8 通りの completion が返るという報告がある。

したがって保証するのは**再現ではなく記述**である：その run が何を読み、何を投げ、何が返り、
何が通ったか。

### ハーネスの時点をどう固定するか

CMoA が読むハーネス（uzushio の vault にある binding な決定）は日々変わる。後から
「その run はどのハーネスを見ていたのか」を再構成するには、DocDag の 2 本の時間軸が要る：
valid time（`--as-of`、その日に有効だと vault が述べていたもの）と transaction time
（`--at`、vault 自体のリビジョン）。

### 満たすべき要件

- R1 1 run = 1 ディレクトリ。ディレクトリを見れば run の全体が分かる
- R2 run-id は時系列順にソートでき、最新が辞書順最大になる
- R3 `as_of` と `at` を必ず記録する
- R4 CMoA はトレースを読み返さない（ADR-0006 D7 の例外を除く）
- R5 途中で死んでも半端な JSON が残らない
- R6 一度書いた事実を後から書き換えない
- R7 秘密（API キー、Authorization ヘッダ）を書かない
- R8 スキーマは uzushio が読める形で公開される

## 決定

### D1. 配置は `<task>/runs/<run-id>/`、run-id は `YYYYMMDDTHHMMSSZ-<8 hex>`

```
YYYYMMDDTHHMMSSZ-xxxxxxxx     例: 20260904T231207Z-3f9a1c04
```

UTC の秒精度タイムスタンプ ＋ `crypto/rand` の 4 バイト（16 進 8 桁）。正規表現
`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$` で検証し（`trace.ParseRunID`）、`trace.Latest` は
`runs/` 配下でこの形に合うディレクトリ名の**辞書順最大**を最新とする。
`cmoa select --run` を省略するとこれが使われる。

Go 1.27 は標準に `uuid` を持ち、`NewV7()` は時系列ソート可能である。**それでも採らない**：
run-id はディレクトリ名として人が読み、`grep` の対象になる。UUIDv7 は先頭 48 bit が
タイムスタンプなので機械的な順序は同じだが、人には日付が読めない。乱数部を 8 桁にしたのは、
同一秒に 2 つの run を作らない限り衝突が問題にならないためである。

run ディレクトリを Task の下に置くのは、Task が実験の単位だからである。Task を配れば
その履歴も一緒に動く。

### D2. ファイルの配置と役割

```
run.json                          propose が 1 回だけ書く
prompt/<proposer-id>.json         その提案者に送った messages とリクエスト本文
candidates/<proposer-id>.json     候補のメタデータ（status を含む）
candidates/<proposer-id>.raw.txt  応答本文そのまま（思考除去前）
candidates/<proposer-id>.diff     抽出した diff（抽出できた場合のみ）
verify/<proposer-id>/result.json  select が書く検証結果
verify/<proposer-id>/stdout.txt
verify/<proposer-id>/stderr.txt
select.json                       select が 1 回だけ書く
```

| ファイル | 何を確定させるか |
| --- | --- |
| `run.json` | 何を読んで始めたか：`task`（id, dir, repo, `rev` と解決済み `resolved_rev`, `files`, `instruction_sha256`）、`config`（既定値を埋めた実効設定）、`harness`（vault, `as_of`, `at`, `docdag_version`, `binding` の一覧）、`proposers`、`byzantine` {n, f}、`cmoa_version`、`prompt_version` |
| `prompt/<id>.json` | 何を投げたか：`messages`、HTTP 本文そのもの、その `sha256` |
| `candidates/<id>.json` | 何が返ったか：`status`、`error`、`finish_reason`、`usage`、`timings`、`diff`（files/additions/deletions/sha256）、`request_sha256`、`response_sha256`、開始・終了時刻 |
| `verify/<id>/result.json` | 何が起きたか：`status`、`exit_code`、`duration_ms`、`command`、`project_name`、`apply_error` |
| `select.json` | 何が選ばれたか：`rule`、`order`、`selection`（直和型の JSON 形）、`also_passed`、`max_parallel` |

ファイル名を綴るのは `internal/trace` の `Dir` 型のメソッドだけである
（`RunFile()` / `PromptFile(id)` / `CandidateFile(id)` / `VerifyResult(id)` …）。
他のパッケージはパスを組み立てない。

`schema_version` は `run.json` と `select.json` が持ち、現在 1。**フィールドの追加では上げない**。
既存フィールドの意味が変わったときだけ上げる。

### D3. `as_of` と `at` は必須。vault も必須

`propose` は最初に `harness.Take` を呼び、失敗したら run ディレクトリを作らずに終わる。

- `as_of`：`--as-of YYYY-MM-DD`。省略時は実行日（UTC）。これが DocDag の valid time で、
  「その日に有効だと vault が述べていた決定」を指す。
- `at`：`git -C <vault> rev-parse HEAD`。`git status --porcelain` が空でなければ **`-dirty`
  サフィックス**を付ける。これが transaction time であり、dirty な vault で走った run は
  そもそも再構成できないという事実をここで明示する。
- `binding`：`docdag query --binding --fields id,title,status,path --format json --as-of <asOf>` を
  vault ディレクトリで exec した結果。
- `docdag_version`：`docdag --version` の出力。

**DocDag はパッケージとして import せず、CLI として exec する**（ADR-0002 D3）。v0 が使う口は
`query --binding` の 1 つだけで、`context` / `validate --touching` は使わない。
CMoA は `docdag.yaml` を生成も編集もしない。

vault を必須にしたのは 2026-09-04 の決定である。「ハーネスを読まずに走った run」を許すと、
その run が何を前提にしていたのか後から言えない。

### D4. CMoA は書くだけで読み返さない

例外は 1 つだけ：`select` が同じ run の `candidates/<id>.json` と `<id>.diff` を読む
（ADR-0006 D7）。それ以外の読み取り API（`ReadRun` / `ReadSelect`）はテストと外部の読み手のために
存在し、CMoA の実行経路では使わない。

過去の run を読んで振る舞いを変えること（学習、キャッシュ、再開）は v0 の非目標である。
学習は uzushio の側にある。

### D5. 原子的書き込みと write-once

- すべての JSON は一時ファイル（同じディレクトリの `.<name>.*`）に書き、`Sync` してから
  `rename` する。半端な JSON が読み手に見えることがない（R5）。パーミッションは 0644。
- **`run.json` と `select.json` は既に存在すればエラー**（`trace.ErrExists`）。
  run は追記されるだけで、書き換えられない（R6）。
- `trace.Create` は run ディレクトリが既にある場合にエラーを返す。`--run-id` を指定して
  同じ run を上書きすることはできない。

### D6. トレースが保証するのは再現ではなく記述

`docs/trace-schema.md`（公開仕様）とこの記録に、次を明記する：

> CMoA のトレースは「同じ入力から同じ出力を再現できる」ことを保証しない。保証するのは
> 「その実行が何を読み、何を投げ、何が返り、何が通ったか」を完全に記述することである。

`request_sha256` / `response_sha256` / `instruction_sha256` / `diff.sha256` は、再現の保証ではなく
**同定**のためにある：2 つの run が同じ本文を投げたかどうかを、本文を突き合わせずに言える。

緩和として、提案者はサーバーのスロット並行に頼らず 1 モデル 1 スロットで動かすことを推奨する
（バッチ非決定性を減らす）。これはフリート側の設定であり、CMoA の決定ではない（ADR-0003 D8）。

### D7. 秘密を書かない

- `run.json` の `config` は `Config.Redacted()` を通す。設定に載るのは `api_key_env`
  （環境変数**名**）だけで、値はそもそも設定ファイルに存在しない（ADR-0003 D4）。
- `prompt/<id>.json` の `request` は HTTP 本文であり、`Authorization` ヘッダを含まない。
- 環境変数一式をトレースに写すことはしない。

### D8. スキーマの正は Go の型、公開仕様は `docs/trace-schema.md`

`internal/trace/trace.go` の構造体が正である。`docs/trace-schema.md` はそれを uzushio の実装者に
向けて説明する文書で、両者が食い違ったら型が勝つ。

OpenTelemetry の GenAI semantic conventions（`gen_ai.*`）を**フィールド名の語彙として借りることは
検討したが、v0 では採らなかった**。CMoA 固有の情報（`as_of` / `at` / `byzantine` / 候補ステータス）が
規約に存在せず、借りた部分と借りていない部分が混ざると、どちらの規約でもない名前空間になる。
トレースは素の snake_case で書き、OTel への写像が必要になった時点で uzushio 側で行う — **未検証**。

## 根拠（調査結果・出典）

### A. ファイルシステムを記憶にする先行例

- Meta-Harness（arXiv 2603.28052）：「ソースコード、スコア、過去候補の実行トレースを
  ファイルシステム経由でアクセスする agentic proposer を用いる」。
  https://arxiv.org/abs/2603.28052
- Agentic Harness Engineering（arXiv 2604.25850）§3.3：「runs ディレクトリ、tracer、verifier、
  LLM 設定は読み取り専用」。また各編集には
  「失敗の証拠、推定した根本原因、対象とする修正、期待される修正と危険な回帰からなる予測影響」を
  述べる change manifest が付く。uzushio の `predicts:` はこれに合わせて
  `expected_fixes` と `at_risk_regressions` の 2 項に分けるとよい（uzushio 側の課題）。
  https://arxiv.org/abs/2604.25850

### B. 再現性の限界

- llama.cpp PR #16016（ビット単位決定的推論。2025-09-15 起票、2026-09 現在 **draft のまま**、
  **CUDA 限定**。`-DGGML_DETERMINISTIC=ON` / `--deterministic`、完全再現には
  `temperature=0, top_k=1, top_p=1` も要る）— https://github.com/ggml-org/llama.cpp/pull/16016
- llama.cpp Issue #7052：同一プロンプトを 8 スロットに投げると 5〜8 通りの completion が返る。
  非決定性の主因はバッチサイズによる reduction 分割の変化である。
  https://github.com/ggml-org/llama.cpp/issues/7052
- 参照フリートは Vulkan なので上記の決定的経路の対象外である。

### C. DocDag の 2 本の時間軸

- DocDag ADR-0005「有効期間を kind ごとの `period:` として宣言し、binding・現行・逸脱の効力を
  as-of 時点の射影にする」：`--as-of`（valid time）と `--at`（transaction time）は独立で、
  組み合わせると bitemporal な問い合わせになる。
  https://github.com/Kaikei-e/DocDag/blob/main/docs/adr/0005-in-force-periods-and-as-of-projection.md
- DocDag README「The model」：binding = accepted かつ未置換。 https://github.com/Kaikei-e/DocDag
- `docdag query --binding --as-of <day> --at <rev>` で「その run が見ていたハーネス」を後から
  再構成する、という設計はこの 2 軸に乗っている。

### D. llama-server のレスポンス形状

- llama-server は OpenAI 互換レスポンスにトップレベルの `timings` を足す（`prompt_ms`、
  `predicted_ms`、`predicted_per_second` など）。非ストリームなら最終レスポンスに必ず入る。
  https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
  （**実レスポンスでのフィールド名は未確認 — 未検証**。送ってこないサーバーではゼロのまま残る設計に
  してある。）

### E. Go の機能

- Go 1.27 の `uuid` パッケージ（`NewV7` は時系列ソート可能）— https://go.dev/doc/go1.27 。
  D1 で採用しなかった。
- 原子的置換は `os.CreateTemp` ＋ `Sync` ＋ `os.Rename`。同一ディレクトリ内の rename は
  POSIX 上アトミックである。

### F. 2026-09-04 に採択された決定

| 決定 | 理由 |
| --- | --- |
| vault は必須。`docdag query --binding` を exec し `as_of` / `at` を書く | 「何を前提に走ったか」が空欄の run は後から解釈できない |
| トレースは `<task>/runs/<run-id>/`。`select` は `--run` 省略時に最新の run を使う | Task が実験の単位であり、Task を配れば履歴も一緒に動く |

## 検討した代替案

- **A. トレースを SQLite / Postgres に入れる。** 不採用。依存が増え（ADR-0002 R2）、
  「ディレクトリを見れば分かる」が失われる。集計が要るなら uzushio が JSON を読んで自分の
  データベースに入れればよい。層の役割としてもそちらが正しい。
- **B. OpenTelemetry でスパンとして送出する。** 不採用（v0）。収集基盤が要る上に、
  トレースの寿命がプロセスの外の可用性に依存する。ファイルなら run と一緒に配れる。
- **C. run-id を UUIDv7 にする。** 不採用（D1）。人が読めない。
- **D. 追記型の JSONL 1 本にする。** 不採用。候補の生応答（数十 KB）と diff を 1 本の行に
  詰めると読みにくく、`verify/<id>/stdout.txt` のような大きな出力の置き場も要る。
  ファイルを分けると `cat` と `jq` だけで読める。
- **E. トレースを読み返して run を再開できるようにする（resume）。** 不採用。
  再開は「前回の状態」を正とする操作で、run が閉じなくなる。やり直しは新しい run である。
- **F. `run.json` を後から更新する（例：`select` の結果を書き足す）。** 不採用（D5）。
  write-once を守ると、あるファイルの内容がいつ確定したかが自明になる。
- **G. run ディレクトリを git 管理下に置く。** 不採用。バイナリに近い大きな出力が毎晩積もる。
  vault（決定記録）と run（観測）は別のライフサイクルを持つ。
- **H. vault を任意にし、無ければ `as_of` / `at` を空にする。** 不採用（D3）。
  「何を前提に走ったか」が空欄の run は、後から解釈できない。

## 影響とトレードオフ

**得るもの**

- run ディレクトリだけを渡せば、他人が run を読める。CMoA も uzushio も要らない。
- `as_of` と `at` により、数か月後でも「その run が見ていたハーネス」を DocDag で再構成できる。
- write-once ＋ 原子的書き込みにより、途中で殺された run も「どこまで進んだか」が正確に残る。

**失うもの・リスク**

- **dirty な vault で走った run は再構成できない。** `at` に `-dirty` が付くのはその事実を
  記録するだけで、内容は復元できない。夜間バッチの前に vault をコミットする運用が要る。
- **`propose` が harness を必須にしたので、DocDag と vault が無い環境では run が始められない。**
  開発中は空の vault（`docdag.yaml` だけ置いたディレクトリ）でも通る。
- **トレースは大きくなる。** 候補ごとに生応答と diff、検証ごとに stdout/stderr を持つ。
  Task ディレクトリの下に無制限に積もり、掃除の仕組みは v0 に無い。
- **`schema_version` を上げる基準が「意味が変わったとき」なので、判断が人に残る。**
  `docs/trace-schema.md` に変更履歴を残して補う。
- **OTel 語彙を採らなかったので、将来の相互運用は写像を書く仕事になる。** 逆に、
  中途半端に借りた名前空間を後から剥がす仕事は発生しない。

## 関連ADR

- 0002（v0 のスコープ）— DocDag を import せず exec するという方針
- 0003（提案者プールとルータ）— `run.json` に写す実効設定と (n, f) の出所
- 0004（候補の表現）— 候補ステータスと `.raw.txt` / `.diff` の中身
- 0005（verifier）— `verify/<id>/result.json` の中身
- 0006（選択規則）— `select.json` の形。`select` が `candidates/` を読むのが D4 の唯一の例外
