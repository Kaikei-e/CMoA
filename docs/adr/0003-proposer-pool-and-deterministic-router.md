---
title: "提案者プールは OpenAI 互換 HTTP のローカル異種 3 体とし、順序と文脈を設定が決める"
status: accepted
date: 2026-09-04
depends-on: [0002]
---

# 0003: 提案者プールは OpenAI 互換 HTTP のローカル異種 3 体とし、順序と文脈を設定が決める

## ステータス

Accepted

採択日: 2026-09-04

## 日付

2026-09-04

## コンテキスト

### ルータが決めること

CMoA の第 1 の責務は決定論的ルータと提案者プールである。決めるのは 3 つ：**誰に訊くか**、**どの順で
訊くか**、**何を見せるか**。3 つとも LLM に委ねない。委ねた場合の成績は測られている
（2605.24048 §5.2：LLM 審判に提案者を採点させる方式は全データセットで低成績、しかも強い審判が弱い審判に
劣る）。

### 異種性の価値と、その測り方

異種提案者の価値は「同じ間違いをしないこと」にある。ただしその測り方は訂正が要る。
2606.27288 は、**平均ペアワイズ誤答相関 ρ では全滅率 β を識別できない**（同一の周辺分布・同一の
ペアワイズ相関で全滅率が異なる誤差則を構成できる）と明言している。初期の設計メモが置いていた
「提案者ペアの誤答相関が最小になる組合せでプールを選ぶ」という規則は、引用元の論文自身が否定した
指標に立っている。

同じ論文は CMoA のコーディング面に直接効く上界も与える：出力が必ずいずれかのメンバの答えである方針
（＝選択型集約）では **合格率 ≤ 1 − β** が厳密に成り立つ。提案者を増やしても β が下がらなければ
合格率は伸びない。

これは測定の話であり、測定は uzushio の責務である。CMoA が負うのは「β を数えられる形で run を残す」
ことだけで、それは ADR-0006 D2（全候補を検証し切る）と ADR-0007（トレース）に落ちる。

### ローカルに閉じる理由

手元の 1 台（統合 GPU を持つワークステーション）で夜間に試行回数を稼ぎ、
失敗マイニングと編集提案をローカル 9B〜20B 級で回す。恩恵を受けるのはフロンティア側である
（2605.30621：恩恵は非単調で中位モデルが最大）。クラウド課金なしで回数を稼ぐことがこの 1 台の役割で、
そこにクラウド API を混ぜると、この構成の存在理由が消える。

### 満たすべき要件

- R1 どの提案者がどの順で呼ばれるかは、設定ファイルだけを読めば分かる
- R2 バックエンドのプロトコルは 1 種類に固定する（クライアントの分岐を増やさない）
- R3 設定の誤りは run が始まる前に落ち、既定値への黙った転落が起きない
- R4 サンプリングは提案者ごとに変えられる（モデル提供者の推奨値が食い違うため）
- R5 提案者数 n から Byzantine 許容数 f を計算し、トレースに残す
- R6 プールの異種性は「ラボ・学習系統・アーキテクチャ・トークナイザ」の複数軸で分離する
- R7 推論フリートの構成（compose、モデルの置き場、起動フラグ）は CMoA のリポジトリに入れない
- R8 秘密（API キー）を設定ファイルに書かない

## 決定

### D1. バックエンドは OpenAI 互換 `POST /v1/chat/completions` のみ

`internal/llm` は 1 エンドポイントしか知らない。

- `stream: false`。非ストリームなら最終レスポンスに llama-server の `timings` が必ず入り、
  トレースが確実に取れる。
- `n` は送らない。**1 提案者 1 サンプル**（D4）。複数サンプルが要るならクライアント側で
  複数リクエストを投げる（llama.cpp のスロットモデルで `n>1` が並列に扱われるかは README から
  断定できない — **未検証**）。
- `response_format` / `grammar` は使わない。diff はプレーンテキストで受け、`internal/patch` が
  切り出す（ADR-0004 D2）。unified diff を JSON 文字列に押し込むと `\n` エスケープが大量に発生し、
  小型モデルの失敗率が上がる。
