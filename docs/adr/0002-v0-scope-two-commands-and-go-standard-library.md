---
title: "v0 のスコープをコーディング面の 2 コマンドに限り、Go 標準ライブラリだけで書く"
status: accepted
date: 2026-09-04
---

# 0002: v0 のスコープをコーディング面の 2 コマンドに限り、Go 標準ライブラリだけで書く

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### CMoA が引き受けるもの

CMoA は 3 層構成（DocDag = 地盤、CMoA = 骨格、uzushio = 内装）の骨格層であり、責務は 4 点に限る：
決定論的ルータと提案者プール、選択型集約、ファイルとして残るトレース、編集可能面の宣言。
評価・判定・vault の設定・ハーネスの中身は CMoA に入れない。

この 4 点をどこまで v0 で出すかが本記録の問題である。骨格層は「何を作らないか」を先に固定しないと
範囲が膨らむ。「ミニマル」という語は成果物の形にしか効かず、作業の範囲は別に縛る必要がある。

### コーディング面から始める理由

選択型集約の要は選択器である。2603.20324 は「単一ラウンドの generate-then-select において、
選択器の質は提案者の多様性より効きうる設計レバーである」と結論した。コーディング面はその選択器を
**テスト実行**に置ける。審判 LLM は要らず、較正も要らず、答えは二値で返る。

一方チャット面は審判 LLM の較正を必要とし、較正には評価基盤（uzushio の `run` / `improve`）が要る。
評価が曖昧な領域で自己改善ループを回すと苦戦することは DGM / Hyperagents が明記しており、Cynefin の
Complex 領域には自動改善が届かない。順序は「コーディング面 → 評価基盤 → チャット面」で固定する
（[docs/roadmap.md](../roadmap.md)）。

### 実装言語と依存の問題

CMoA は I/O 中心のオーケストレータである。推論はプロセスの外（llama-server）にあり、CMoA 自身が
するのは HTTP、並行フェッチ、JSON、子プロセス、ファイル I/O だけである。加えて CMoA のコードは
**自己改善ループが編集する対象になりうる**。編集提案を書く能力はモデル階層を通じてほぼ一定である
（2605.30621）が、それは対象が標準的なコードであることを前提にしている。方言化したコードは
その前提を崩す。

### 満たすべき要件

- R1 v0 の成果物は常駐しないコマンドであり、uzushio から呼ばれる
- R2 外部依存ゼロ（`go.mod` の `require` が空である）
- R3 閉じた列挙・N 値の直和・失敗・外部入力由来の ID を型で表し、網羅漏れを機械が指摘する
- R4 uzushio が読む公開 API は 1 パッケージに閉じる
- R5 `go test ./...` が LLM・Docker・DocDag のいずれも無しで通る
- R6 合否の判定を CMoA に持ち込まない（審判 LLM を置かない、終了コードで採否を語らない）
- R7 run の全体が、コードを読まずトレースだけで記述できる
- R8 Go のバージョンは 1.27 系の最新に固定し、リンタもそれを解する版に揃える

## 決定

### D1. v0 はコーディング面のみとする

チャット面（単一審判、盲検、提示順の無作為化と位置スワップ、較正ログ）は v0 の非目標である。
ただし選択結果の直和型には `JudgeTimeout` を最初から置く（ADR-0006 D3）。v0 では生成されないが、
型が後から増えると、それを消費する型スイッチが全部書き換わるためである。

同じく非目標：合成型集約、審判パネル、投票（2502.00674 / 2605.29800 / 2603.20324）、評価と判定、
DocDag の設定生成、ハーネスそのものの内容。

### D2. コマンドは `propose` と `select` の 2 つ。常駐しない

```
cmoa propose --task <dir> [--config <file>] [--as-of YYYY-MM-DD] [--run-id <id>]
cmoa select  --task <dir> [--config <file>] [--run <run-dir>]
cmoa surfaces [--format text|json]
cmoa version
```

`surfaces` と `version` は補助である（前者は ADR-0008 の面を uzushio に渡す口）。HTTP サーバーは
v1（チャット面）で足す。

