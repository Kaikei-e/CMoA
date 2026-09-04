---
title: "候補は unified diff 1 本とし、抽出も適用も 1 段に固定して失敗を候補として残す"
status: accepted
date: 2026-09-04
depends-on: [0002]
---

# 0004: 候補は unified diff 1 本とし、抽出も適用も 1 段に固定して失敗を候補として残す

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### 4〜9B に何を書かせるか

コーディング面の候補は「リポジトリへの変更」である。それを表す形式には少なくとも 4 つの選択肢が
あった：ファイル全文（whole-file）、検索置換ブロック（search/replace）、行番号を持たない独自形式
（OpenAI の V4A 系）、unified diff。

「小型モデルに unified diff は無理」という通念はある。aider 自身が
「弱いモデルはシステムプロンプトに従わない傾向が強く、ほとんどのローカルモデルは aider で動くのが
やっとで、編集エラーはおそらく避けられない」と警告している。

しかしこの通念は**素の `git apply` を前提にした場合にのみ正しい**。git 2.43.0 / GNU patch 2.7.6 で
実際に壊れたパッチを当てて確かめたところ、行数の誤り・開始行番号の誤り・文脈行の空白ずれは
緩和フラグですべて吸収できた。吸収できない誤りは 3 つに限られる。

| # | 誤り | 素の `git apply` | 緩和後 |
| --- | --- | --- | --- |
| 1 | `@@` の行数が違う | ✗ corrupt patch | ✓ `--recount` |
| 2 | `@@` の開始行番号が違う | ✓ そのまま通る（文脈で位置を探す） | — |
| 4 | 文脈行の空白・タブが実ファイルと違う | ✗ patch does not apply | ✓ `--ignore-whitespace` |
| 5 | ハンク端の文脈行が 1 行違う | ✗ | ✓ `-C1`（**不採用**。照合する文脈が 1 行になると別の場所に当たりうる。verifier が拾うとはいえ、誤位置適用の失敗は読み解きにくい。運用で apply_failed が多ければ再検討） |
| 7 | 新規作成で `new file mode` も `index` も無い | ✓ mode 644 で作成される | — |
| 9 | `a/` `b/` 接頭辞なし | ✗ `-p1` が実パスの先頭要素を剥がす | ✓ `diff --git a/x b/x` 行を合成すると git はそこからパスを読む（D2 の 5） |
| **A** | **`@@` 行そのものが無い** | ✗ No valid patches in input | **救済不能** |
| **B** | **末尾改行なしファイルで `\ No newline at end of file` を書き忘れた** | ✗ | **救済不能** |
| **C** | **追加行（`+` 行）のインデントが誤り** | パッチは**通ってしまう** | **救済不能** |

C が設計上もっとも重要である。`--ignore-whitespace` は**文脈行しか緩めない**ので、追加された行は
モデルが書いたままの誤インデントで残る。適用成功と正しさが乖離する唯一のケースであり、
これを拾うのは verifier（ADR-0005）の仕事になる。

### 文脈をどう与えるか

提案者にファイルを探索させる（tool calling でリポジトリを歩かせる）か、必要なファイルを全文
同梱するか。前者はツールコールの契約違反バグを踏む（FastFlowLM 0.9.46 の tool_calls バグ、
Qwen3.5-9B の thinking 時のツールコール異常 Issue #20837）。後者は文脈長を食う。

### 失敗をどう扱うか

再試行してでも候補を作るか、失敗を失敗として記録するか。CMoA は測定基盤の一部であり、
「どの提案者がどの段で何回失敗したか」は uzushio が読むべきデータである。

### 満たすべき要件

- R1 候補の形式は 1 つに固定する（提案者ごとに変えない）
- R2 抽出は決定論的で、同じ応答本文から常に同じ diff が出る
- R3 適用は候補ごとに隔離され、リポジトリの作業ツリーを触らない
- R4 適用の緩和は「吸収できる誤り」に限り、`+` 行の内容を書き換えない
- R5 失敗はすべて候補として残り、再試行で塗り潰されない
- R6 提案者にファイル探索をさせない
- R7 文脈の総量に上限があり、超過は run 開始前に落ちる

## 決定

### D1. 候補は `diff --git` 行を持たない unified diff 1 本とする

