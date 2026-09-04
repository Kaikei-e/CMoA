---
title: "v0 のスコープに verify コマンドと task.json v2 を足し、Go 標準ライブラリだけで書く決定を引き継ぐ"
status: accepted
date: 2026-09-05
supersedes: [0002]
depends-on: [0004, 0005]
---

# 0009: v0 のスコープに verify コマンドと task.json v2 を足し、Go 標準ライブラリだけで書く決定を引き継ぐ

## ステータス

Accepted

採択日: 2026-09-05

本記録は ADR-0002 を supersede する。0002 の決定 D1・D3〜D7 は文言を変えずに引き継ぎ、D2（コマンドの
一覧）だけを改める。0003〜0008 が宣言する `depends-on: [0002]` は当時の依存の記録としてそのまま残す。

## 日付

2026-09-05

## コンテキスト

### 0002 が決めたこと、その後に分かったこと

0002 は v0 をコーディング面の `propose` と `select`（補助として `surfaces` と `version`）に限り、
Go 1.27 と標準ライブラリだけで書き、型表現を素の Go とリンタで得ると決めた。それは 2026-09-04 に
出荷され、翌日 uzushio が Step 2（vault の設定を Go で組む）を終えた。

Step 3 は uzushio の `task doctor` である。verifier が信用できるかを、既知の正解（reference solution）
に verifier が偽陽性を出さないこと、注入した欠陥（mutant）を verifier が殺せることで測る。これには
「任意の diff を Task の verifier で 1 回検証し、結果を返す」口が要る。

### 口をどこに置くか

verifier の実体は CMoA の `internal/verify`（ADR-0005：Task の compose を候補ごとに固有プロジェクト名で
`docker compose run` する）であり、CMoA ADR-0008 D3 はそれを **読み取り専用の要素** `verifier` として
名指ししている。uzushio が同じものを再実装すれば実体が 2 つになり、片方だけが直った瞬間に
「uzushio が測った verifier」と「CMoA が候補を選んだ verifier」が別物になる。verifier の健全性を測る層が、
測る対象と違う実装で測ることになる。

一方 CMoA の Go パッケージは `internal/` にあり、公開 API はルートパッケージの語彙に限る（ADR-0008 D4）。
uzushio が使える口は CLI だけである。

### Task に何が足りないか

`task.json` v1 は instruction・files・rev・verify（compose と service）を持つ。健全性の測定に要るのは
それに加えて、全 grader を通る正解の形（reference solution）、注入する欠陥の一覧（mutants）、判定の
閾値（kill rate の下限、reference の再実行回数）である。設計文書はこれらを Task マニフェストの必須要素と
していた。

## 決定

### D1. v0 はコーディング面のみとする（0002 D1 を引き継ぐ）

チャット面（単一審判、盲検、提示順の無作為化と位置スワップ、較正ログ）は非目標のまま。選択結果の
直和型に `JudgeTimeout` を置くことも変えない（ADR-0006 D3）。

### D2. コマンドは `propose`、`select`、`verify` の 3 つ。常駐しない（0002 D2 を改める）

```
cmoa propose --task <dir> [--config <file>] [--as-of YYYY-MM-DD] [--run-id <id>]
cmoa select  --task <dir> [--config <file>] [--run <run-dir>]
cmoa verify  --task <dir> --diff <file> [--config <file>] [--out <dir>] [--timeout <dur>] [--label <name>]
cmoa surfaces [--format text|json]
cmoa version
```

`verify` は 1 つの diff を、`select` が候補に対して行うのと同じ経路——`rev` の git worktree、`git apply`、
Task の compose を固有プロジェクト名で `docker compose run`——で 1 回検証し、結果を **JSON 1 オブジェクト**
として標準出力に書く。`status` の語彙はトレースの `verify/<id>/result.json` と同じ
（`pass` / `fail` / `apply_failed` / `timeout` / `runner_error`）。`--out` を与えれば `result.json`、
`stdout.txt`、`stderr.txt` をそのディレクトリに書く。`<task>/runs/` には書かない。run のトレースは
`propose` と `select` のものであり、単発の検証は uzushio が自分の記録（`doctor/<run-id>/`）に保存する。

終了コードは 0 pass / 1 fail・apply_failed・timeout / 2 使い方・Task エラー（標準出力なし）/ 3 runner_error
（JSON は書く）。`--label` は compose のプロジェクト名に入るので `^[a-z0-9][a-z0-9_-]{0,63}$` に限り、外れれば exit 2。
`select` と異なり `verify` は結果を終了コードに写す。`select` の R6（採否を終了コードで語らない）は
「run の結末は CMoA の失敗ではない」という理由からで、`verify` の呼び手は uzushio であり、その判定
（kill rate、偽陽性）を組み立てるのは uzushio である。`verify` 自身は 1 回の検証の事実を返すだけで、
採否を決めていない。

`surfaces` と `version` は補助のまま。HTTP サーバーは v1（チャット面）で足す。

### D3. task.json は version 2 を受ける。v1 は変えない

v2 は v1 に次を足す。すべて任意。