終了コードは 0 成功 / 1 実行時エラー / 2 使い方エラー / 3 設定・Task 検証エラー。**`select` は
`NoCandidate` でも `VerifierFailed` でも 0 を返す**（R6）。どちらも「run はこう終わった」という事実で
あって CMoA の失敗ではない。結果はトレースにあり、判定は uzushio がする。標準出力には kind を 1 行だけ
書く。ログは stderr にプレーンテキストで 1 行 1 事象。

### D3. Go 1.27.1、標準ライブラリのみ

`go.mod` は `module github.com/Kaikei-e/CMoA` / `go 1.27.1` の 2 行で、`require` を持たない。
使う標準パッケージと用途は次のとおり。

| 用途 | パッケージ |
| --- | --- |
| 提案者への HTTP | `net/http`、`encoding/json` |
| 子プロセス（git / docker / docdag） | `os/exec`（`CommandContext` ＋ `Cmd.Cancel` ＋ `Cmd.WaitDelay`） |
| プロンプト生成 | `text/template` ＋ `embed` |
| 並行 | goroutine、`sync`、`context` |
| トレース | `encoding/json`、`os`、`path/filepath`、`crypto/sha256`、`crypto/rand` |
| CLI | `flag` ＋ 手書きサブコマンド分岐 |
| テスト | `testing`、`net/http/httptest`、`testing/synctest` |

DocDag は Go で書かれているが、**パッケージとして import せず CLI として exec する**（ADR-0007 D3）。
型の共有より依存の切断を採る。

### D4. cobra を採用しない

設計段階で未決として持ち越していた項目を、標準 `flag` ＋手書きのサブコマンド分岐に決める。
サブコマンドは 4 つで入れ子が無く、補完スクリプトの需要も無い。DocDag に揃える利益は、
R2（依存ゼロ）を崩す不利益に見合わない。

### D5. 型表現は素の Go とリンタで得る

| 表したいもの | 書き方 |
| --- | --- |
| 閉じた列挙 | 名前付き文字列型＋定数。網羅性は `exhaustive`（`config.SelectionRule`、`trace.CandidateStatus`、`trace.VerifyStatus`、`cmoa.Surface`、`cmoa.Autonomy`） |
| N 値の直和 | 封印インターフェース＋`//sumtype:decl`＋`gochecksumtype`（`selection.Selection`）。**宣言と型スイッチは同一パッケージに置く** |
| 「無い」 | ポインタ（`Proposer.Temperature`、`Proposer.Seed`、`Candidate.Diff`）または `(T, bool)` |
| 失敗 | `(T, error)` ＋型付きエラー（`*config.ValidationError`、`*llm.HTTPError`、`*llm.DecodeError`、`*patch.ApplyError`、`*verify.RunnerError`、`*worktree.Error`、`*harness.Error`）＋ `errors.AsType` |
| 外部入力から作る ID | 検証を通した newtype（`config.ProposerID`、`task.TaskID`、`trace.RunID`）。`Parse*` が唯一の構築口 |

`.golangci.yml`（v2 スキーマ）は `exhaustive` と `gochecksumtype` を有効にし、両方の
`default-signifies-exhaustive` を `false` に落とす。既定値は前者が `false`、後者が `true` で逆なので、
明示しないと sum type だけ `default:` 一つで網羅扱いになる。golangci-lint は v2.13.0 で go1.27 対応が
入ったので v2.13.2 以上を使う。

同一パッケージ規約は go-check-sumtype の既知の取りこぼし（宣言と型スイッチが別パッケージだと
検出漏れする、golangci-lint #4158）への対処である。

### D6. fp-go を採用しない

`IBM/fp-go` v2 は二値の直和（Option / Either / Result）しか与えず、CMoA が必要とする 4 値の直和は
表せない。加えて Go の方言化はエージェントによる編集の品質を落とす（D5 の前段の理由）。

### D7. パッケージ配置

ルートパッケージ `cmoa` だけが公開 API を持ち（ADR-0008）、実行するものはすべて `internal/` に置く。

