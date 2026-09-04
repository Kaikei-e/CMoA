---
title: "選択規則は設定順の最初の通過者とし、結果を封印された Selection 直和型で返す"
status: accepted
date: 2026-09-04
depends-on: [0002, 0004, 0005]
---

# 0006: 選択規則は設定順の最初の通過者とし、結果を封印された Selection 直和型で返す

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### 集約は選択であって合成ではない

MoA の集約者に候補を合成させる方式は、2026 年の証拠では支持されない。
2603.20324 は、審判による選択が MoA 型の合成を Δ_WR = +0.631 で上回り、
**合成型は 42 タスク中 0 タスクでしか選ばれなかった**と報告している。パネル投票も同様で、
9 審判のパネルは実精度で独立投票の期待を 8〜22pp 下回り、最良の単一審判がパネルに並ぶか上回る
（2605.29800）。したがって CMoA は選ぶだけで、合成も投票もしない（ADR-0002 D1）。

コーディング面では選択器が verifier である。候補が複数通過したとき、どれを採るかという規則だけが
残る問題になる。

### 同率をどう割るか

verifier は二値しか返さない。2 体が通過したら、両者は verifier から見て等価である。
差をつける材料としては diff の小ささ、応答の速さ、提案者の過去成績などが考えられるが、
どれも v0 では測っていない。

### 最初の 1 体が通ったら残りを打ち切りたくなる

打ち切れば時間が節約できる。しかしその瞬間、残りの候補の結果は**未観測**になる。
2606.27288 の上界（合格率 ≤ 1 − β、β = 全提案者が同時に失敗する率）を uzushio が推定するには、
「その run でどの候補も通らなかった」を数える必要がある。早期打切りをすると、通過した run では
残りの候補が観測されず、提案者ごとの通過率も、ペアの誤答相関も、条件付きの偏った標本になる。

これは測定の設計であって最適化の問題ではない。CMoA は測定基盤の一部である。

### 結果の型

`select` の結果は 4 通りある：選ばれた、誰も通らなかった、審判が時間切れになった（チャット面）、
verifier 自体が動かなかった。これを `(string, error)` で返すと、呼び手は「空文字列は
NoCandidate か VerifierFailed か」を文字列で見分けることになる。

### 満たすべき要件

- R1 選択規則は決定論で、同じ検証結果から常に同じ候補が選ばれる
- R2 候補は合成されず、投票もされない
- R3 通過した候補が複数あるとき、選ばれなかった通過者も記録に残る
- R4 早期打切りをせず、全候補の検証結果が揃う
- R5 結果は 4 値の直和として型で表され、網羅漏れを機械が指摘する
- R6 verifier 基盤の失敗が候補の失敗として集計されない
- R7 1 つの run が二度選択されない

## 決定

### D1. 規則 `first`：設定順で最初に verifier を通った候補

`cmoa.json` の `selection.rule` は v0 では `"first"` のみ。`config.SelectionRule` は名前付き文字列型
の列挙で、将来 `"smallest"`（最小 diff）などを足す余地を型として残す（`exhaustive` が、足した瞬間に
書き換えるべき switch を全部指す）。

「設定順」は `cmoa.json` の `proposers` 配列の順であり、それが `select.json` の `order` に写る
（ADR-0003 D3）。選択の理由は `Selected.Reason` に
`"first passing candidate in configured order"` と書く。

同率を diff の小ささや応答の速さで割らないのは、それらが「良さ」の代理指標として検証されていない
ためである。設定順は**恣意的だが説明可能**で、順序を変えたら結果が変わるという事実が
`order` から読める。

### D2. 全候補を検証し切り、`also_passed` を残す

最初の pass が出ても残りの検証を続ける。`select.json` は次を持つ。

```json
{"rule": "first",
 "order": ["granite", "qwen", "ministral"],
 "selection": {"kind": "selected", "candidate_id": "qwen", "reason": "..."},
 "also_passed": ["ministral"],
 "max_parallel": 1}
```

理由は 3 つ。

1. **β の推定**（2606.27288）。「どの候補も通らなかった run」の割合を数えるには、通った run でも
   全候補の結果が要る。合格率 ≤ 1 − β は uzushio が最初に出すべき数値である。
2. **提案者ペアの誤答相関**（補助指標）。uzushio が算出するには候補ごとの pass/fail 行列が要る。
3. **提案者ごとの通過率**。プールの入れ替えを判断する材料。

コストは「候補数 × verifier 時間」で、`max_parallel` が 1 なら候補 3 体で 3 倍になる。
夜間バッチを前提に許容する。

### D3. `Selection` は封印された 4 変種の直和型

```go
//sumtype:decl
type Selection interface{ sealed() }

type Selected struct { CandidateID config.ProposerID; Reason string }
type NoCandidate struct { Tried int }
type JudgeTimeout struct { After time.Duration }
type VerifierFailed struct { Err error }
```

- 宣言・`sealed()` の実装・型スイッチ（`Record`）はすべて `internal/selection` に置く。
  go-check-sumtype は宣言と型スイッチが別パッケージだと取りこぼす（golangci-lint #4158）。