- 失敗は型で分ける：`*llm.HTTPError{Status, Body}`（非 2xx）、`*llm.DecodeError{Body, Err}`
  （2xx だが chat completion ではない）、`context.DeadlineExceeded`（タイムアウト）。
  これが ADR-0004 D4 の候補ステータスにそのまま対応する。
- 応答の `content` から `<think>…</think>` を除去する（`llm.StripReasoning`）。
  除去前は `RawContent` として残し、`candidates/<id>.raw.txt` に書く。

**不採用：** Anthropic 互換 `/v1/messages`（llama-server も llama-swap も提供するが、
OpenAI 互換で足りる）、Ollama ネイティブ `/api/chat`（`message.thinking` など独自形状が増える）、
クラウド API（2026-09-04 に採択：提案者バックエンドは OpenAI 互換 HTTP のみ、ローカル完結）。

llama-server / llama-swap / Lemonade / Ollama の `/v1` はすべて OpenAI 互換を話すので、
CMoA から見たフリートの入れ替えはこの 1 本で足りる。

### D2. 設定は JSON（`cmoa.json`）。未知キーはエラー

探索順は `--config` → `$CMOA_CONFIG` → `<task>/cmoa.json` → `./cmoa.json`。見つからなければエラー。

```json
{
  "version": 1,
  "proposers": [
    {"id": "granite", "base_url": "http://127.0.0.1:8081/v1", "model": "granite-4.2-8b",
     "temperature": 0.2, "max_tokens": 4096, "timeout_seconds": 300,
     "extra_body": {"chat_template_kwargs": {"enable_thinking": false}}},
    {"id": "qwen",     "base_url": "http://127.0.0.1:8082/v1", "model": "qwen3.5-9b"},
    {"id": "ministral","base_url": "http://127.0.0.1:8083/v1", "model": "ministral-3-8b",
     "temperature": 0.1}
  ],
  "harness": {"vault": "/path/to/uzushio", "docdag": "docdag"},
  "verify": {"max_parallel": 1, "timeout_seconds": 600},
  "selection": {"rule": "first"}
}
```

- `encoding/json` で読み、`DisallowUnknownFields`。**キーの綴り間違いが既定値への転落にならない**（R3）。
- 検証は読み込み時に全項目。`id` は `^[a-z0-9][a-z0-9-]{0,31}$` で一意（run ディレクトリのファイル名に
  なる）。`base_url` は http/https。`temperature` は [0, 2]。既定値は
  `temperature 0.2` / `max_tokens 4096` / `timeout_seconds 300` / `max_parallel 1` /
  `verify.timeout_seconds 600` / `docdag "docdag"` / `rule "first"`。
- 誤りは `*config.ValidationError{Path, Msg}` で、JSON パス（`proposers[1].base_url`）を持つ。
- YAML は手書きしない。ADR-0002 D3 の依存ゼロ方針では YAML パーサが標準に無く、
  自作するのは設定 1 ファイルのために重すぎる。
- 既定値を埋めた**実効値**を `run.json` の `config` に写す（ADR-0007）。

### D3. 提案者順は設定の配列順。全提案者を並行に呼ぶ

`propose` は提案者ごとに goroutine を立て、各自の `timeout_seconds` で `llm.ChatCompletion` を呼ぶ。
`select` は `cfg.Proposers` の順に候補を集め、その順が `select.json` の `order` になり、規則 `first`
の「最初」の意味になる（ADR-0006 D1）。

順序をモデルの強さで並べ替えることはしない。2605.24048 §5.3 は提示順が系統的なバイアスを持つ
（強い提案者を後ろに置くほうが良い、long-context の recency bias と整合）と実測しているが、それは
**審判に候補を見せる**設定の話である。コーディング面の選択器は verifier であり、候補を読まない。
順序が効くのはチャット面（v1）だけで、そこでは無作為化と位置スワップを入れる。

### D4. サンプリングは提案者ごと。既定温度 0.2

