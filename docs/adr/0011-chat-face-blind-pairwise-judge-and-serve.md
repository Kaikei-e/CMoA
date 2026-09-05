---
title: "v1 のスコープ：チャット面を足す——盲検の総当たり pairwise 審判、task.json v3、judge と serve。0009 の決定を引き継ぐ"
status: accepted
date: 2026-09-06
supersedes: [0009]
depends-on: [0003, 0006, 0007, 0008, 0010]
---

# 0011: v1 のスコープ：チャット面を足す——盲検の総当たり pairwise 審判、task.json v3、judge と serve。0009 の決定を引き継ぐ

## ステータス

Accepted

採択日: 2026-09-06

## 日付

2026-09-06

## コンテキスト

### ロードマップの 7 番目

ロードマップの Step 7 は「チャット面：別のアクセラレータ上の盲検単一審判、無作為化と位置スワップの提示、
較正ログ」である。Step 1〜6 は 2026-09-05 までに出荷された（DocDag の公開 `config`、uzushio の vault
設定、`task doctor`、CMoA v0、`run` と `improve`、前プロジェクトの制約の条項化）。コーディング面では
選択器は verifier で、審判 LLM を使わない（0006 D1）。チャット面には verifier がない。選ぶのは推論的な
grader であり、その grader 自身が較正の対象になる。

### 調査で分かったこと（2026-09-05〜06）

- **選択型 > 合成型**。Selection Bottleneck（arXiv 2603.20324）は 42 課題 210 試行で、審判による選択が
  MoA 型の合成に勝率差 +0.63 で勝ち、合成が基準を上回った課題は 0 件。「選択器の質は提案者の多様性より
  効くレバー」。Nine Judges（2605.29800）は審判 9 体のパネルが実効 2 票にしかならないことを示す。
  単一審判・合成なしは維持する。
- **pointwise 採点の argmax は選択に使えない**。arXiv 2603.12520：全体相関 0.47 の審判は pointwise で
  oracle best-of-N の利得の 21% しか回収できず、粗い点数は 67% のペアを同点にする。pairwise に替えると
  回収率は 61% に上がる。
- **位置スワップの不一致は常態**。MT-Bench（2306.05685）表 2 で GPT-4 のスワップ一致は 65%、Claude-v1 は
  24%。再質問で安定した判定に達するには 11 回要る（2606.13685）。不一致を「決められない」と正直に
  返すのが唯一の一貫した扱いで、決定論的な代替規則（先頭、短い方、最初の提案者）は隠したはずの位置・
  長さバイアスを設計として復活させる。
- **JSON のキー順が効く**。arXiv 2408.02442：形式制約は推論を劣化させ、JSON モードは `answer` を
  `reason` の前に出して思考を迂回する。`reason` を先に固定する。実機の測定で、gpt-oss 系の chat 形式は
  生の GBNF `grammar` と衝突して HTTP 500 になり、`response_format: json_schema` なら通り、
  プロパティ順は送った順に保たれた（ただし Go の `map` は JSON でキーをソートするので構造体で送る）。
- **オープンウェイトは推論量を増やすと審判として悪化する**（2512.01232）。`reasoning_effort: low` を既定に。
- **審判の隔離にハードウェアの分離は要らない**。Self-Harness（2606.09498）も AHE（2604.25850）も
  審判・verifier のハードウェア隔離を論じていない。根拠になるのは自己選好バイアス（2410.21819、10〜25%）で、
  その対策は**別のモデル系列**である。手元の測定では、提案者と同じローカル GPU 上の審判の方が別のアクセラレータ上より速く、
  同時常駐でき、量子化も元の形式のまま使える。別のアクセラレータは平行して試し、依存にはしない。
- **較正の統計**。3 値の κ ≈ 0.6 で CI ±0.10 には約 170 項目要る。人間ラベル付き・寛容ライセンス・同一
  プロンプトに 3 モデル以上の応答、を同時に満たす公開データは 1 つしかなく（uzushio 側の記録に譲る）、
  同点・棄権・不正出力の扱いは前処理ではなく別の量の推定なので、扱いの名前を全数値に添える（2606.00093）。

### 所有者の判断（2026-09-05、3 ラウンド）