- `JudgeTimeout` は**チャット面用で、v0 では生成されない**。それでも今宣言するのは、
  変種を後から足すと `Record` 以外の型スイッチも一斉に書き換わるためで、
  型が完全であることを最初に固定しておく。
- `VerifierFailed{Err}` は verifier 基盤そのものの失敗（`*verify.RunnerError`、
  worktree の作成失敗、トレースの書き込み失敗）を運ぶ。**候補について何も語らない**（R6）。
- `NoCandidate{Tried}` の `Tried` は設定上の提案者数、つまり `order` の長さである
  （`no_diff` で候補にならなかったものも含む）。

`Record(Selection) trace.SelectionRecord` が JSON 形に写す。`trace.SelectionKind` は
`selected` / `no_candidate` / `judge_timeout` / `verifier_failed` の 4 値。

### D4. `select` は 1 つの run につき 1 回

`select.json` が既にあれば `select` はエラーを返し、何も書かない。
run ディレクトリは append-only であり、上書きしない（ADR-0007 D5）。

再検証したければ新しい run を作る。`propose` をやり直さずに検証だけをやり直す仕組みは v0 に無い。
「同じ候補を別の日に検証したら結果が変わった」を表現するには 2 つの run が要る、という立場である。

### D5. `select` は判定しない

`NoCandidate` も `VerifierFailed` も**終了コード 0** で終わる（ADR-0002 D2）。標準出力に
kind を 1 行だけ書く。run がどう終わったかは事実であり、それを「失敗」と呼ぶかは uzushio の判断で
ある。CMoA が非 0 を返すのは、設定・Task が壊れているか、トレースが書けなかったときだけである。

### D6. `apply_failed` は候補の失敗であり、基盤の失敗ではない

`select` の 1 候補あたりの手順と、その失敗の行き先：

| 段 | 失敗したときの `verify/<id>/result.json` | 選択への影響 |
| --- | --- | --- |
| `git worktree add` | `runner_error` | `VerifierFailed` |
| `git apply` | `apply_failed`（`apply_error` に stderr） | 通過候補にならないだけ |
| `docker compose run`（基盤の失敗） | `runner_error` | `VerifierFailed` |
| コンテナが非 0 で終了 | `fail` | 通過候補にならないだけ |
| タイムアウト | `timeout`（`exit_code: -1`） | 通過候補にならないだけ |
| 終了コード 0 | `pass` | 通過候補 |
| 候補の status が `ok` でない | `skipped` | 検証しない |

`apply_failed` を候補側に置くのは、diff を書いたのは候補だからである（ADR-0004 D3 が
`--recount --ignore-whitespace` まで緩めた上でなお通らなかった、という事実になる）。

`skipped` を書くのは、`propose` が `no_diff` などで候補を作れなかった提案者についても
`verify/` に痕跡を残すためである。「その run で誰が何をしたか」がディレクトリを見るだけで分かる。

### D7. `select` は同じ run の `candidates/` だけを読む

CMoA はトレースを書くだけで読み返さない（ADR-0007 D4）。唯一の例外がここで、`select` は
`propose` が同じ run ディレクトリに残した `candidates/<id>.json` と `<id>.diff` を読む。
過去の run、他の Task、uzushio の判定結果は読まない。

読み込み時に `run.json` の `task.id` と、引数で与えられた Task の `id` が一致することを確認する。
別の Task の run ディレクトリを指した場合はエラーである。

## 根拠（調査結果・出典）

### A. 選択型集約の文献

- Selection Bottleneck（arXiv 2603.20324）：「審判による選択は MoA 型の合成を Δ_WR = +0.631 で上回る。
  審判パネルは 42 タスクのうち 0 タスクでしか合成をベースラインより好まなかった」。
  結論は「単一ラウンドの generate-then-select において、選択器の質は生成側の多様性より大きな
  設計レバーでありうる」。 https://arxiv.org/abs/2603.20324
  CMoA のコーディング面は選択器をテスト実行に置き換えており、この結論の側に立つ。
- Nine Judges（arXiv 2605.29800）：パネルの実精度は独立投票の期待を 8〜22pp 下回り、
  「最良の単一審判が全条件でパネルに並ぶか上回る」。投票を採らない根拠。
  https://arxiv.org/abs/2605.29800
- Co-Failure Ceiling（arXiv 2606.27288）：「出力がメンバのいずれかの答えであるどの方策も、
  精度は 1 − β を超えない」「利得はモデルが異なる問題で失敗することから来る」。
  D2（全候補を検証し切る）の直接の根拠。 https://arxiv.org/abs/2606.27288
- Self-MoA（arXiv 2502.00674）：合成型集約の弱さ。 https://arxiv.org/abs/2502.00674
- Condorcet の陪審定理は投票者の独立性を前提とする。独立性が崩れた集団での投票は定理の外にある。

### B. 型表現

- alecthomas/go-check-sumtype：`//sumtype:decl` で宣言した封印インターフェースに対し型スイッチの
  網羅性を検査。golangci-lint に v1.55 から収録。宣言と型スイッチが別パッケージだと検出漏れする
  報告（golangci-lint #4158）。 https://github.com/alecthomas/go-check-sumtype
