---
title: "propose は描かれたハーネスディレクトリをプロンプトに注入し、seed と temperature を run 単位で固定できる"
status: accepted
date: 2026-09-05
depends-on: [0004, 0007, 0008, 0009]
---

# 0010: propose は描かれたハーネスディレクトリをプロンプトに注入し、seed と temperature を run 単位で固定できる

## ステータス

Accepted

採択日: 2026-09-05

## 日付

2026-09-05

## コンテキスト

ADR 0008 は編集可能面を宣言し、0007 は run が読んだハーネス（vault の binding 文書）を run.json に
記録すると決めた。しかし `propose` は binding 文書の**中身**を提案者に見せていない。`propose.go` は
スナップショットを取った後、`prompt.Build(t)` にそれを渡さない。uzushio が Step 5 で edit の効果を測る
には、edit が提案者の受け取るバイト列を変える経路が要る。

自己改善の研究（AHE、Self-Harness、Meta-Harness）はいずれも「ループが編集する成果物 = エージェントが
読み込む成果物」である。CMoA が vault を解釈するのは責務の外（ADR 0007 D3、0008 D4）なので、
**描くのは uzushio、読むのは CMoA** という分担にする。

対応ありの測定（同じ Task をベースラインと候補で回す）には、提案者の seed と temperature を run 単位で
揃える必要がある。設定ファイルは提案者ごとの値を持つが、run ごとに上書きする口がない。

## 決定

### D1. `cmoa propose --harness <dir>`

`<dir>` は uzushio が描いたディレクトリで、次の 3 種だけを読む。

| パス | 描き方 |
| --- | --- |
| `system-prompt.md` | あれば、`system.tmpl` の内容の**後に** `HARNESS` の見出しを付けてそのまま追記する。テンプレートは常に先頭に、無変更で |
| `memory/**/*.md` | user プロンプトの `# Harness` の下に `## Notes` 節を足し、パス順に各ファイルの本文を置く（`.md` 以外は描かないがハッシュには入る）。改行コードを含め中身はそのまま描く——行末は描いた側の責任 |
| `skills/<name>/SKILL.md` | `## Available skills` 節に `- <name>: <description>` を 1 行ずつ、パス順に。description は frontmatter の `description:`、なければ最初の見出しでない非空行。本文は描かない |

空のディレクトリなら今日と同じプロンプト（バイト単位で同一。`internal/prompt/testdata/` の golden で固定）。
ディレクトリが無ければ exit 2、`--harness ""` も exit 2——シェルの変数が空のまま展開されると、候補を測ったつもりで
ベースラインを測ってしまう。

次のものは exit 3 で拒否する。黙って no-op にすると edit が「効かなかった」理由を取り違えるか、
`tree_sha256` が「送られていないバイト列」に約束することになるからである。

- `SKILL.md` ファイルの無い skill ディレクトリ（**空のディレクトリを含む**）、説明の導けない `SKILL.md`、
  `^[a-z0-9][a-z0-9._-]{0,63}$` に合わない skill 名——名前は 1 行のリストに描かれるので、機械が提案した
  名前が行を増やせてはならない
- UTF-8 でないファイル（task のファイルと同じ規則。JSON エンコーダが黙って置換するので、
  ハッシュの約束するバイト列と送られるバイト列がずれる）、通常ファイルでないもの（symlink は辿らず拒否）
- `max_context_bytes` を超える木（D4）

`prompt_version` を上げる。CMoA は vault を読まず、ディレクトリだけを読む。

### D2. run.json に `harness.render` を記録する