Step 7 は **CLI と OpenAI 互換 HTTP の両方**。チャット Task は **task.json version 3 の `face: chat`**。
`serve` は選ばれた応答に**メタ情報を添えて**返し、決められないときは **HTTP エラー**。審判は
**総当たり pairwise × 位置スワップの 6 コール**、モデルは提案者と別系列の **gpt-oss-20b**、置き場は
**提案者と同じローカル GPU**（別のアクセラレータは平行試験）。人間ラベルは**公開データで種を撒き、所有者がブラウザのページで少量**。

## 決定

### D1. v1 はコーディング面にチャット面を足す（0009 D1 を改める）

コーディング面は 0009 のまま：verifier 通過で選び、審判を使わない。チャット面は単一の審判 LLM が
3 候補から 1 つを選ぶ。合成しない。パネルも投票もしない。Task が面を宣言し（D3）、`propose` と
`select` は面で分岐する。トレースは同じ `runs/<run-id>/` に置く（0007）。

### D2. コマンドは 6 つ。`serve` だけ常駐する（0009 D2 を改める）

```
cmoa propose --task <dir> [--config <file>] [--as-of YYYY-MM-DD] [--run-id <id>]
             [--harness <dir>] [--seed <int>] [--temperature <float>]
cmoa select  --task <dir> [--config <file>] [--run <run-dir>]
cmoa verify  --task <dir> --diff <file> [--config <file>] [--out <dir>] [--timeout <dur>] [--label <name>]
cmoa judge   --task <dir> --candidate <file>... [--config <file>] [--run-id <id>] [--seed <int>] [--judge-seed <int>] [--harness <dir>]
cmoa serve   --config <file> [--listen <addr>] [--harness <dir>] [--as-of YYYY-MM-DD] [--allow-remote]
cmoa surfaces [--format text|json]
cmoa version
```

`verify` は 0009 D2 のまま。`judge` は**外から与えた候補**（`c1`…`cN`、与えた順）に対して D4 の審判だけを
行い、提案者を呼ばない。較正コーパス（人間ラベル付きの既存応答）を審判に通す口で、run.json は
`candidates_origin: external` と各ファイルの sha256 を記録する。`--seed` は提示の nonce だけを動かし、
審判のサンプリング seed は `--judge-seed` でしか動かない——再実行一致率を測るとき、動かしたものが
1 つでなければ何を測ったか分からない。

`serve` は OpenAI 互換の `POST /v1/chat/completions` と `GET /v1/models` を出す。1 リクエスト = 1 Task
ディレクトリ（`serve.runs_dir/<run-id>/` に task.json v3 と conversation.json を生成）= 1 run。
応答は選ばれた 1 応答を `choices[0]` に、`usage` は提案者と審判の合計、拡張フィールド `cmoa` に
run-id・選択の種類と理由・審判のコール数と位置一致ペア数・ハーネスの木のハッシュを載せる。
**選ばれた提案者の id は応答に載せない**（トレースにある）。`stream: true` は選択後に 1 チャンクと
`[DONE]` を SSE で送る擬似ストリーム——選択は全応答が揃ってからしか起きない。決められないとき
（D4 の `NoCandidate`）は **502** に OpenAI 形式の `error`（`type: no_candidate`、`code` にサブ理由、
`param` に run-id）、審判のタイムアウトは 504、審判の障害は 502 `judge_failed`、入力不正は 400。
候補を並べて返す代替案は「クライアントが選ぶ」ことになり、選択型 MoA の約束から外れるので採らない。
既定は loopback だけに bind し、それ以外は `--allow-remote` を要求する。認証はない。`max_inflight`
（既定 1）で同時選択数を絞る。

### D3. task.json は version 3 を受け、`face` を持つ。v1 / v2 は変えない

v3 は `face: coding | chat` を必須にする。coding は v2 と同じフィールド。chat は `conversation`
（`{role, content}` の配列、末尾は user、UTF-8、空なし）、任意の `reference.answer`（**審判だけ**が見る）、
任意の `rubric`（審判だけ）、`judge.allow_tie`（既定 true）を持ち、`repo` / `rev` / `files` / `verify` /
`mutants` / `doctor` / `reference.diff` を**持ってはならない**（exit 3）。v1 / v2 に `face` を書くのも拒否する。
`instruction.md` はチャット Task では読まない。`max_context_bytes` は conversation とハーネスの合計に効く。

