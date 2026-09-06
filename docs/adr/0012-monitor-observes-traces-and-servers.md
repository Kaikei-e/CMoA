---
title: "観測面：CMoA Monitor はトレースとサーバーの状態を読むだけで、何も起動しない"
status: accepted
date: 2026-09-06
depends-on: [0007, 0011]
---

# 0012: 観測面：CMoA Monitor はトレースとサーバーの状態を読むだけで、何も起動しない

## ステータス

Accepted

採択日: 2026-09-06

## 日付

2026-09-06

## コンテキスト

Step 7 までで CMoA は二つの顔を持った。コーディング面は `propose` → verifier → `select`、チャット面は
`propose` → 盲検 pairwise 審判 → `select`（0011）。どちらも 1 round を 1 ディレクトリの write-once な
JSON として残す（0007）。round は提案者 3 体の同時生成と、審判 6 コールまたは verifier の逐次実行から
成り、実機で数十秒から数分かかる。その間に何が起きているか——どの提案者が prefill 中か、審判のどの
ペアが両順序で一致したか、どこで時間を使っているか——は、トレースのファイルが**現れる順**と、各
llama-server の `/slots` にしか無い。

これまでは端末用のシェルスクリプトが 0.5 秒ごとにその両方を読み、1 画面に描いていた。それは開発機の
外に持ち出せる形ではなかった（設定と run の場所を直に知っている）。一方で「round を見る」という要求
自体は残る。uzushio が長い suite を回すとき、`serve` に外部クライアントがぶら下がるとき、較正の verdict
が binding のまま日を跨ぐとき——人が見るのは round の内側である。

### 所有者の判断（2026-09-06、3 ラウンド）

- データの届け方は **SvelteKit 単体（adapter-node）**。サーバールートが run ディレクトリと `/slots` を読み、
  Server-Sent Events で押し出す。Go 側は変えない。
- **観測のみ**。round を起動せず、`cmoa serve` に POST もしない。
- 表示は端末版の 1 画面に加えて、フリートの常時ヘルス、run 履歴と切替、候補・審判コール・verify 出力の
  中身、較正ステータス。
- 設定は **`cmoa.json` と監視ルートの環境変数**。モニターは公開コードなので、開発機のパスを知らない。
- 見た目は艦載の戦闘指揮所（CIC）の計器盤：等幅、固定グリッド、リン緑・黄・白、英語のみ、
  演出は遷移と走査線まで、音なし。1 画面 + ドロワー。
- pnpm、Svelte 5、TypeScript、手書き CSS、vitest と Playwright、CI。
- roadmap の表には足さず「Tooling」として記す。この ADR を書く。端末版は残す。

## 決定

### D1. 観測面は独立したプロセスで、CMoA の 6 コマンドを増やさない

0011 D2 は「コマンドは 6 つ、`serve` だけ常駐する」と決めた。モニターはその外に置く：`monitor/` の
SvelteKit アプリケーションで、Go モジュールは変わらず、`cmoa` バイナリはモニターの存在を知らない。
Go に `monitor` サブコマンドを足して静的資産を埋め込む案は、Go のリポジトリに Node のビルドを持ち込み、
「Go 1.27 と標準ライブラリだけ」（0009 D2、0011 D6）の境界を破る。別プロセスなら境界は残る。

### D2. 読むだけ。書かない、起動しない、POST しない

モニターの入力は二つだけである。

| 入力 | 何を読むか | 根拠 |
| --- | --- | --- |
| run ディレクトリ | `run.json`、`candidates/*.json`、`verify/*/result.json`、`judge/*.json`、`judge.json`、`select.json`、serve の request ディレクトリの `conversation.json` | 0007 の契約（`docs/trace-schema.md`）。write-once なので `select.json` の書かれた run はキャッシュしてよい |
| 各サーバー | 提案者と審判の `/slots`（無ければ `/metrics`）、`serve` の `GET /v1/models` | llama-server が公開する読み取り専用の状態。到達性と slot の `is_processing` / `n_decoded` / prefill 進捗 |

モニターは round を起動しない（`propose` / `select` を子プロセスにしない）、`serve` に補完要求を送らない、
ディレクトリに書かない。理由は 0007 と同じで、**トレースを書くのは round を回した者だけ**である。
観測者が起動者を兼ねると、モニターが落ちた round と `serve` が落とした round が同じ形で残り、上位層が
「誰がこの run を作ったか」を run.json から読めなくなる。ヘルス確認は `GET /v1/models` に限り、
`POST /v1/chat/completions` は決して送らない——観測が選択を増やしてはならない。

### D3. 設定は `cmoa.json` と監視ルートで、モニター専用の設定ファイルは作らない