温度は提案者ごとに設定でき、省略時 0.2。`seed` は省略時にリクエストへ載せない。
`extra_body` は任意の JSON オブジェクトで、リクエスト本文にマージする（llama-server の
`chat_template_kwargs` / `reasoning_format` などサーバー固有キーのため）。
ただし CMoA が設定するキー（`model` / `messages` / `temperature` / `max_tokens` / `seed` /
`stream` / `n`）を `extra_body` に置くのは設定エラーにする。同じ値が 2 箇所から来るとトレースの
`request_sha256` が説明できなくなる。

提案者ごとに変える必要があるのは、公式推奨が食い違うためである：Mistral は Ministral 3 のカードで
「本番では temperature を 0.1 未満に」と明記し、IBM は Granite 4.2 に `temperature=1.0, top_p=0.95` を、
Qwen は Qwen3.5 非思考に `temp=0.7, top_p=0.8, top_k=20` を推奨する。diff 生成では総じて下げる。

API キーは `api_key_env`（環境変数名）で指定し、値は設定にもトレースにも書かない（R8）。
ローカル完結の構成では通常空である。

### D5. Byzantine 許容数 f をトレースに書く

n 体の提案者は f = ⌊(n−1)/3⌋ 体の欺瞞的提案者に耐える（Lamport–Shostak–Pease の n ≥ 3f+1）。
`config.ByzantineTolerance()` が (n, f) を返し、`run.json` の `byzantine` に書く。

**3 体構成は f = 0 である。1 体も許容しない。** これを設定の帰結として毎 run 記録し、判定はしない。
4 体以上にするかは、uzushio が β と提案者ごとの通過率を見て決める。

### D6. 提案者 3 体は Granite 4.2 8B / Qwen3.5-9B / Ministral 3 8B とする

モデル選定は公開情報の調査で決め、ダウンロード前に候補を提示する、という 2026-09-04 の決定に対する
回答である。

| 役割 | モデル | GGUF | Q4_K_M | 推定 VRAM（16k, KV=q8_0） | LiveCodeBench v6 |
| --- | --- | --- | --- | --- | --- |
| A | IBM Granite 4.2 8B | `ibm-granite/granite-4.2-8b-GGUF` | 5.35 GB | ~6.7 GiB | 73.24 |
| B | Qwen3.5-9B | `unsloth/Qwen3.5-9B-GGUF` | 5.68 GB | ~6.0 GiB | 65.6 |
| C | Ministral 3 8B Instruct 2512 | `mistralai/Ministral-3-8B-Instruct-2512-GGUF` | 5.20 GB | ~6.4 GiB | 61.6 |

3 体を同時常駐させる前提で KV キャッシュは `q8_0` にする（f16 のままでは KV が約 2 倍になり、同時常駐の枠を圧迫する）。3 体とも Apache-2.0。

異種性は 4 軸すべてで分離している（R6）。

| 軸 | Granite 4.2 8B | Qwen3.5-9B | Ministral 3 8B |
| --- | --- | --- | --- |
| ラボ / 国 | IBM（米） | Alibaba Qwen（中） | Mistral AI（仏） |
| 学習系統 | Granite 4 系（独自 15T token、独自スケーリング） | Qwen3.5 系（ネイティブマルチモーダル、MTP） | Mistral 3 系（Tekken tokenizer） |
| アーキテクチャ | 純 dense ＋ GQA、40 層 | ハイブリッド（Gated DeltaNet 24 層 ＋ full attention 8 層） | 純 dense ＋ GQA、34 層、SWA なし |
| 語彙 | 100,352（GPT-2 系 BPE） | 248,320 | 131,072（Tekken） |
| チャット書式 | ChatML 系 ＋ `<think>` | ChatML 系 ＋ `<think>` | `[SYSTEM_PROMPT]` / `[INST]` |
| KV プロファイル | 重い（160 KiB/tok） | 非常に軽い（32 KiB/tok） | 中（136 KiB/tok） |

失敗モードも逆を向く：Granite は「reasoning 過多で diff が長い」、Ministral は「reasoning なしで即答
するが浅い」。選択型集約が欲しいのはこの種の分散である。