チャット面の `propose` は 0003 のルータそのまま：全提案者に同じメッセージ列（チャット面用の短い
system テンプレート + 0010 のハーネス注入 + conversation）を同時に送る。候補は本文（reasoning を
剥いだもの）で `candidates/<id>.txt`、空なら `empty`。候補には `reasoning_bytes` と
`usage.reasoning_tokens` を記録する——`finish_reason: length` で本文が空のとき、「答えが空」と「予算を
全部考えるのに使った」を区別できるのはこの数字だけである。

### D4. 審判は盲検・総当たり pairwise・位置スワップ・Condorcet。決められなければ `NoCandidate`

- **盲検**。審判は提案者 id、モデル名、応答長、応答時間を受け取らない。候補は `A` / `B` と呼ばれ、
  候補と提案者の対応はトレースにだけある。総当たりで両順序を必ず見せるので、候補の並べ替えはどの
  リクエストも変えない（レビューで判明）。提示の seed が動かすのは nonce で、`--seed`（無ければ run-id）
  から決定的に導いて記録する——再実行で送るバイト列を変える「無関係なトークン」の摂動であり、審判の
  サンプリング seed とは別。
- **6 コール**。3 ペア × 2 順序。ペアは**両順序で同じ候補を選んだときだけ**その候補の勝ち。不一致、
  どちらかが `tie`、どちらかが不正出力なら**引き分け**（片方が不正だったから勝ち、は無い）。
  2 勝の候補が一意なら `Selected`（Condorcet 勝者）——勝者に関わらないペアが timeout や障害でも勝者は
  立つ。それ以外は `NoCandidate` にサブ理由
  `cycle` / `no_majority` / `all_draws` / `invalid_output` / `too_few_candidates`。**再質問は JSON 形式の
  再送 1 回だけ、決定論的な代替規則は置かない**。`ok` な候補が 1 つ以下なら `too_few_candidates`——
  1 つの答えは「選択」ではない。
- **出力は JSON 1 オブジェクト** `{"reason": <=400 字, "choice": "A"|"B"|"tie"}`。`reason` を先に置く
  （2408.02442）。`judge.output_format` が `json_schema` なら `response_format` で同じ順のスキーマを送る。
  生の `grammar` は送らない（実機で chat 形式と衝突）。`allow_tie: false` なら列挙から `tie` を落とす。
  解析は末尾の釣り合った `{…}` を取り、未知キー拒否・列挙のみ。
- **注入対策**。候補は呼び出しごとの nonce 付きデリミタ `<candidate id="A" n="…">…</candidate:…>` で囲み、
  本文中の閉じタグ様の列はエスケープして記録し、C0 制御文字は落とす。指示と出力形式は候補の後に
  もう一度置く（サンドイッチ）。注入語句の一致は**フラグとして記録するだけで、判定に使わない**——
  較正でフラグ付き候補の勝率を見る材料。
- **記録**。`judge/<i>-<ab|ba>.json` に送ったメッセージ列と生の応答（API キーは除く）、`judge.json` に
  seed と nonce・ペアごとの両順序の結果と一致・引き分けの理由（tie / disagree / invalid / unmeasured）・
  勝ち数・結末・再送回数・サニタイズ・フラグ・遅延・候補の文体量（トークン数、見出し・箇条書き・強調・コードフェンスの数）。審判が見たものは全部
  再構成できる。
- **審判の設定**は cmoa.json version 2 の `judge`：`base_url`、`model`、`temperature`（既定 0）、
  `max_tokens`、`timeout_seconds`、`seed`（nil なら run-id から導き、記録）、`parallel`、
  `output_format`、`extra_body`（`response_format` / `grammar` / CMoA が設定するキーは拒否）。
  `judge` の無い設定でチャット Task を回すのは exit 3。version 1 の設定はそのまま有効で「審判なし・
  serve なし」を意味する。
- 0006 の直和型は `NoCandidate` に `Reason`、新しく `JudgeFailed{Err}` を足す。`JudgeTimeout` は
  0006 で宣言だけされていたものが初めて生成される。timeout と障害が同時なら timeout が勝つ。

