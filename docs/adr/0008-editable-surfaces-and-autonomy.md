---
title: "編集可能面と自律度をルートパッケージ cmoa が宣言し、公開 API をそれだけに限る"
status: accepted
date: 2026-09-04
depends-on: [0002]
---

# 0008: 編集可能面と自律度をルートパッケージ cmoa が宣言し、公開 API をそれだけに限る

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### 自己改善ループが編集してよいものは決まっている

CMoA の第 4 の責務は「編集可能面の宣言」である。ここでいう面（surface）とは、自己改善ループが
編集を提案してよいハーネスの構成要素を指す。Agentic Harness Engineering（arXiv 2604.25850）は
ハーネスを 7 つの直交する構成要素として実体化した：system prompt、tool description、
tool implementation、middleware、skill、sub-agent configuration、long-term memory。

同じ論文は、**runs ディレクトリ・tracer・verifier・LLM 設定を読み取り専用**にしている。理由は
自明で、ループが verifier を書き換えられるなら、ループは合格の定義を書き換えられる。評価器と
権限制御はループの外に置く。

### 語彙を誰が持つか

この語彙を使うのは 3 者である。CMoA（提案を出すとき）、uzushio（DocDag の `component` フィールドの
`one_of` と `touches` 辺の語彙に落とすとき）、DocDag（`edit_touches_readonly` のようなルールを
評価するとき）。3 者が別々に文字列を書くと、綴りがずれた瞬間に検査が黙って無効になる。

したがって語彙は 1 箇所で定義され、Go の値として export され、残りはそれを読む。

### 自律度は面ごとに違う

すべての面を同じ自律度で扱うのは誤りである。編集の効き方が面ごとに違うことは測られている。
AHE の component-level ablation（Terminal-Bench 2、89 タスク）では、seed の 69.7% に対し
memory のみで 75.3%、tool のみで 73.0%、middleware のみで 71.9% と伸びる一方、
**system prompt のみを差し替えると 67.4% で seed を 2.3pp 下回る**（唯一の回帰）。
論文自身が「利得は tools、middleware、long-term memory に局在し、system prompt ではない。
事実としてのハーネス構造は転移するが、散文レベルの戦略は転移しない」と述べている。

自動化の度合いを段階として宣言するという発想自体は新しくない（SAE J3016 の運転自動化レベル、
Sheridan & Verplank の自動化レベル）。要点は「段階が対象ごとに違ってよい」ことである。

### 満たすべき要件

- R1 面と自律度の語彙は 1 箇所で定義され、文字列としても取り出せる
- R2 読み取り専用のコンポーネントも列挙され、名指しで拒否できる
- R3 外部入力から面を作る経路は 1 つで、検証を通る
- R4 uzushio が読む公開 API はこのパッケージに閉じる
- R5 自律度の変更は記録に残る（黙って上がらない）
- R6 CMoA はハーネスの中身を持たない（面の**宣言**であって実体ではない）

## 決定

### D1. ルートパッケージ `cmoa` が面・自律度・読み取り専用要素を export する

```go
type Surface string

const (
    SurfaceSystemPrompt       Surface = "system-prompt"
    SurfaceToolDescription    Surface = "tool-description"
    SurfaceToolImplementation Surface = "tool-implementation"
    SurfaceMiddleware         Surface = "middleware"
    SurfaceSkill              Surface = "skill"
    SurfaceSubagentConfig     Surface = "subagent-config"
    SurfaceMemory             Surface = "memory"
)

type Autonomy string

const (
    AutonomyAutoAccept    Autonomy = "auto-accept"
    AutonomyHumanApproval Autonomy = "human-approval"
    AutonomyProposeOnly   Autonomy = "propose-only"
)

type ReadOnlyComponent string

const (
    ReadOnlyVerifier    ReadOnlyComponent = "verifier"
    ReadOnlyTracer      ReadOnlyComponent = "tracer"
    ReadOnlyModelConfig ReadOnlyComponent = "model-config"
)

func AllSurfaces() []Surface
func AllSurfaceNames() []string
func EditableSurfaces() []Surface
func ReadOnlyComponents() []ReadOnlyComponent
func (s Surface) Autonomy() Autonomy
func ParseSurface(s string) (Surface, error)
```