```
cmoa.go                  公開パッケージ cmoa（編集可能面の宣言）
cmd/cmoa/main.go         CLI
internal/config          cmoa.json の読み込みと検証        （ADR-0003）
internal/task            task.json + instruction.md + files（ADR-0004）
internal/llm             OpenAI 互換クライアント            （ADR-0003）
internal/prompt          text/template によるメッセージ生成 （ADR-0004）
internal/harness         docdag query --binding の exec     （ADR-0007）
internal/trace           run ディレクトリのスキーマと書き込み（ADR-0007）
internal/propose         propose のオーケストレーション
internal/patch           diff 抽出・統計・git apply         （ADR-0004）
internal/worktree        git worktree add / remove          （ADR-0004）
internal/verify          docker compose run ランナー        （ADR-0005）
internal/selection       Selection 直和型と select          （ADR-0006）
```

## 根拠（調査結果・出典）

### A. MoA の設計原則

- Self-MoA（arXiv 2502.00674）：合成型集約では単一最良モデル×複数サンプルが異種混合を上回る。
  https://arxiv.org/abs/2502.00674
- Selection Bottleneck（arXiv 2603.20324）：異種チーム＋審判選択の対単一モデル勝率 0.810 に対し同種
  チームは 0.512。**合成型は 42 タスク中 0 勝**（Δ_WR = +0.631）。結論は「選択器の質が主要なレバー」。
  https://arxiv.org/abs/2603.20324
- Nine Judges（arXiv 2605.29800）：審判パネルは実精度で独立投票の期待を 8〜22pp 下回り、最良の単一審判が
  全条件でパネルに並ぶか上回る。 https://arxiv.org/abs/2605.29800
- Complementary-MoA（arXiv 2605.24048）：LLM 審判に提案者を採点させる方式は全データセットで低成績で、
  強い審判（GPT-5.2）が弱い審判（Aya）に劣る。 https://arxiv.org/abs/2605.24048
- This Is Your Doge（arXiv 2503.05856）：欺瞞的な提案者 1 体が MoA の出力を壊しうる。
  https://arxiv.org/abs/2503.05856

D1 の「合成しない・投票しない・パネルを組まない」はこの 5 本が支える。コーディング面を先に置く
判断は 2603.20324 の結論の直接の帰結である（選択器を verifier で固めてから提案者を議論する）。

### B. 言語とツールチェーン

- Go 1.27 リリースノート（2026-08-19）：ジェネリックメソッド、`encoding/json` が v2 実装に切り替わり
  既定有効（opt-out は `GOEXPERIMENT=nojsonv2`）、トップレベル `uuid` パッケージ、
  `testing/synctest.Sleep`、`net/http/httptest.NewTestServer`。 https://go.dev/doc/go1.27
- Go 1.26 リリースノート：`errors.AsType[E any](err error) (E, bool)`。 https://go.dev/doc/go1.26
- Go 1.27.1 が 2026-09-04 時点の最新。 https://go.dev/dl/
- golangci-lint v2.13.0（2026-08-19）で go1.27 対応、v2.13.2（2026-08-27）が最新。
  https://github.com/golangci/golangci-lint/releases
- alecthomas/go-check-sumtype と `//sumtype:decl`。宣言と型スイッチが別パッケージだと検出漏れする報告
  （golangci-lint #4158）。 https://github.com/alecthomas/go-check-sumtype
- IBM/fp-go v2（二値の直和のみ）と、初出時の批判「このライブラリで書かれたコードはもはや Go ではない」
  （Hacker News 37171149）。
- Zig 0.16 は 1.0 前で毎リリース破壊的変更。Rust の YAML エコシステムは serde_yaml が 2024-03 に開発終了、
  serde_yml が RUSTSEC-2025-0068（unsound・unmaintained）。どちらも骨格層の土台にしない理由になる。
- 2605.30621「Harness Updating Is Not Harness Benefit」：編集を書く能力はモデル階層を通じてほぼ一定、
  恩恵は非単調で中位モデルが最大。 https://arxiv.org/abs/2605.30621
  （この主張を「Qwen3.5-9B から Claude Opus 4.6 まで横ばい」と具体的なモデル範囲で述べる引用が
  あるが、要旨からは確認できていない — **未検証**。根拠として使うのは「階層を通じてほぼ一定」まで
  とする。）