| キー | 内容 |
| --- | --- |
| `verify.kind` | `exit-code`（既定）または `band`。`band` は語彙として予約するだけで、指定されたコマンドは「未実装」として exit 2 で拒否する |
| `verify.timeout_seconds` | Task 側のタイムアウト。フラグ > Task > 設定の順で効く |
| `reference.diff` | 全 grader を通る正解の unified diff（`rev` に対して）。候補と同じ形 |
| `mutants[]` | `diff`（正解に対して当てる unified diff）、`expect`（`killed` / `equivalent`、既定 `killed`）、`origin`（`hand` / `generated`、既定 `hand`）、`operator`、`note` |
| `doctor` | `kill_rate_min`（既定 0.8）、`reference_runs`（既定 3） |

`DisallowUnknownFields` は残す。v1 のファイルは今までどおり読める。CMoA は `reference` と `mutants` を
**読むだけ**で、mutant を作らず、kill rate も計算しない。それは uzushio の `task mutate` と `task doctor`
の仕事である。CMoA がスキーマを持つのは、Task の定義は CMoA が読む `task.json` にあるべきで、
uzushio が別のマニフェストを重ねると「同じ Task」が 2 つの定義を持つためである。

`band` を予約だけする理由は、帯域判定型の grader（測定値が [lo, hi] に入るか）が実在する
（性能ゲート）一方で、それを容れる Task がまだこのリポジトリにないからである。語だけ先に置くのは、
`exit-code` が「唯一の種別」ではなく「既定の種別」であることを v2 の読み手に示すためである。

### D4. Go 1.27、標準ライブラリのみ（0002 D3 を引き継ぐ）

`go.mod` は `require` を持たない。`verify` が新たに使うものは無い（`internal/worktree`、`internal/patch`、
`internal/verify`、`internal/trace` の既存の部品を組み合わせる）。

### D5. cobra を採用しない（0002 D4 を引き継ぐ）

サブコマンドは 5 つで入れ子が無い。標準 `flag` と手書きの分岐のまま。

### D6. 型表現は素の Go とリンタで得る（0002 D5 を引き継ぐ）

`verify.kind`、`mutants[].expect`、`mutants[].origin` は名前付き文字列型＋定数で、`exhaustive` が
網羅性を見る。外部入力（task.json）からの構築は `Parse*` または `Load` の検証を通る。

### D7. fp-go を採用しない（0002 D6 を引き継ぐ）

### D8. パッケージ配置（0002 D7 を引き継ぐ）

`verify` は `cmd/cmoa` に分岐を足し、`internal/task` に v2 のフィールドを足すだけで、新しいパッケージは
増やさない。

## 根拠（調査結果・出典）

- ADR-0002 の根拠は本記録が引き継ぐ（2603.20324、2605.30621、DGM / Hyperagents、golangci-lint #4158、
  fp-go v2 の二値直和）。
- ADR-0005：verifier の実体と `docker compose run` の経路。ADR-0008 D3：verifier は読み取り専用の要素。
- uzushio 設計（P4 verifier 健全性）：kill rate と偽陽性率で verifier の健全性を測り、閾値未満の verifier は
  「無い」とみなす。Task マニフェストの必須要素として reference solution を挙げる。
- ミューテーションテストの語彙（PIT、Stryker、gremlins）：killed / survived / timeout / equivalent。
  uzushio 側の記録（uzushio ADR 0005）が詳細を持つ。

## 検討した代替案

- **uzushio が compose runner を再実装する。** 不採用。verifier の実体が 2 つになり、健全性を測る層が
  測る対象と別の実装で測ることになる。
- **CMoA のルートパッケージに `task` と `verify` を公開して uzushio が import する。** 不採用。
  ADR-0008 D4（公開 API は語彙のみ）を崩し、Go の版に uzushio を縛る。CLI の JSON は言語も版も跨げる。
- **uzushio が独自のマニフェストを Task の隣に置く（doctor.json）。** 不採用。同じ Task が 2 つの
  定義を持つ。
- **0002 を編集して D2 を書き換える。** 不採用。accepted な記録の決定は書き換えず、supersede する
  （ADR-0001 の運用、DocDag の `status_drift` が検査）。
- **0002 を supersede せず、0009 を「追加」として depends-on だけで足す。** 不採用。0002 D2 は
  「コマンドは 2 つ」と言い切っており、それが変わったのだから 0002 は現行の決定ではない。
- **`verify` の結果を `<task>/runs/` にトレースとして書く。** 不採用。run はハーネスを読んで候補を
  作り選ぶ一連の記録であり、単発の検証は run ではない。トレーススキーマに種別を足すより、呼び手が
  自分の記録に保存する方が境界が保たれる。

## 影響とトレードオフ

- 得るもの：uzushio の `task doctor` が CMoA と同じ verifier で測る。Task の定義が 1 ファイルに保たれる。
- 失うもの：0002 の「コマンドは 2 つ」という言い切りの単純さ。`verify` は `select` と同じ経路を通るが、
  終了コードの意味が `select` と違う（D2 に理由を書いた）。
- リスク：`band` を予約語にしたまま実装しない期間が長引くと、語が形骸化する。Plecto 系の性能ゲートを
  Task にするときに実装するか、supersede して語を消す。

## 関連ADR

- 0002（supersede 元）、0004（候補は unified diff。reference と mutant も同じ形）、0005（verifier の
  経路）、0007（トレースの境界）、0008（verifier は読み取り専用、公開 API は語彙のみ）