### D5. 較正は CMoA の外。CMoA は測れる形で記録するだけ

信頼性（位置スワップ一致、再実行一致）と妥当性（人間一致）を κ で測り、扱いの名前を添えて記録し、
妥当性が測られなくなったら警告する、のは uzushio の仕事である（uzushio ADR 0009）。CMoA が約束するのは
D4 の記録、`judge` コマンド、`--seed` と `--judge-seed` の分離、そして**推論的 grader の判定を自分では
合否に昇格させない**こと——`serve` の 502 はその現れである。

### D6. 0009 の残りの決定を引き継ぐ

task.json v2（reference、mutants、doctor）、`verify` の意味と終了コード、`verify.kind: band`、
Go 1.27 と標準ライブラリだけ、型の表現（定数 + `exhaustive`、封印インターフェース + `gochecksumtype`、
`(T, error)`、構築時検証）は 0009 D2〜D5 のとおり。HTTP サーバーの実装も標準ライブラリ
（`net/http`）で、依存は増えない。

## 根拠（調査結果・出典）

- Selection Bottleneck 2603.20324、Nine Judges 2605.29800、pointwise の限界 2603.12520、MT-Bench 2306.05685
  （表 2、スワップの保守則）、再質問の不安定さ 2606.13685、位置整合性 2406.07791。
- JSON 形式と推論 2408.02442、推論量と審判精度 2512.01232、温度 0 の非十分性 2606.26185、Rating Roulette
  2510.27106（決定論化が人間一致を下げうる——較正で測る項目）。
- 注入：JudgeDeceiver 2403.17710、2504.18333、RobustJudge 2506.09443。
- 隔離の根拠：自己選好バイアス 2410.21819。Self-Harness 2606.09498、AHE 2604.25850 はハードウェア隔離を
  論じない。
- 報告の作法：Agreement Metrics 2606.00093、信頼性と妥当性の分離 2606.19544。
- 実機測定（2026-09-06）：gpt-oss-20b の chat 形式と生 GBNF の衝突、`response_format` の通過、
  `reasoning_effort: low` の効き、審判 1 コール 3〜30 秒（提案者と同時実行時）。

## 検討した代替案

- **listwise（3 候補を 1 プロンプトで順位付け）**。不採用。大域整合性が弱く、ペア単位のスワップ一致が
  測れない。
- **pointwise 採点の argmax**。不採用（2603.12520）。
- **勝者が早く決まったら残りのペアを省く**。不採用。推移性を仮定し、較正でペアが欠ける。
- **不一致時の決定論的フォールバック、または再質問**。不採用（上記）。
- **`serve` が `NoCandidate` で候補を全部返す**。不採用（所有者）。選択型の約束から外れる。
- **審判を別ハードウェアに置くことを要件にする**。不採用。根拠が無く、実測で遅い。設定で差し替え可能に
  しておき、平行して試す。
- **審判パネル・投票**。不採用（2605.29800、0002 以来）。
- **チャット Task を別ファイル形式にする**。不採用。task.json に `face` を足す方が Task の道具
  （doctor、suite、トレース）を共有できる。

## 影響とトレードオフ

- 得るもの：チャット面で「決められた／決められなかった」が全部トレースに残り、上位層が審判の信頼性と
  妥当性を測れる。既存のクライアントは OpenAI 互換 HTTP で CMoA を試せる。
- 失うもの：1 選択に審判 6 コール（提案者と同時実行で数十秒〜数分）。`NoCandidate` は珍しくない
  （MT-Bench の GPT-4 でスワップ一致 65%）——`serve` の 502 は頻繁に見えるだろうが、それは審判の
  現状を映している。
- リスク：8〜20B 級の審判の妥当性は較正されるまで不明。チャット面の edit の自動受理は較正ログが
  揃うまで人の承認を残す（uzushio 側）。

## 関連ADR

- 0003（ルータ）、0006（選択の直和型——拡張）、0007（トレース）、0008（面と自律度）、0009（supersede）、
  0010（ハーネス注入はチャット面にも効く）、uzushio ADR 0009（較正ログ）