**この 3 体はまだ実測していない — 未検証。** どのベンチマークも「unified diff だけを出力する能力」を
直接は測っていない。導入前に小さな Go タスク集（10〜20 件）で diff 形式順守率と `git apply` 成功率を
測り、それを最終的な選定基準にする。

### D7. Gemma 4 12B は予備に降格する

Gemma 4 12B-it は 4 つ目の完全に独立した系統（Google DeepMind）で、コード性能は Granite に匹敵する
（LiveCodeBench v6 72.0、Apache-2.0）。それでも本命トリオに入れない理由は 1 つ：

**参照機と同じ世代の統合 GPU で Vulkan フルオフロード時に出力が決定的に壊れる未修正
Issue #27007 がある。** 原因は fused `mul_mat_vec_q`（MMVQ）カーネル経路と特定されており、
回避策は `GGML_VK_DISABLE_MMVQ=1` または `--n-cpu-moe 99`。関連 Issue #24311 では Gemma 4 12B QAT でも
同じ問題が報告されている。

予備の序列は次のとおり。

1. `gemma-4-12B-it`（投入前に temperature=0 で決定的出力の健全性チェックが必須）
2. `Qwen3.5-4B`（Q6_K で 3.5 GB。系統が B と同じなので共倒れの観点では劣る。タイブレーカ用）
3. `granite-4.1-8b`（Granite 4.2 の Jinja テンプレートや `<think>` パースで問題が出た場合の退避先）

同じ理由で **Gemma 4 E2B/E4B は提案者に使わない**：中核である Per-Layer Embeddings が llama.cpp の
forward graph に未実装で（Issue #22243, OPEN）、クラッシュせずに品質だけが静かに劣化する。
ローカルの Ollama にある `gemma4-e4b-q4km` も同じ理由で不可。`Ternary-Bonsai-8B-Q2_0` は
2bit 相当で diff の桁レベルの正確さに耐えない。

### D8. フリート構成は CMoA の責務外とし、`local/` に置く

llama-server の起動フラグ、モデルの置き場、compose、ポート割当は、この端末固有の事実で
あって CMoA の決定ではない。`local/` を gitignore し、CMoA は `cmoa.json` の `base_url` でのみ
フリートに接する。推奨フラグ（`-c 16384 -np 1 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 --jinja`、
モデルごとの reasoning 設定）は `local/` 側に置き、公開リポジトリには持ち込まない。

llama.cpp の Docker タグは可動タグ（`:server-vulkan`）ではなくビルド番号でピン留めする
（現在 `server-vulkan-b9859`）。Gated DeltaNet 関連の修正は継続的に入っているので、タグを上げたら
Qwen3.5 の速度を再計測する。

## 根拠（調査結果・出典）

### A. 提案者の選び方に関する文献

- Complementary-MoA（arXiv 2605.24048）：LLM 審判に提案者を採点させる方式は全データセットで低成績。
  「モデル間の多様性は、同一モデル内のプロンプト差による多様性より重要」（model-first greedy）。
  提示順の系統的バイアス（§5.3）。 https://arxiv.org/abs/2605.24048
- Co-Failure Ceiling（arXiv 2606.27288）：「出力がいずれかのメンバの答えであるどの方策も、精度は
  1 − β を超えない」「通常の診断指標である平均ペアワイズ誤答相関 ρ は β を識別できない」
  「利得は、モデルが**異なる問題で**失敗することから来るのであって、モデルを増やすことからではない」。
  https://arxiv.org/abs/2606.27288
- Selection Bottleneck（arXiv 2603.20324）：異種チーム＋選択の勝率 0.810 対 同種 0.512。
  https://arxiv.org/abs/2603.20324
- Lamport, Shostak, Pease「The Byzantine Generals Problem」(1982)：n ≥ 3f + 1。
- Condorcet の陪審定理は投票者の独立性を前提とし、独立性が崩れると成り立たない。上の β の議論は
  その前提破れの実測である。