`CMOA_CONFIG` で `cmoa` と同じ `cmoa.json` を読む（提案者の id と `base_url`、審判、`serve.runs_dir`、
`harness.vault`）。監視するディレクトリは `CMOA_MONITOR_ROOTS` に `:` 区切りで与える。各要素は run
ディレクトリ、task ディレクトリ（`runs/` を持つ）、serve のルート（`<request>/runs/<run>/`）のどれかで、
種別は中身から判定する。省略時は `serve.runs_dir`。較正文書の場所は `CMOA_MONITOR_CALIBRATIONS`、省略時は
vault の `spec/calibrations`。

二重管理を避けるためである。提案者の一覧と `base_url` が二か所にあると、フリートを組み替えたとき
モニターだけが古い server を見続ける。監視ルートが `cmoa.json` に無いのは、それが round の設定ではなく
観測者の関心だからで、環境変数は起動スクリプト 1 行で済む。

### D4. 派生は純関数で、端末版と同じ状態語を使う

ファイル群と `/slots` のサンプルから画面状態を導く関数は副作用を持たず、fixture で検査される。状態語は
端末版と同じ：レーンは idle / unreachable / prefill / generating / done / failed、審判のペアは
`judge.json` が書かれる前でも `judge/<pair>-<ab|ba>.json` から暫定判定（`~gemma`、`~draw (tie)`、
`~draw (disagree)`）を出し、確定後は `judge.json` の verdict に置き換える。色は 4 種だけ
（ok / run / bad / dim）。

暫定判定を出す理由は 0011 D4 の「両順序が一致したときだけ勝ち」を、人が待っている**その瞬間に**見せる
ためである。ペア 0 の ab が終わって ba を待つ 15 秒間、画面が「judging」しか言わないなら、swap 一致率
という中心の量が観測できない。

### D5. 表示は 1 画面、公開コードは開発機を知らない

1 画面の HUD（フリート帯 / 提案レーン / 審判グリッド / 選択 / タイムライン / 較正）と、run 履歴と中身を
出す 2 つのドロワー。URL は 1 つで `?run=<id>` がピン留め、無指定は最新の run を追う。
`monitor/` の中（README、コメント、テストの fixture）に開発機のパス、機体、GPU、`local/` 配下への言及を
置かない。fixture の候補本文と審判の reason は合成テキストで、モデルの出力を含めない。

## 根拠（調査結果・出典）

- 端末版の実装（gitignore 配下）が 2026-09-04〜06 に実際に使われた画面構成：提案レーン、pair × ab/ba
  グリッド、select、フェーズ timeline。モニターはその集合を減らさない。
- 0007（トレースは write-once の JSON、`run-id` の辞書順が時刻順）——最新 run の追従と不変 run のキャッシュ
  はこの二つから直接導ける。
- 0011 D4（両順序一致、Condorcet、`NoCandidate` のサブ理由）——暫定判定と outcome 表示の語彙。
- llama.cpp server の `/slots` と `/metrics`：読み取り専用で、`is_processing`、`n_prompt_tokens_processed`、
  `next_token.n_decoded` を返す。`/slots` を切ったサーバーには `/metrics` の累積値から run 開始時の基準差分
  を取る。
- SvelteKit の `+server.ts` は `ReadableStream` を返せるので、SSE に追加の依存は要らない。

## 検討した代替案

- **Go に `cmoa monitor` を足し、Svelte のビルドを `go:embed` する。** 不採用。単一バイナリになる利点は
  あるが、Go リポジトリの CI に Node が入り、0011 D6 の「依存は増えない」を破る。
- **静的ビルド + Go のサイドカー。** 不採用。プロセスが 2 つになり、サイドカーが結局 Go 側に `monitor`
  相当を足すことになる。
- **モニターから round を起動する／`serve` にプロンプトを送る。** 不採用（所有者）。D2 の理由。
- **モニター専用の設定ファイル。** 不採用。提案者と `base_url` の二重管理になる。
- **ページ分割（/、/runs、/runs/[id]）。** 不採用。1 画面に全部が見えることが端末版の価値だった。
- **roadmap の Step 8 にする。** 不採用（所有者）。roadmap の step は「上の層が検証できる順」で並ぶ
  （`docs/roadmap.md`）。モニターには検証器が無く、順序に入らない。番号なしの Tooling として記す。

## 影響とトレードオフ

- 得るもの：round の内側が、開発機の端末以外からも見える。swap 一致と `NoCandidate` の理由が、待っている
  間に見える。較正の verdict と有効期限が同じ画面にある。
- 失うもの：リポジトリに Node のツールチェーンと 2 つ目の CI ワークフローが入る。`monitor/` の依存は
  SvelteKit と検査・テストの道具に限り、UI ライブラリや CSS フレームワークは入れない。
- リスク：`/slots` を 0.5 秒ごとに叩く負荷は無視できるが、`/metrics` だけのサーバーでは tok/s が累積値の
  差分になり、他のクライアントの生成が混じる。到達性と busy は正しく、tok/s は目安として表示する。

## 関連ADR

- 0007（トレース——読む契約）、0011（チャット面と `serve`——観測する対象）、0009 D2 / 0011 D6（Go の
  依存は増えない——別プロセスにした理由）