CMoA が**自分で読んだ木**から `{dir, tree_sha256, rendered_bytes, files: [{path, sha256}]}` を計算して書く
（`tree_sha256` は `<path>\n<sha256>\n` を path 順に連結した manifest の sha256。`render.json` と、
ディレクトリでもファイルでもある `.git` およびその中身 `.git/**` は除く）。`dir` は `harness.vault` と同じく絶対パスに正規化する
（同じ木を違う書き方で指した 2 つの run が同じ値を残すため）。`rendered_bytes` は 2 つのメッセージに実際に入った
ハーネスのバイト数で、D4 の予算が使う数でもある。uzushio 側の `render.json` は信用せず、照合の対象にする。
既存の `harness.binding`（何が binding か）と並ぶ：前者は「何を読んだか」、後者は「なぜそれが有効だったか」。

ハッシュは**ファイル**についてのものなので、空のディレクトリは digest に届かない。だから空の skill
ディレクトリは照合に任せず D1 で拒否する。

### D3. `cmoa propose --seed <int> --temperature <float>`

すべての提案者の seed と temperature を run 単位で上書きする。実効設定として run.json に残る
（既存の `config` 記録）。対応ありの測定は両方を揃えたときだけ成立する、と README に書く。
`select` は変わらない。`--temperature` は `[0, 2]` の外を exit 2 で拒否する。NaN は 2 つの比較の
どちらも通ってしまうので明示的に弾く——通すと JSON エンコードの段で全提案者が候補エラーになり、
run が拒否されずに終わる。

### D4. ハーネスは task の `max_context_bytes` を共に消費する

`instruction + files + rendered_bytes > max_context_bytes` なら exit 3 で、何も書かずに拒否する。
エラーには両方の数（task の分とハーネスの分、合計、上限）を書く。

Notes 節はモデルの文脈としてはファイルと同じものである。そして `memory` と `skill` は ADR 0008 で
auto-accept の面なので、掘り出されたパターンと際限のない Notes 節のあいだに人間の関門がない。
上限が無いと、失敗の形は「uzushio の run が regress として捕まえる」ではなく「サーバの文脈長を静かに
超えて切り詰められた応答が返り、予算のバグがハーネスの regress として記録される」になる。
上限そのものは task が既に持っているので、新しい設定項目は増やさない。

## 根拠（調査結果・出典）

- AHE 2604.25850 §3.1（ファイルとしての 7 コンポーネントと `code_agent.yaml` による有効化）、
  Self-Harness 2606.09498（宣言された空の面）、ACE 2510.04618（全面書き換えの文脈崩壊）。
- Claude Code の skills はターンごとに名前と説明の一覧が見え、本文は呼び出し時に読まれる——skill を
  一覧で描く理由。
- Miller 2411.00640、Wang 2512.21326：対応あり分析と seed の共有。
- uzushio ADR 0008（Step 5 の設計）。

## 検討した代替案

- **CMoA が vault の binding edit を読んで自分で描く。** 不採用。CMoA は vault を解釈しない（0007、0008）。
- **設定ファイルにハーネスの場所を書く。** 不採用。描かれたディレクトリは run ごと（ベースラインと候補）
  に違うので、フラグでなければならない。
- **skill の本文を描く。** 不採用。実在のハーネスの形と違い、トークンを増やす。
- **system.tmpl を harness に移す。** 不採用（所有者）。テンプレートは seed として無変更で先頭に置く。

## 影響とトレードオフ

- 得るもの：edit が提案者のプロンプトを変える。何を読んだかが run.json に残り、uzushio の render と
  照合できる。対応ありの測定ができる。
- 失うもの：`propose` にフラグが 3 つ増える。プロンプトの形が harness の有無で変わるので、
  `prompt_version` が動く。
- リスク：memory が増えるとプロンプトが伸び、小さいモデルの出力が崩れる。D4 の予算がその上限であり、
  超えた木は run を拒否する。予算の内側でどこから壊れ始めるか（Notes 節の実用的な目安）は
  `rendered_bytes` を残してあるので、測ってから決める。

## 関連ADR

- 0004（候補は diff）、0007（トレース）、0008（面と自律度）、0009（verify）、uzushio ADR 0008