- golangci-lint の `gochecksumtype.default-signifies-exhaustive` の既定は `true` で、
  `default:` があるだけで網羅扱いになる。CMoA は `false` に落としている（ADR-0002 D5）。
  https://golangci-lint.run/docs/linters/configuration/
- IBM/fp-go v2 は二値の直和しか持たず、4 値の `Selection` は表せない（ADR-0002 D6）。

### C. 利用者の決定（2026-09-04）

| # | 決定 |
| --- | --- |
| 6 | 同率は設定順で最初の通過者 |
| 11 | トレースは `<task>/runs/<run-id>/`。select は `--run` 省略時に最新 |
| 12 | 検証並列度は `max_parallel`、既定 1 |

## 検討した代替案

- **A. 通過した候補を合成して 1 本の diff にする。** 不採用。2603.20324 の合成型 0 勝と、
  そもそも「2 つの diff を意味的にマージする」実装が verifier より複雑になるという実務上の理由。
  合成物は誰も検証していないコードでもある。
- **B. 通過した候補を審判 LLM に選ばせる。** 不採用。verifier が二値の答えを返している上に、
  審判を足すと選択理由がトレースから追えなくなり、審判の較正という別の問題が生える。
- **C. 投票（複数 verifier、複数実行の多数決）。** 不採用（2605.29800、Condorcet の前提破れ）。
  同じテストを 3 回走らせても独立ではない。
- **D. 最初の pass で残りを打ち切る。** 不採用（D2）。時間は節約できるが、
  β と通過率の標本が条件付きに偏る。CMoA は測定基盤の一部である。
- **E. 同率を diff の小ささで割る（`smallest`）。** 不採用（v0 では）。小ささが良さの代理指標である
  という証拠が無い。`SelectionRule` の列挙に後から足せる形にしてある。
- **F. 同率を無作為に割る。** 不採用。決定論を捨てる代わりに得るものが無い。
  順序バイアスを避けたいのはチャット面（審判に候補を見せる場合）であって、
  候補を読まない verifier には順序バイアスが無い。
- **G. `NoCandidate` や `VerifierFailed` で非 0 の終了コードを返す。** 不採用（D5）。
  CMoA が判定を持つことになる。CI の赤緑は uzushio が決める。
- **H. `Selection` を `(candidateID string, err error)` や単一の enum で表す。** 不採用。
  `VerifierFailed` はエラーを、`NoCandidate` は試行数を、`JudgeTimeout` は経過時間を運ぶ。
  変種ごとに違うデータを持つのが直和型を選ぶ理由である。
- **I. `select` を冪等にし、二度目は上書きする。** 不採用（D4）。run は append-only である。

## 影響とトレードオフ

**得るもの**

- 選択が完全に説明できる：`order` と各 `verify/<id>/result.json` を見れば、なぜその候補が選ばれたかが
  一意に再構成できる。LLM の判断が一切挟まらない。
- `also_passed` により「verifier から見て等価な候補が他にもあった」ことが残る。これは
  タスクの弁別力が弱い（テストが緩い）ことの兆候でもあり、uzushio が Task の質を測る材料になる。
- 型スイッチの網羅漏れが lint で落ちるので、変種を足したときの影響範囲が機械的に分かる。

**失うもの・リスク**

- **全候補を検証し切るので、v0 の `select` は最悪ケースで `候補数 × timeout_seconds` かかる。**
  既定では 3 × 600 秒 = 30 分。`max_parallel` を上げれば縮むが、その分タイムアウトの意味がぶれる。
- **設定順が結果を決めるので、`proposers` の並べ替えが「実験条件の変更」になる。** これを
  忘れると、順序を変えただけの run を同じ条件として比較してしまう。`run.json` に実効設定を
  丸ごと写してあるので、後から検出はできる。
- **`first` は「良い候補」を選ばない。「通った候補のうち先頭のもの」を選ぶだけである。**
  verifier が緩ければ緩い候補が採られる。verifier の強さが選択の質の上限になる、という構造は
  ADR-0005 の帰結でもある。
- **`JudgeTimeout` は v0 では到達不能な変種である。** `Record` に case はあるが、
  実行時には現れない。チャット面が来るまで、この case はテストでしか通らない。
- **`VerifierFailed` は最初に観測した基盤エラー 1 つだけを運ぶ。** 3 候補すべてで docker が失敗した
  場合、`select.json` に残るのは 1 つ目のエラー文字列である。個々の失敗は
  `verify/<id>/result.json` の `runner_error` に残るので、情報は失われない。

## 関連ADR

- 0002（v0 のスコープ）— 直和型の書き方と、判定を持たないという方針
- 0003（提案者プールとルータ）— 「設定順」の出所
- 0004（候補の表現）— `apply_failed` の意味
- 0005（verifier）— `RunnerError` と `fail` の区別、全候補を検証するコスト
- 0007（トレース）— `select.json` の形と write-once の規則