文字列形が語彙の正である。`AllSurfaceNames()` は DocDag の `one_of` にそのまま貼れる形
（`[]string`）を返す。`AllSurfaces()` は毎回新しいスライスを返すので、呼び手が並べ替えても
語彙は壊れない。

`cmoa surfaces [--format text|json]` がこれを標準出力に出す。uzushio は Go の値として import しても
よいし、CLI の JSON を読んでもよい。

### D2. 自律度の割当

| 面 | 自律度 | 理由 |
| --- | --- | --- |
| `memory` | `auto-accept` | 追加が加法的で、誤りの影響が局所。AHE の ablation で単独の伸びが最大（+5.6pp） |
| `skill` | `auto-accept` | 同上。ファイルとして独立しており、held-out で効果を測れる |
| `tool-description` | `human-approval` | 記述の変更がツールの呼ばれ方全体に効く。ablation では +3.3pp |
| `middleware` | `human-approval` | 実行経路に割り込む。誤ると全タスクに効く。ablation では +2.2pp |
| `subagent-config` | `human-approval` | 委譲の構造を変える。単独の測定が難しい |
| `system-prompt` | `human-approval` | **最も収穫が薄く、単独編集は回帰の実績がある**（AHE Table 3 で seed を 2.3pp 下回る唯一の面） |
| `tool-implementation` | `propose-only` | 実行される任意のコードである。ループが自分の権限を書き換える経路になりうる |

`auto-accept` は「held-out split を通れば人を介さず受理する」、`human-approval` は
「検証まで自動、受理は人」、`propose-only` は「提案は記録・検証されるが、ループは決して適用しない」
を意味する。段階の名前は暫定である。

`system-prompt` を `human-approval` に置く根拠は、当初「散文だから危険」という直感だったが、
AHE の ablation により「最も収穫が薄く、単独編集は回帰した面である」という測定に置き換えた。

### D3. verifier / tracer / model-config は面ではない

これらは `Surface` ではなく `ReadOnlyComponent` として、別の型で列挙する。同じ型に入れて
自律度 `none` を足す方法もあるが、それでは「面の一覧」を回すコードが読み取り専用要素を
うっかり含める。**型が違えば混ざらない。**

列挙すること自体には目的がある：uzushio が `touches: verifier` と書かれた編集提案を
名指しで拒否できる（DocDag の `edit_touches_readonly` に落とす）。名前が無ければ拒否できない。

CMoA 自身の `internal/verify`（ADR-0005）と `internal/trace`（ADR-0007）が、それぞれ
`verifier` と `tracer` の実体である。CMoA は自分自身のこの 2 つを、ループの編集対象として
提示しない。

### D4. 公開 API はこのパッケージに限る

ルートパッケージ `cmoa` 以外はすべて `internal/` に置く（ADR-0002 D7）。
`internal/selection` の `Selection` も、`internal/trace` の JSON 型も、Go の API としては公開しない。

- uzushio が必要とするのは**語彙**であって、CMoA の内部型ではない。
- トレースの読み取りは**ファイル**を通じて行う（ADR-0007 D8。スキーマは `docs/trace-schema.md`）。
  Go の型を公開すると、uzushio が CMoA のバージョンに Go のコンパイル時依存で縛られる。
  JSON なら言語も版も跨げる。
- 公開面が小さいほど、CMoA の内部は自由に変えられる。骨格層の内部は、まだ何度も変わる。

### D5. `ParseSurface` が唯一の外部入力からの構築口

`Surface` は文字列型なので `Surface("typo")` と書けてしまうが、外部入力（設定ファイル、
DocDag の frontmatter、CLI 引数）から作る経路は `ParseSurface` だけとし、
未知の名前には語彙を並べたエラーを返す。

`(Surface).Autonomy()` は 7 定数以外の値で **panic する**。これは意図的で、そのような値は
このパッケージが作れない以上、呼び手が型変換で偽造したことを意味する。エラーで返すと
「未知の面の自律度は何か」という無意味な分岐が呼び手側に生える。

### D6. 自律度の昇格は supersede で記録する

`memory` を `auto-accept` から降格する、`tool-implementation` を `human-approval` に昇格する——
いずれも本記録を**編集して**行わない。新しい決定記録を書き、frontmatter に `supersedes: [0008]` を
宣言し、本記録の status を `superseded` にする（ADR-0001 が定めた運用であり、DocDag の
`status_drift` がこの対応を検査する）。

