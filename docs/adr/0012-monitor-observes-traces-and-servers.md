---
title: "観測面：CMoA Monitor はトレースとサーバーの状態を読み、round を頼むときは serve の一クライアントとしてだけ頼む"
status: accepted
date: 2026-09-06
depends-on: [0007, 0011]
---

# 0012: 観測面：CMoA Monitor はトレースとサーバーの状態を読み、round を頼むときは serve の一クライアントとしてだけ頼む

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

### 所有者の判断（2026-09-06、4 ラウンド）

- データの届け方は **SvelteKit 単体（adapter-node）**。サーバールートが run ディレクトリと `/slots` を読み、
  Server-Sent Events で押し出す。Go 側は変えない。
- 最初の判断は**観測のみ**（round を起動せず、`cmoa serve` に POST もしない）。同日、画面から CMoA と
  対話したいという理由でこれを改め、**チャット欄（COMM パネル）を置く**。送信経路は**モニターが `cmoa serve`
  に中継する**（ブラウザから直接 POST すると CORS に掛かり、Go 側の変更が要る）。審判が決められなかった
  ときは**エラーとして見せ、会話には入れない**（人が候補を選ぶ案も、自動で送り直す案も不採用）。
  フリート帯のレーダー演出は要らない。
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

### D2. 読むのが本分。round を頼むのは `cmoa serve` の一クライアントとしてだけ

モニターが自分で読む入力は二つだけである。

| 入力 | 何を読むか | 根拠 |
| --- | --- | --- |
| run ディレクトリ | `run.json`、`candidates/*.json`、`verify/*/result.json`、`judge/*.json`、`judge.json`、`select.json`、serve の request ディレクトリの `conversation.json` | 0007 の契約（`docs/trace-schema.md`）。write-once なので `select.json` の書かれた run はキャッシュしてよい |
| 各サーバー | 提案者と審判の `/slots`（無ければ `/metrics`）、`serve` の `GET /v1/models` | llama-server が公開する読み取り専用の状態。到達性と slot の `is_processing` / `n_decoded` / prefill 進捗 |

モニターは round を自分で回さない（`propose` / `select` を子プロセスにしない）し、ディレクトリに書かない。
理由は 0007 と同じで、**トレースを書くのは round を回した者だけ**である。観測者が起動者を兼ねると、
モニターが落ちた round と `serve` が落とした round が同じ形で残り、上位層が「誰がこの run を作ったか」を
run.json から読めなくなる。

画面のチャット欄（COMM）から送った会話は、モニターの `POST /api/chat` が **`cmoa serve` の
`/v1/chat/completions` にそのまま中継する**。モニターは serve にとって他のクライアントと区別の付かない
一クライアントで、round を回すのも、request ディレクトリと run を書くのも serve である（0011 D2）。
モニターは応答の `cmoa` 拡張（run id、選択の種別と理由、審判のコール数とスワップ一致）を会話の脇に出し、
その run をピン留めして round の内側を同じ画面に見せる。会話の履歴は転送する message 配列そのもので、
モニターは system プロンプトも候補も足さない。

審判が決められなかった応答（HTTP 502 `no_candidate`、504 `judge_timeout`）はエラーとして赤で見せ、
**会話には入れない**。人が 3 候補から選んで続ける案は、審判の代わりを人が務めることになり、
0011 D4 の「決められなければ `NoCandidate`」を画面の側で無かったことにする。自動で送り直す案は 0011 の
「再質問しない」に反する。同じ質問を人が送り直すのは、別の run として記録される普通の要求である。
ヘルス確認は `GET /v1/models` に限る。

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
- **モニターが `propose` / `select` を子プロセスで回す。** 不採用。モニターが round の起動者になり、
  トレースの書き手が二つになる（D2）。コーディング面の round は verifier がリポジトリとコンテナを要るので、
  HTTP のクライアントに渡すものでもない（0011）。
- **Go の `serve` に CORS ヘッダを足してブラウザから直接 POST する。** 不採用。Go 側の変更が要り、
  loopback 限定の serve に origin の概念を持ち込む。中継なら Go は無変更で済む。
- **`no_candidate` のとき人が候補を選んで会話を続ける。** 不採用（所有者）。D2 の理由。
- **`no_candidate` のとき自動でもう 1 回送る。** 不採用。0011 の「再質問しない」。
- **観測のみに留める。** 当初の判断だったが同日に改めた。画面から CMoA と対話できないモニターは、
  round を見るために別の端末で `curl` を打つことになり、見る場所と頼む場所が分かれる。
- **モニター専用の設定ファイル。** 不採用。提案者と `base_url` の二重管理になる。
- **ページ分割（/、/runs、/runs/[id]）。** 不採用。1 画面に全部が見えることが端末版の価値だった。
- **roadmap の Step 8 にする。** 不採用（所有者）。roadmap の step は「上の層が検証できる順」で並ぶ
  （`docs/roadmap.md`）。モニターには検証器が無く、順序に入らない。番号なしの Tooling として記す。

## 影響とトレードオフ

- 得るもの：round の内側が、開発機の端末以外からも見える。swap 一致と `NoCandidate` の理由が、待っている
  間に見える。較正の verdict と有効期限が同じ画面にある。会話を送った画面で、その会話の round が見える。
- 中継の代償：モニターは `serve` への書き込み経路を一つ持つ。それは他のどのクライアントも持つ経路で、
  モニターにしか出来ないことは増えていない。`serve` が落ちていれば送信は無効になり、モニターは
  それを `SERVE OFFLINE` として見せる。
- 失うもの：リポジトリに Node のツールチェーンと 2 つ目の CI ワークフローが入る。`monitor/` の依存は
  SvelteKit と検査・テストの道具に限り、UI ライブラリや CSS フレームワークは入れない。
- リスク：`/slots` を 0.5 秒ごとに叩く負荷は無視できるが、`/metrics` だけのサーバーでは tok/s が累積値の
  差分になり、他のクライアントの生成が混じる。到達性と busy は正しく、tok/s は目安として表示する。

## 関連ADR

- 0007（トレース——読む契約）、0011（チャット面と `serve`——観測する対象）、0009 D2 / 0011 D6（Go の
  依存は増えない——別プロセスにした理由）