プロンプトの出力契約（`internal/prompt` のシステムプロンプト）は次を要求する。

- 応答は ` ```diff ` フェンス 1 個だけ。前後に散文を書かない。
- パスはリポジトリルートからの相対で、`a/` と `b/` を付ける
  （`--- a/pkg/foo/bar.go` / `+++ b/pkg/foo/bar.go`）。
- **`diff --git` 行、`index` 行、`similarity index` 行、ファイルモード行を書かせない。**
  `git apply` に不要であり、新規・削除と組み合わせると `diff --git a//dev/null b/x` のような
  壊れた行を生みやすい（git の diff 形式では、作成・削除でも `diff --git` 行には実パスが入り、
  `/dev/null` が現れるのは `---` / `+++` の 2 行だけ）。
- **`@@` 行の存在は必須。数値の正確さは求めない**（誤りは `--recount` が吸収し、欠落は救えない）。
- ハンク内の各行は空白・`-`・`+` のいずれかで始める。文脈行はバイト単位で写す。
- 追加行のインデントは周囲に合わせる。「これはパッチツールが見ないが、フォーマッタとコンパイラが
  見る」と明記する（誤り C への対処）。
- 末尾改行の無いファイルを編集するときは `\ No newline at end of file` を保つ（誤り B）。
- 新規は `--- /dev/null` ＋ `+++ b/<path>`、削除は `--- a/<path>` ＋ `+++ /dev/null`。
- できないなら空の diff ブロックを返す。

出力契約は**プロンプトの末尾**にも要約を再掲する。形式指示が推論を先取りすると成績が落ちる
（arXiv 2408.02442）ため、契約はユーザメッセージの後ろに置く。

### D2. 抽出規則（`patch.Extract`）