### C. 2026-09-04 に採択された決定

本記録が記録する、この日の決定：

| 決定 | 理由 |
| --- | --- |
| Go は 1.27 系の最新（1.27.1）を使う | `errors.AsType`（1.26）と `testing/synctest` を前提にでき、golangci-lint v2.13 系が対応済み |
| 公開 API はルートパッケージ `cmoa` の Surface / AllSurfaces / Autonomy のみ | uzushio が読む語彙を 1 パッケージに閉じる（→ ADR-0008） |
| cobra は使わない | 依存ゼロを保つ（D4） |

## 検討した代替案

- **A. v0 から OpenAI 互換の常駐サーバーを出す。** 不採用。常駐すると設定のリロード、ヘルス、
  同時実行の資源制御、そして「今どのハーネスを読んでいるか」の可変状態が生まれる。v0 の目的は
  1 run を完全に記述することであり、プロセス寿命が run より長いと `as_of` / `at` の意味が薄れる。
- **B. チャット面を同時に作る。** 不採用。審判の較正には評価基盤が要り、それは uzushio 側の後続工程で
  ある。評価が曖昧な領域から始めると自己改善ループが回らない。
- **C. cobra を採用して DocDag に揃える。** 不採用（D4）。
- **D. fp-go で Result / Either を使う。** 不採用（D6）。
- **E. Rust または Zig で書く。** 不採用。Rust は推論をプロセス内に持ち込む場合にのみ再検討する
  （llama.cpp バインディング、WASM ホスト）。Zig は 1.0 前。
- **F. コーディング面にも審判 LLM を置き、verifier の結果と併用する。** 不採用。verifier は二値の
  答えを返す最強の選択器であり、そこに LLM の判断を混ぜると、選択の理由がトレースから追えなくなる。
- **G. 列挙と直和型を `go generate` でコード生成する。** 不採用。生成物はエージェントが編集できない
  領域を増やす。リンタで足りる。

## 影響とトレードオフ

**得るもの**

- 出荷判定が単純になる：`propose` と `select` が `examples/task-hello` を端から端まで回れば v0 である。
- 依存ゼロなので、CMoA のコードを読むのに CMoA 以外を読まなくてよい。エージェントが編集する対象として
  これは直接の利点になる（2605.30621 の前提を保つ）。
- 型表現を素の Go に寄せたことで、`internal/*` のどのパッケージも単体で読める。

**失うもの・リスク**

- **標準ライブラリ縛りの結果、YAML が読めない。** 設定は JSON にする（ADR-0003 D2）。DocDag の
  `docdag.yaml` は CMoA が読まないので実害は無いが、フリートの compose を CMoA が解釈することは
  今後もできない。
- **`exhaustive` と `gochecksumtype` は lint であってコンパイラではない。** CI を通さずにマージすると
  網羅漏れが残る。`Makefile` と `.github/workflows/go.yml` でこの 2 つを必須にする。
- **Go 1.27 の `encoding/json` が v2 実装になったため、重複キーや不正 UTF-8 を含む JSON が拒否される。**
  提案者の応答本文は JSON としてサーバーが組み立てるので通常は問題ないが、壊れた本文は
  `malformed` として候補に落ちる（ADR-0004 D4）。この分類が増えたら実装側で緩和を検討する。
- **CLI のフラグ解析を自前で持つ。** サブコマンドが増えると手書き分岐が重くなる。5 つ目のコマンドを
  足す時点で D4 を見直す。
- **本記録は実装と同じ日に採択された。** `cmd/cmoa` と `internal/*`、ルートパッケージ `cmoa` は
  D3・D5・D7 の通りに書かれている。実装が本文と食い違った場合は、記録を編集せずこの節に追記する。

## 関連ADR

- 0001（DocDag の採用）— 本記録を含む決定記録の置き場と検査を定める
- 0003（提案者プールとルータ）— D3 の依存方針の下で設定形式を JSON に決める
- 0004（候補の表現）、0005（verifier）、0006（選択規則）、0007（トレース）、0008（編集可能面）—
  いずれも本記録のスコープと型表現を前提にする