理由は、自律度が「いつからそうだったか」を持つ事実だからである。あるトレースを読むとき、
その run の時点でどの面がどこまで自動だったかが分かる必要がある。記録を上書きすると
その情報が消える。

### D7. ケイパビリティ型による型検査は v0 では採らない

面ごとに書き込みケイパビリティを別の型にし、`tool-implementation` への書き込み型を存在させない
ことで、`edit_touches_readonly` を実行時検査から型検査に前倒しする——という設計は魅力的だが、
**v0 では採らない（未検証）**。

理由は、v0 の CMoA が実際にはハーネスを**書かない**ためである（R6）。書くのは uzushio と人で、
CMoA は語彙を宣言するだけである。書き込みの主体が CMoA の中に現れた時点で、この案を再検討する。

## 根拠（調査結果・出典）

### A. ハーネスの構成要素と読み取り専用集合

- Agentic Harness Engineering（arXiv 2604.25850）§3.1 逐語：
  「我々はハーネス H を NexAU フレームワーク上で実体化する。これは単一のワークスペース内の
  固定マウントポイントに、**7 つの直交するコンポーネント型**——**system prompt、tool description、
  tool implementation、middleware、skill、sub-agent configuration、long-term memory**——を
  明示的なファイルとして公開する」。 https://arxiv.org/abs/2604.25850
- 同 §3.3 逐語：「Evolve Agent はハーネスワークスペースの内側にのみ書き込み、
  **runs ディレクトリ、tracer、verifier、LLM 設定は読み取り専用**であり、種となる system prompt は
  削除不可と印付けされる」。D3 はこれと完全に一致する。
- 同 Table 3（component-level ablation, Terminal-Bench 2, 89 タスク）：

  | 変種 | All (89) |
  | --- | --- |
  | seed | 69.7% |
  | + memory only | 75.3% |
  | + tool only | 73.0% |
  | + middleware only | 71.9% |
  | **+ system_prompt only** | **67.4%（seed を 2.3pp 下回る＝唯一の回帰）** |
  | full | 77.0% |

  要旨：「ablation は利得を tools、middleware、long-term memory に局在させ、system prompt には
  局在させない。事実としてのハーネス構造は転移するが、散文レベルの戦略は転移しないことを示唆する」。
  D2 の自律度の並びはこの順序に沿っている。

### B. 権限をループの外に置く

- Lilian Weng「Harness Engineering for Self-Improvement」(2026-07-04)：評価器と権限制御は
  ハーネスを進化させるループの外に置く。ハーネスは複雑な論理を包みつつインターフェースは単純に保つ。
- Meta-Harness（arXiv 2603.28052）：候補ハーネスをディレクトリとして扱い、ソースコード・スコア・
  実行トレースをファイルシステム経由で見せる。 https://arxiv.org/abs/2603.28052
- Dennis & Van Horn（1966）のケイパビリティ：権限を「持っているものだけができる」形で表す。
  D7 が保留した案の出所。
- SAE J3016（運転自動化レベル）と Sheridan & Verplank（1978）の自動化レベル：
  自動化の度合いを段階として宣言する先行例。CMoA の 3 段階はこれらより粗いが、
  「対象ごとに段階が違ってよい」という点を共有する。

### C. 編集の効き方

- 「Harness Updating Is Not Harness Benefit」（arXiv 2605.30621）：harness-updating（有益な更新を
  作る能力）と harness-benefit（更新から性能を得る能力）を分離。更新の品質はモデル階層を通じて
  ほぼ一定、恩恵は非単調で中位モデルが最大。 https://arxiv.org/abs/2605.30621
- SIA（arXiv 2605.27276）：自己改善のスキャフォールド編集に関する研究。
  「スキャフォールド編集はパース・リトライ・ディスパッチ等の衛生面に集中し、領域固有の推論は稀」
  という要約を根拠に使いたくなるが、**この主張は要旨には無く、本文由来と思われる — 未検証**。
  https://arxiv.org/abs/2605.27276

### D. 実装

- `cmoa.go`（ルートパッケージ）が D1〜D5 の実体である。`allSurfaces` は配列、`autonomyOf` は
  マップで、`AllSurfaces()` はコピーを返す。`ParseSurface` は語彙を並べたエラーを返す。