1. `\r\n` と `\r` を `\n` に正規化する。
2. ` ``` ` / ` ```diff ` / ` ```patch ` のフェンスを順に走査し、中身が diff らしいブロックだけを
   拾って順に連結する。**閉じないフェンスは、そこから末尾までを中身とみなす**（`max_tokens` 到達で
   切れた応答を捨てない）。
3. フェンスが 1 つも無ければ、`diff --git ` / `--- <path>` に `+++ ` が続く対 / `Index: ` の
   最初の出現から末尾までを取る。
4. 先頭の散文を落とし、末尾から diff 行でない行を落とす（`+ - 空白 @ \` で始まる行、および
   `diff ` / `index ` / `new file mode` / `deleted file mode` / `similarity index` / `rename ` /
   `old mode` / `new mode` / `Binary files` を diff 行とみなす）。
5. **ヘッダを正規化する**（`normalizeHeaders`）。`---`/`+++` の対ごとに `a/` `b/` 接頭辞と
   タブ以降のタイムスタンプを整え、`diff --git a/<path> b/<path>` 行が無ければ合成する
   （`/dev/null` 側があれば `new file mode 100644` / `deleted file mode 100644` も添える）。
   D1 でモデルに `diff --git` 行を書かせないのはこのためで、書かせないぶん CMoA が機械的に補う。
6. 末尾改行を保証する。
7. 何も残らなければ `patch.ErrNoDiff` → 候補ステータス `no_diff`。

`<think>…</think>` の除去はこの前段（`llm.StripReasoning`）で済んでいる。思考フラグは信用しない：
llama.cpp の `--reasoning-budget 0` はモデルによって黙って効かず、その Issue #20196 は wontfix で
クローズされている。

`patch.ComputeStats` は同じ diff から `files` / `additions` / `deletions` / `sha256` を数え、
`candidates/<id>.json` の `diff` に入れる。ハンク数値の検証はしない（`--recount` の仕事）。

### D3. 適用は git worktree に対して 1 段で行う

```
git worktree add --detach --quiet <dir> <rev>
git apply --recount --ignore-whitespace --whitespace=nowarn -   （stdin から）
git worktree remove --force <dir> && git worktree prune
```

- 候補ごとに `rev`（`task.json` の `rev` を `git rev-parse` した SHA）から worktree を切る。
  **作業ツリーの未コミット変更は見ない。** worktree はオブジェクトストアを共有するので安い。
- `--recount` はハンクの行数を信用せずパッチを見て数え直す。git の公式説明そのものが
  「ハンクヘッダを直さずにパッチを編集した後」を想定しており、LLM 出力のためのオプションである。
- `--ignore-whitespace` は文脈行の空白ずれを許す。`--whitespace=nowarn` は末尾空白の警告を止め、
  トレースの stderr を静かに保つ。
- **3 段の適用ラダー（`git apply --check` → 緩和 → `patch -p1 --fuzz=3`）は v0 では採らない。**
  段を分けて「何段目で通ったか」を `degraded` として残す案はあるが、段を増やすとトレースの語彙が
  増え、緩い段ほど誤適用が verifier に流れる。v0 は 1 段に固定し、段の必要性は `apply_failed` の
  実測が出てから判断する — **この判断は未検証である**。
- `--3way` は期待しない。index の blob ID が要るがモデルは書けない。実測では素の適用に落ちるだけで、
  害も利益も無い。
- `git apply` の子プロセス呼び出しにする理由：Go の `github.com/bluekeyes/go-gitdiff` は
  「fragment の内容と位置がソースと厳密に一致すること」を要求する厳格モードで、ファジー照合を持たない。
  ADR-0002 の依存ゼロ方針とも合わない。

### D4. 再試行はしない。失敗も候補としてトレースに残す

`propose` は 1 提案者に 1 回だけ訊く。結果は必ず `candidates/<proposer-id>.json` になる。

| status | 意味 | 出所 |
| --- | --- | --- |
| `ok` | diff を抽出できた | — |
| `http_error` | 非 2xx、接続拒否、または本文が読めない | `*llm.HTTPError` |
| `timeout` | その提案者の `timeout_seconds` が尽きた | `context.DeadlineExceeded` |
| `malformed` | 2xx だが chat completion ではない | `*llm.DecodeError` |
| `no_diff` | 応答は来たが unified diff が無い | `patch.ErrNoDiff` |

適用の失敗はここには現れない。`select` が worktree に当てて初めて分かるので、
`verify/<id>/result.json` の `apply_failed` として記録される（ADR-0006 D6）。

`candidates/<id>.raw.txt` には応答本文をそのまま（思考除去前）残し、diff を抽出できた場合だけ
`candidates/<id>.diff` を書く。1 体も `ok` にならなくても `propose` はエラーにしない。それは
`select` が `NoCandidate` として記録する事実である。

### D5. 文脈は `task.json` の `files` を全文同梱する

```json
{"version": 1, "id": "hello", "repo": "repo", "rev": "HEAD",
 "files": ["add.go", "add_test.go"], "max_context_bytes": 65536,
 "verify": {"compose_file": "compose.yaml", "service": "verify"}}
```

- `files` はリポジトリルートからの相対パス。**全文をプロンプトに載せる。提案者にファイルを
  探索させない**（2026-09-04 に採択）。ツールコールを使わないので、FastFlowLM や Qwen3.5 の
  ツールコール系バグの影響を受けない。
- ファイルは `## <path>` ＋フェンスで渡し、**行番号を振らない**。行番号を見せるとモデルはそれを
  `@@` に転記しようとして失敗する（OpenAI が V4A で行番号を捨てた判断と同じ）。
- インデント様式は構造化フィールドとして渡す（`Indentation: tabs`）。Go はタブだが、
  小型モデルの事前分布はスペース寄りである。
- verifier のコマンドをプロンプトに見せる。「編集を反証可能な契約にする」（AHE）の最小版で、
  何で採点されるかを提案者に知らせる。
- `instruction.md` と全ファイルの合計が `max_context_bytes`（既定 65536）を超えたら、
  **propose を始める前に**エラーにする。存在しないパス、リポジトリ外に出るパス、
  非 UTF-8 のファイルもすべて読み込み時のエラー（`*task.ValidationError`）。

### D6. 適用成功と正しさの乖離は verifier に拾わせる

誤り C（`+` 行のインデント誤り）は `--ignore-whitespace` で通ってしまう。したがって候補の
verifier コマンド列には**フォーマッタ検査を 1 段目に置くことを推奨する**（Go なら `gofmt -l` と
`go vet`）。これは Task 側の compose が決めることであり、CMoA は強制しない。