「誤答相関 ρ の最小化でプールを選ぶ」という初期の規則は、2606.27288 により**目的関数として不適切**
である。置き換えは「β（全提案者が同時に失敗する率）の直接推定」で、CMoA のコーディング面では
verifier が二値を返すので run 履歴から数えられる。ρ は補助指標に降格する。Markowitz の共分散類推は
「ペアワイズ二次形式で尾部リスクが記述できる」という前提に立っており、その前提はここで崩れている。

### B. モデルとランタイムの一次情報

- IBM Granite 4.2 8B — https://huggingface.co/ibm-granite/granite-4.2-8b 、
  公式 GGUF https://huggingface.co/ibm-granite/granite-4.2-8b-GGUF
- Qwen3.5-9B — https://huggingface.co/Qwen/Qwen3.5-9B 、
  GGUF https://huggingface.co/unsloth/Qwen3.5-9B-GGUF
  （カード：`/think` `/nothink` のソフトスイッチは 3.5 で非対応。無効化は
  `chat_template_kwargs: {"enable_thinking": false}`）
- Ministral-3-8B-Instruct-2512 — https://huggingface.co/mistralai/Ministral-3-8B-Instruct-2512
  （カード：本番運用では temperature を 0.1 未満に）
- Gemma 4 12B-it — https://huggingface.co/google/gemma-4-12B-it
- llama.cpp Issue #27007（特定の iGPU 世代で Gemma 4 の出力破損、OPEN）—
  https://github.com/ggml-org/llama.cpp/issues/27007
- llama.cpp Issue #22243（Gemma 4 E2B/E4B の PLE 未実装、OPEN）—
  https://github.com/ggml-org/llama.cpp/issues/22243
- llama.cpp PR #20334（Vulkan の GATED_DELTA_NET 追加。iGPU で実測ベンチ済み）—
  https://github.com/ggml-org/llama.cpp/pull/20334
- llama.cpp Issue #24861 / #25080（iGPU でのアテンショングラフのハング、コマンド投入時のメモリ不足）—
  https://github.com/ggml-org/llama.cpp/issues/24861 、
  https://github.com/ggml-org/llama.cpp/issues/25080
- llama-server README（エンドポイント、`chat_template_kwargs`、`timings`、フラグ）—
  https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
- llama.cpp Issue #20196（`--reasoning-budget 0` と `chat_template_kwargs` が黙って効かない、
  wontfix でクローズ）— https://github.com/ggml-org/llama.cpp/issues/20196 。
  D1 の「`<think>` を必ず除去する」はこれが根拠である。思考フラグを信用しない。
- llama.cpp docs/docker.md（タグ体系。`:server-vulkan` は可動タグ）—
  https://github.com/ggml-org/llama.cpp/blob/master/docs/docker.md

### C. 2026-09-04 に採択された決定

| 決定 | 理由 |
| --- | --- |
| 提案者バックエンドは OpenAI 互換 HTTP のみ。ローカル完結、クラウド API なし | 夜間に課金なしで試行回数を稼ぐという構成の目的。プロトコルが 1 本ならクライアントに分岐が生えない |
| 設定は JSON（`cmoa.json`）。標準ライブラリで読む | 依存ゼロ（ADR-0002 R2）を崩さない |
| モデル選定は公開情報の調査で決め、ダウンロード前に候補を提示する | 数十 GB のダウンロードは後戻りが高い。選定理由が文書に残る |
| 1 提案者 1 サンプル。温度は提案者ごと、既定 0.2 | 異種性はモデル軸で取る。同一モデルの複数サンプルは多様性が出にくい（2605.24048） |
| 推論フリートは `local/` に非公開で置く（gitignore） | 端末固有の事実は CMoA の決定ではない（D8） |

## 検討した代替案

- **A. クラウド API（Anthropic / OpenAI）を提案者に混ぜる。** 不採用。ローカル推論機の役割は
  課金なしで試行回数を稼ぐことで、そこにクラウドを混ぜると構成の存在理由が消える。フロンティア側は
  「恩恵を受ける側」であって提案者ではない。