- `cmoa_test.go` が、7 面すべてに自律度が割り当てられていること、`EditableSurfaces()` が
  `tool-implementation` を含まないこと、`ParseSurface` が未知の名前を拒否することを検査する。

### E. 2026-09-04 に採択された決定

| 決定 | 理由 |
| --- | --- |
| 公開 API はルートパッケージ `cmoa` の `Surface` / `AllSurfaces` / `Autonomy` のみ | uzushio が必要とするのは語彙であり、内部型ではない。トレースは JSON を通じて渡す |

## 検討した代替案

- **A. 面と自律度を DocDag の設定（`docdag.yaml` の `one_of`）に直接書く。** 不採用。
  CMoA は DocDag の設定を生成も編集もしない（責務の外）。加えて語彙が YAML の中の文字列になり、
  Go 側から参照できない。
- **B. 語彙を uzushio 側に置く。** 不採用。面はハーネスの構造の話で、ハーネスを回すのは CMoA で
  ある。uzushio は判定層であり、判定対象の語彙を判定者が定義すると、CMoA が知らない面を
  uzushio が要求できてしまう。
- **C. CMoA が 7 面を実際に編集する（ハーネスの中身を持つ）。** 不採用（R6）。CMoA は
  「編集可能面が宣言された対象」としてハーネスを扱うだけで、ハーネスそのものは既存の
  コーディングエージェントの側にある。
- **D. verifier を面に含め、自律度を `propose-only` にする。** 不採用（D3）。
  「提案だけならよい」ように見えるが、verifier の変更提案が記録に載ること自体が、
  合格の定義を交渉可能なものにする。AHE は読み取り専用と明記している。
- **E. 自律度を数値レベル（0〜5）にする。** 不採用。SAE J3016 のような数値は、段階間の距離が
  意味を持つ場合に有効である。3 段階しかなく、しかも「人が承認する／しない」という質的な違いなので、
  名前のほうが読める。
- **F. ケイパビリティ型で読み取り専用面への書き込みを型検査に前倒しする。** 保留（D7、未検証）。
- **G. `Surface` を `int` の iota 定数にする。** 不採用。語彙は文字列として DocDag の frontmatter と
  JSON に出る。文字列型なら変換が要らず、`exhaustive` の網羅性検査も同じように効く。

## 影響とトレードオフ

**得るもの**

- 面の綴りが 1 箇所にある。uzushio と DocDag の語彙がずれたら、`ParseSurface` か
  DocDag の `one_of` 検査のどちらかが落ちる。黙って無効にはならない。
- 自律度が測定に基づいて割り当てられている。特に `system-prompt` を人の承認に置く根拠が
  「散文は怖い」から「単独編集は回帰した」に変わった。
- 公開 API が 6 関数と 3 つの型に収まるので、`internal/` の設計変更が uzushio を壊さない。

**失うもの・リスク**

- **自律度の段階名は暫定である。** `auto-accept` / `human-approval` / `propose-only` の 3 段階が
  実運用に足りるかは分かっていない。「held-out 通過で自動受理」の held-out をどう作るかは
  uzushio 側の未決事項である。
- **`Autonomy()` が panic する。** 型変換で偽造した値を渡すと落ちる。CMoA の外の Go コードが
  `cmoa.Surface("x")` と書けてしまう以上、この panic は防御として弱い（型システムでは止められない）。
  D5 の規約に依存している。
- **7 面は AHE の分類をそのまま採っている。** 別のハーネス（Claude Code、Codex CLI、OpenCode）の
  構造がこの 7 つに素直に対応するとは限らない。対応が崩れたら面を足すのではなく、
  この記録を supersede して分類ごと入れ替える。
- **v0 の CMoA はどの面も編集しない。** 本記録は宣言だけを与える。宣言と実際の編集の間に
  ずれが生じうる（uzushio が語彙を無視して編集を受理する、など）。それを検査するのは
  DocDag のルールであり、CMoA ではない。

## 関連ADR

- 0001（DocDag の採用）— D6 の supersede 運用が依存する記録
- 0002（v0 のスコープ）— 公開 API を 1 パッケージに閉じるという要件（R4）の出所
- 0005（verifier）— `ReadOnlyVerifier` の実体
- 0007（トレース）— `ReadOnlyTracer` の実体。トレースを JSON で渡すという D4 の判断