`examples/task-hello` の compose は現在 `go test ./...` のみで、フォーマッタ検査を含んでいない
— **この推奨は例題にはまだ反映されていない（未検証）**。例題の目的は end-to-end の配管を通すこと
であり、誤り C を捕まえる力は測っていない。

## 根拠（調査結果・出典）

### A. `git apply` の許容範囲【実測】

- `git apply --recount` の公式説明：「ハンクヘッダの行数を信用せず、パッチを検査して推論する
  （例：ハンクヘッダを適切に調整せずにパッチを編集した後）」。 https://git-scm.com/docs/git-apply
- `-C<n>`：「各変更の前後で少なくとも n 行の文脈が一致することを保証する」。同上。
- git の diff 形式：「作成・削除であっても、`a/` や `b/` のファイル名の位置に `/dev/null` は
  使われない」。 https://git-scm.com/docs/diff-format
- コンテキストの表（#1〜#9、#A〜#C）は git 2.43.0 / GNU patch 2.7.6 で実際に当てて得た結果である。
- go-gitdiff は「fragment の内容と位置がソースと厳密に一致しなければならない」厳格モード。
  https://pkg.go.dev/github.com/bluekeyes/go-gitdiff/gitdiff

### B. 出力形式と思考モードの文献

- 「Let Me Speak Freely?」（arXiv 2408.02442、EMNLP 2024 Industry Track）：形式制約下で推論能力が
  有意に落ちる。ただし機序は「形式が推論を先取りしたこと」（GPT-3.5 Turbo の JSON モード応答の
  100% が `answer` キーを `reason` キーより前に置いた）。緩和策 NL-to-Format は無制約と同等の性能を
  保った。 https://arxiv.org/abs/2408.02442
- 「Thinking Before Constraining」（arXiv 2601.07525）：自由推論 → トリガ → 構造化出力の 2 相構成で
  最大 +27%、1.5B〜4B の小型モデルで特に効く。 https://arxiv.org/html/2601.07525v2
- 「The Danger of Overthinking」（arXiv 2502.08235）：SWE-bench Verified の 4,018 トラジェクトリ分析。
  overthinking スコアが高いほど性能が落ちる。「思考を禁じる」ではなく「思考に上限を置く」根拠。
  https://arxiv.org/abs/2502.08235
- aider の編集エラーに関する文書：弱いモデル・量子化モデルはシステムプロンプトに従いにくい。
  編集エラーが出るなら `--edit-format whole` に落とせ。 https://aider.chat/docs/troubleshooting/edit-errors.html
- aider polyglot leaderboard の well-formed 率は推論モデルでも 97〜100% で、
  「推論モード一般が形式を壊す」という命題は成立しない。 https://aider.chat/docs/leaderboards/
- llama.cpp Issue #20196（`--reasoning-budget 0` が黙って効かない、wontfix）—
  https://github.com/ggml-org/llama.cpp/issues/20196 。D2 の「思考タグを常に除去する」の根拠。
- llama.cpp Issue #20516（Qwen3.5-9B の応答が `</think>` で始まる）—
  https://github.com/ggml-org/llama.cpp/issues/20516 。開始タグの無い思考ブロックが実在する。

### C. 2026-09-04 に採択された決定

| 決定 | 理由 |
| --- | --- |
| Task 形式は CMoA が最小形式を定義し、上位層が後で寄せる | 骨格層が先に走るため。`task.json` は 7 キーしか持たない |
| 候補は unified diff。git worktree に apply | 緩和フラグ付きの `git apply` が小型モデルの典型的な誤りを吸収できると実測できたため（付録の表） |
| 文脈は `task.json` の `files` を全文同梱する | 提案者にファイル探索をさせない。小型モデルのツールコールはランタイム側の不具合が多い |
| 再試行なし。失敗も候補としてトレースに残す | 再試行は「その提案者が何回に 1 回成功するか」という測定を壊す |

## 検討した代替案

- **A. whole-file（ファイル全文を返させ、CMoA が `git diff` を取る）。** 不採用（v0 では）。
  差分構文の失敗が構造的に起こり得ないという強い利点があり、aider の推奨フォールバックでもある。
  ただしトークン代が増え、`max_context_bytes` の設計が変わる。**udiff で verifier 通過率が
  頭打ちになったときの第一のフォールバックとして保留する。**