- **B. Ollama ネイティブ API（`/api/chat`）を話す。** 不採用。`message.thinking` など独自形状が増え、
  クライアントに分岐が生える。Ollama も `/v1` を提供する。
- **C. Anthropic 互換 `/v1/messages` を話す。** 不採用。llama-swap が入口として提供するが、
  OpenAI 互換で足りるところにプロトコルを 2 本持つ理由がない。
- **D. 設定を YAML にする。** 不採用。標準ライブラリに YAML が無く、依存ゼロ（ADR-0002 R2）を崩す。
  Rust 側の YAML エコシステムの現状（serde_yaml 開発終了、serde_yml の RUSTSEC-2025-0068）も、
  YAML を手書きの設定形式として選ぶ動機を弱めた。
- **E. 未知キーを無視して読み飛ばす。** 不採用。`temperture` と書いた設定が 0.2 で走り、しかも
  トレースには実効値 0.2 が正しく記録される。原因の追えない実験になる。
- **F. 1 提案者に N サンプルを引かせる（Self-MoA 型）。** 不採用（D4）。Self-MoA の優位は
  合成型集約について示されたもので、選択型・異種構成の設定には転移していない。ただし将来
  `n` を設定に足す余地は残す（`config.Proposer` にフィールドを 1 つ増やすだけで済む）。
- **G. ルータに LLM を置き、タスクに応じて提案者を選ばせる。** 不採用（2605.24048）。
- **H. 提案者順をモデルの強さで並べる。** 不採用（D3）。コーディング面では選択器が候補を読まない。
- **I. 1 プロセスに 3 モデルを載せる（llama.cpp のルータモード）か、3 コンテナに分けるか。**
  CMoA の決定ではない（D8）。調査の推奨は組み込みルータモードで、`--models-max` の LRU により
  iGPU メモリが自動管理される。

## 影響とトレードオフ

**得るもの**

- クライアントが 1 エンドポイントしか知らないので、`internal/llm` は小さく、失敗の分類が一意になる。
- 設定 1 ファイルを読めば「誰に、どの順で、どんなサンプリングで訊いたか」が分かる。同じ内容が
  実効値として `run.json` に写るので、後から設定ファイルが変わっても run の記述は壊れない。
- 異種性を 4 軸で分離したプールは、β を下げる方向の設計としては現状もっとも根拠がある。

**失うもの・リスク**

- **f = 0 である。** 3 体構成は欺瞞的提案者を 1 体も許容しない。ローカルの GGUF を自分で起動している
  以上、実務上の脅威は低いが、モデルの供給元が信頼できない場合はこの構成では守れない。
- **モデル選定はベンチマークの自己申告に依存している。** LiveCodeBench v6 の値は各社のモデルカード
  由来で、thinking の有無や評価プロンプトが揃っていない可能性がある。しかも「unified diff だけを
  出力する能力」を直接測ったベンチは無い（**未検証**、D6 の末尾）。
- **llama.cpp 側の不安定さを CMoA は吸収しない。** 特定 GPU 世代固有の Vulkan バグ（#27007 / #24861 /
  #25080）はフリート側の問題として `local/` に閉じ込める。CMoA から見ると、それらは
  `http_error` か `no_diff` として候補に現れる。
- **`extra_body` は検証されない生の JSON である。** サーバー固有キーの綴り間違いは、サーバーが
  黙って無視するかエラーを返すかのどちらかで、CMoA は前者を検出できない。
- **設定順が選択の意味を持つ（ADR-0006 D1）ため、配列の並べ替えが結果を変える。** これは意図した
  決定論だが、「順番を入れ替えたら別の候補が採られた」という現象を uzushio 側で説明できるよう、
  `select.json` に `order` を明示的に書く。

## 関連ADR

- 0002（v0 のスコープ）— 依存ゼロと型表現の方針が D2・D1 の形を決めている
- 0004（候補の表現）— D1 が受け取ったテキストを diff として切り出す規則
- 0006（選択規則）— D3 の設定順が「最初の通過者」の意味を与える
- 0007（トレース）— D2 の実効設定、D5 の (n, f) を書く場所