- **B. search/replace ブロック形式。** 不採用。`@@` を持たないので `--recount` の恩恵が無く、
  照合の緩和を CMoA が自分で実装することになる（＝厳密照合の再実装）。
- **C. diff を JSON 文字列のフィールドに入れる（`{"diff": "..."}`＋ json_schema）。** 不採用。
  `\n` エスケープが大量に発生して小型モデルの失敗率が上がり、加えて文法制約と思考モードは相性が
  悪い（vLLM は構造化出力エンジンが reasoning の存在を見て構造化をスキップすると明記）。
- **D. `go-gitdiff` で自前適用する。** 不採用。ファジー照合が無く、依存も増える。
  ただし**パース・統計だけを go-gitdiff で行い、適用は `git apply`** という分担は現実的で、
  依存ゼロ方針が緩んだ場合の再検討対象である。v0 は統計も自前で数える（`patch.ComputeStats`）。
- **E. `git apply --3way` を使う。** 不採用（D3）。
- **F. 抽出や適用に失敗したら温度を上げて再試行する。** 不採用（2026-09-04 に採択：再試行なし）。再試行は
  「その提案者はこのタスクで何回に 1 回成功するか」という測定を壊す。試行回数はタスクの側で稼ぐ。
- **G. tool calling でリポジトリを探索させる。** 不採用（D5）。小型モデルのツールコールは
  ランタイム側のバグが多く、探索の是非をタスクごとに再現するのが難しい。
- **H. 推論相と差分相の 2 コール構成にする。** 不採用（v0 では）。arXiv 2601.07525 は小型モデルで
  最大 +27% を報告しており、伸ばし代としては最有力である。v0 は差分相のみの 1 コールで始め、
  通過率が頭打ちになってから足す。

## 影響とトレードオフ

**得るもの**

- 候補の形が 1 つなので、`select` は「diff を worktree に当てる」以外のことを知らなくてよい。
- 失敗が候補として残るので、提案者ごとの失敗様式（`no_diff` が多いのか `apply_failed` が多いのか）を
  uzushio がそのまま集計できる。プロンプト改善の指標がトレースから直接出る。
- 抽出が決定論なので、同じ `raw.txt` から常に同じ `.diff` が出る。トレースを読み直して再抽出できる。

**失うもの・リスク**

- **適用が 1 段なので、`-C1` や `patch --fuzz` なら通ったはずの候補を落とす。** 落とした事実は
  `apply_failed` として残るので、後から段を足す判断はデータに基づいてできる。
- **誤り C（`+` 行のインデント誤り）は CMoA では検出できない。** verifier のコマンド列に
  フォーマッタ検査を置くかどうかは Task 作者に委ねられており、置き忘れると
  「テストは通るが `gofmt` が落ちるコード」が選ばれる。
- **文脈を全文同梱するので、大きなファイルを含む Task は作れない。** `max_context_bytes` 既定 65536 は
  16k コンテキストのモデル 3 体を前提にした値で、ファイルが増えれば run 開始前にエラーになる。
  タスクを小さく切ることを強制する制約でもある。
- **`--ignore-whitespace` は文脈行の空白を緩めるので、空白が意味を持つ言語（Python、YAML）の
  タスクでは誤適用の余地が残る。** v0 の対象は Go であり、この点は Go 以外に広げるときに再検討する。
- **フェンスが閉じない応答を採用する規則は、`max_tokens` 到達で切れた diff を「途中まで」適用しうる。**
  途中で切れた diff はハンクが壊れているので通常は `apply_failed` になるが、
  たまたま最後のハンクが完結していれば部分適用が通る。その場合は verifier が落とす。

## 関連ADR

- 0002（v0 のスコープ）— 依存ゼロが D3 の「`git apply` を子プロセスで呼ぶ」を決めている
- 0003（提案者プールとルータ）— D1 が受け取るテキストの出所
- 0005（verifier）— D6 の乖離を拾う場所、および適用済み worktree を渡す先
- 0006（選択規則）— `apply_failed` を候補の失敗として扱う
- 0007（トレース）— 候補ステータスと `.raw.txt` / `.diff` の置き場
