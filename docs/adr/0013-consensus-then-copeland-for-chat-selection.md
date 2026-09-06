---
title: "チャット面の選択：まず候補どうしの合意、次に Copeland スコアと記録される同点解消。0011 の D4 だけを改める"
status: accepted
date: 2026-09-06
supersedes: ["0011"]
depends-on: ["0003", "0006", "0007", "0008", "0010"]
---

# 0013: チャット面の選択：まず候補どうしの合意、次に Copeland スコアと記録される同点解消。0011 の D4 だけを改める

## ステータス

Accepted

採択日: 2026-09-06

本記録は ADR-0011 を supersede する。0011 の決定 D1・D2・D3・D5・D6 は文言を変えずに引き継ぐ。改めるのは
**D4 だけ**——正確には「勝ちは両順序一致のときだけ／Condorcet 勝者が居なければ `NoCandidate`」という結末の
規則と、同じ D4 の中の「**決定論的な代替規則は置かない**」の一節である。盲検、6 コール、出力 JSON の形と
キー順、注入対策、記録の粒度、審判の設定は D4 のまま残る。0011 が宣言する `depends-on`（0003、0006、0007、
0008、0010）もそのまま引き継ぐ。

## 日付

2026-09-06

## コンテキスト

### 出荷直後に測れたこと（2026-09-06）

Step 7 を出荷した当日、`cmoa serve` に投げた 11 リクエストのうち **10 が HTTP 502 `no_candidate`** で返った。
較正コーパス 400 run では **217 が `no_candidate`**（`no_majority` 175、`all_draws` 30、`invalid_output` 11、
`cycle` 1）、`selected` は 183 だった。障害は 1 件も無い。6 コールは全部返っており、規則が決めることを
拒んでいるだけである。

内訳を読むと、拒んだ相手が問題だった。

- 「1足す100は？」——提案者 3 体とも `101` と答え、3 ペアとも両順序で `tie`。**全員が正解で一致している**
  run が `all_draws` になった。
- 「224かける１２３４は？」——2 体が同じ正しい数を答えて 3 体目にそれぞれ勝ち、その 2 体どうしは `tie`。
  勝ち数は 1・1・0 で Condorcet 勝者が立たず `no_majority`。**正解が過半数を占めている** run が拒まれた。

つまり CMoA は、**候補どうしの一致という最も強い正しさの信号を「情報が無い」として捨て**、そのうえで
「決められなかった」と報告していた。`no_majority` が拒否の 8 割を占めるのは、1 つの引き分けが明らかな
首位を塞いだ、という形が支配的だからである。

### 調査で分かったこと（2026-09-06）

- **一致は選択器そのものである**。Self-Consistency は複数の推論路の最終答えを多数決するだけで GSM8K を
  +17.9 点上げた（[2203.11171](https://arxiv.org/abs/2203.11171)）。Universal Self-Consistency は自由文へ
  拡張し「we propose Universal Self-Consistency (USC), which leverages LLMs themselves to select the most
  consistent answer among multiple candidates」（[2311.17311](https://arxiv.org/html/2311.17311)）。
  Agent-Forest は「This involves calculating the cumulative similarity for each sample relative to the
  others […]. The sample that exhibits the highest cumulative similarity is then chosen as the final
  answer」（[2402.05120](https://arxiv.org/html/2402.05120)）。Smoothie / MBR も
  「selects the generation that has the highest average similarity (i.e., cosine) with other generations」
  （[2412.04692](https://arxiv.org/html/2412.04692)）。
- **算術では審判より多数決である**。Stephan らは審判を答えの選択器として測り、
  「Rather, we find that it is more sensible to use the judge model as an answer generator, and
  subsequently take the majority vote of all three answers」「the majority vote is more likely to be
  better than the answer chosen by the judge」と書く（[2409.04168](https://arxiv.org/html/2409.04168)。
  この記録の調査段階で Sharma et al. と誤記していたが、著者は Stephan et al. である）。
- **スワップ不一致は「同点」であって「無」ではない**。MT-Bench の原文は
  「If the results are inconsistent after swapping, we can call it a tie」
  （[2306.05685](https://arxiv.org/abs/2306.05685)）。Shi らはこの扱いを **inconsistency-as-a-tie** と
  名付け、「researchers proposed 'inconsistency-as-a-tie' for both candidate models in pairwise
  comparative settings to consider all judgments for further analysis」と記す
  （[2406.07791](https://arxiv.org/html/2406.07791)）。Chatbot Arena と Arena-Hard は同点を
  Bradley–Terry に半勝として畳む（[LMSYS 2023](https://www.lmsys.org/blog/2023-12-07-leaderboard/)、
  [2406.11939](https://arxiv.org/html/2406.11939)。`arena-rank` の該当行そのものは二次情報でしか確認して
  いない）。**0011 は前半（両順序一致を要求する）だけを実装し、後半（同点を 0.5 として数える）を実装して
  いなかった。**
- **引き分けは「差が小さい」という測定である**。Shi ら：「the answer pairs or series with larger quality
  disparities are easier to achieve judgment consistency, whereas those of similar quality are difficult
  to judge, increasing the likelihood of position bias」（同上）。引き分けの多さは審判の故障ではなく、
  候補が近いことの現れである。
- **審判の長さバイアスは「長い方」に向く**。AlpacaEval は「known to favor models that generate longer
  outputs」（[2404.04475](https://arxiv.org/html/2404.04475)）、OffsetBias は
  「length bias becomes influential when the bad / good response length ratio surpasses 2.0」
  （[2407.06551](https://arxiv.org/html/2407.06551)）、小さい差では
  「In the 0-10 length difference interval, the preferences of all evaluators are near 0.5」
  （[2402.10669](https://arxiv.org/html/2402.10669)）。生成側では冗長さが質の悪化と相関する——
  「most of the datasets and models show lower performance on verbose samples」
  「verbose responses exhibit higher uncertainty across all five datasets」
  （[2411.07858](https://arxiv.org/html/2411.07858)）。ただし
  「the relationship between output length and judge accuracy is dataset-specific and cannot be simply
  summarized as *longer is worse*」（[2606.01629](https://arxiv.org/html/2606.01629v1)）。
- **決定論的な同点解消には先例がある**。審判 9 体のパネルの記録は
  「These are broken via deterministic SHA-256 hashing of the item index and vote sequence, ensuring
  reproducibility and avoiding insertion-order bias」と書く（[2605.29800](https://arxiv.org/html/2605.29800)、
  Appendix K）。0011 が「審判パネルは実効 2 票」の根拠に挙げたのと同じ記録である。
- **固定の提案者優先順位を支持する根拠は無い**。「no single model can optimally address all tasks and
  applications」（RouterBench、[2403.12031](https://arxiv.org/html/2403.12031)）。
- **同点を許さない強制選択には利得の報告がある**。GenArena は
  「The resulting +29.0% gain demonstrates that enforcing binary decisions is imperative」と書くが
  （[2602.06013](https://arxiv.org/html/2602.06013)）、これは**視覚言語**の審判での測定で、テキストのみの
  審判への転移は未確認である。

### 所有者の判断（2026-09-06）

段階を 2 つにする。**まず候補どうしを比べ、過半数が同じことを言っていればそれを返す**（審判コール 0）。
**一致しなければ 6 コールはそのまま回し、引き分けを 0.5 として数える**。数えても並ぶ候補は、記録される
決定論的な鍵の連鎖で解く。`outcome.kind` の語彙は動かさない。合成はしない。強制選択の追加コールは
いまは置かない。

## 決定

### D1. 0011 の D4 のうち、結末の規則と「決定論的な代替規則は置かない」を改める

0011 D4 の以下は**そのまま**である：盲検（提案者 id・モデル名・応答長・応答時間を審判に渡さない）、
総当たり 3 ペア × 2 順序の 6 コール、提示 nonce と `--seed` / `--judge-seed` の分離、
出力は `{"reason", "choice"}` の 1 オブジェクトで `reason` が先、`response_format: json_schema`、
生 `grammar` を送らないこと、nonce デリミタとサンドイッチと注入フラグ（記録するだけで判定に使わない）、
`judge` 設定ブロック、`judge.allow_tie`（既定 true）、**JSON 形式の再送 1 回だけで再質問はしない**こと。

改めるのは 2 つ：**(a)** ペアの結果から 1 つを選ぶ規則、**(b)**「決定論的な代替規則は置かない」の一節。
0011 D1・D2・D3・D5・D6 は文言を変えずに引き継ぐ。`serve` の HTTP 対応（D2）も変えない——変わるのは
502 が出る**頻度**であって、502 の**意味**ではない。

### D2. 段階 1——正規化した答えの合意。過半数が一致すれば審判を呼ばない

各候補の答えを正規化する。正規化は次の順で、手書きの互換分解である（0009 D2 / 0011 D6 の「標準ライブラリ
だけ」を守るため、`golang.org/x/text` は入れない）：

- 互換の畳み込み——全角 ASCII ブロック（U+FF01–FF5E）を ASCII に、U+3000 を空白に。`１０１` と `101` は
  同じ答えである。半角カナや丸数字など NFKC の残りは畳まない。
- 小文字化。
- markdown の強調記号 `*` `_` `` ` `` を除去。
- 行頭の箇条書き記号と番号を 1 つ除去。番号は**後ろが数字でないときだけ**マーカーと見なす
  （`1. 答え` は項目、`101.` は答え、`1.101` は小数）。
- 空白の連続を 1 つに畳み、両端を落とす。
- 末尾の `.` `。` `!` `?` を落とす。

2 つの答えが**一致する**のは、(i) 正規化した文字列が等しいとき、または (ii) **両方が 160 runes 以下で、
両方に数が含まれ、末尾の数が桁区切りを外した正準形で等しい**とき。末尾の数を見るのは、筆算は被演算子を
先に、結果を最後に書くからである。長さの上限は、数を 1 つ共有するだけの長文どうしを一致と呼ばないための
ものである。どちらの検査も質問も `reference.answer` も読まない——共有された読み違いで一致に持ち込まれない
ようにするためである。

候補を一致で分割し、**厳密な過半数**が互いに一致する群があれば、その群から 1 つ（D4 の連鎖で選ぶ）を
`Selected` として返す。**審判は 1 コールも呼ばない。** 3 体なら過半数は 2 体なので、「2 体が一致、1 体が
異論」というよくある形がここで決まる。結末の理由は
`consensus: 2 of 3 agree on the normalised answer (numeric)` の形で、`judge.json` の `consensus` に
正規化の版（`nfkc-v1`）・群の分割・選ばれた候補・一致の種類（`exact` / `numeric`）を記録する。

根拠は §コンテキストの一致系（2203.11171、2311.17311、2402.05120、2412.04692）と、審判より多数決を
勧める測定（2409.04168）である。

### D3. 段階 2——6 コールはそのまま、引き分けを 0.5 と数える Copeland スコア

一致が無ければ 0011 D4 の 6 コールをそのまま回す。ペアの判定も 0011 のまま：**両順序が同じ候補を選んだ
ときだけ**その候補の勝ちで、不一致・どちらかが `tie`・どちらかが不正出力なら引き分け。改めるのは
引き分けの**数え方**である。

- 勝ち **1**、引き分け **0.5**（両方に）、負け **0**。
- 0.5 が付くのは、審判が**答えた**引き分け——`draw_reason` が `tie` か `disagree` のとき。順序が割れたのは
  「位置が喋った」という測定であり、MT-Bench の「we can call it a tie」の読みそのものである。
- `invalid`（解析できない）と `unmeasured`（timeout・送信不能）の引き分けは **0**。機械の故障は判定では
  なく、それに半勝を払えば「到達できない審判」が結末を決めてしまう。

Condorcet 勝者（`need = n-1` ペアを全勝した候補）が居ればこれまでどおり選び、理由の文言も
`condorcet winner, …` のまま変えない。居なければ**スコアの argmax** を選び、理由は
`copeland winner, score 1 of 2 (no condorcet winner)`。順位（`ranked`）は勝ち数ではなくスコア順になる——
全ペアが引き分けた run では勝ち数が全員 0 で、`ranked` が提案者順に退化していた。

**機械の故障はスコアより上位に残る。** 0011 D4 の escalate はそのまま：答えの無いペアがまだ勝者を左右し得る
なら `judge_timeout` / `judge_failed`（timeout が優先）、左右し得ないなら勝者は立つ。解析できないペアが
あれば `no_candidate` / `invalid_output`。比べる答えが 2 つ未満なら `too_few_candidates`。

### D4. 同点は 3 つの鍵の連鎖で解き、効いた鍵を記録する

最高スコアの候補が複数あるとき（および D2 の一致群から 1 つ選ぶとき）、次の順に鍵を当て、**最初に集合を
1 つに絞った鍵が決める**。効いた鍵は `judge.json` の `tie_break`（`among` / `key` / `chosen`）に記録し、
理由の文言にも出す。

1. **合意中心性**（`consensus`）——run の**他の全候補**のうち、正規化した答えが一致する数が最も多い候補。
   負けた候補との一致も一致である。USC / Agent-Forest / MBR の「最も似ているものを選ぶ」規則そのもので
   （2311.17311、2402.05120、2412.04692）、pairwise の審判が捨てている唯一の信号である。
2. **短い方（ゲート付き）**（`length`）——正規化した答えが**一意に最短**で、かつ**最長が最短の
   `lengthGateRatio = 1.5` 倍以上**のときだけ効く。比が届かなければこの鍵は決めない。
   `lengthGateRatio` は**調整可能な定数であって、測定された最適値ではない**——差が小さい領域では選好が
   0.5 付近（2402.10669）、支配的になるのは 2.0 倍から（2407.06551）という 2 つの測定の間を取っただけで、
   1.5 を測った文献は無い。
3. **正規化テキストの SHA-256 が最小**（`hash`）——恣意的だが、**答えそのものについてだけ**恣意的である。
   提示位置でも提案者でも長さでもなく、同じ答えなら毎回・どの機械でも同じ勝者になる。決定論的な同点解消の
   先例は 2605.29800 Appendix K。

**明示的に採らない鍵**：提示位置、先頭に並んだ候補、固定の提案者優先順位、提示 nonce のハッシュ。
前 3 つは 0011 が正しく退けたものである——スワップは位置バイアスを検出するために在り、Shi らの測定では
位置バイアスが最も強く出るのは**まさに同点の領域**（2406.07791）で、固定の提案者順を支持する根拠は無い
（2403.12031）。nonce のハッシュは再実行で答えが変わるので、応答する系には使えない。

**0011 の論法にはここで正面から答える。** 0011 は「決定論的な代替規則（先頭、短い方、最初の提案者）は
隠したはずの位置・長さバイアスを設計として復活させる」と書いた。**位置についてはこの論法は完全に正しく、
本記録もそれを守る**（上の 3 つを退けたのはその理由である）。**長さについては転移しない**：スワップが
検出するのは位置であって長さではなく、スワップを飛ばしたところで長さの補正が失われるわけではない。
そして測定された審判の長さバイアスは**すべて長い方に向いている**（2404.04475、2407.06551、2402.10669）
ので、「短い方」は審判のバイアスの復活ではなくその逆向きの梃子である。それでも無条件の「短い方が勝ち」は
それ自体が付け入られる規則なので、比のゲートを置いて、差が本物のときだけ効かせる。冗長さと正確さの関係が
データセット依存である（2606.01629）ことは、この鍵を連鎖の 2 番目に置き、1 番目を合意にした理由である。

### D5. 語彙とスキーマは足すだけ。`outcome.kind` は動かさない

- `outcome.kind` は `selected` / `no_candidate` / `judge_timeout` / `judge_failed` のまま。一致で選んだ
  ものも、スコアで選んだものも、同点を解いて選んだものも `selected` + `candidate_id` である。上の層
  （uzushio の `judge calibrate`）はこれを選択として数え続ける。
- 残る `no_candidate` は **`too_few_candidates` と `invalid_output` だけ**で、`serve` ではこれまでどおり
  **502**（0011 D2）。`cycle` / `no_majority` / `all_draws` は**語彙には残すが、もう生成されない**——
  以前に書かれたトレースがこの語を持っているからで、定数には「歴史的」と註を付ける。
- `judge.json` に足すのは任意フィールド 3 つ（`scores`、`consensus`、`tie_break`）だけなので、
  **`schema_version` は 1 のまま**である（0007 の規則：意味の変わるフィールドがあれば上げる、任意の
  フィールドを足すだけなら上げない）。`select.json` の `rule` は `judge-pairwise` から
  **`consensus-then-copeland`** になる。
- `serve` の `cmoa` 拡張は `selection.score`、`selection.consensus`（正規化・一致の種類・一致数・母数）、
  `selection.tie_break`（鍵と同点の数）を出す。**候補 id は出さない**——理由の文字列も、同点の候補名を
  並べる末尾を落として返す。どの提案者が書いたかがトレースにしか無いのは 0011 D2 のままである。

## 根拠（調査結果・出典）

- 一致を選択器として使う系列：Self-Consistency [2203.11171](https://arxiv.org/abs/2203.11171)、
  Universal Self-Consistency [2311.17311](https://arxiv.org/html/2311.17311)、
  Agent-Forest [2402.05120](https://arxiv.org/html/2402.05120)、
  Smoothie / MBR [2412.04692](https://arxiv.org/html/2412.04692)、
  審判より多数決 [2409.04168](https://arxiv.org/html/2409.04168)（Stephan et al.）。
- 同点の会計：MT-Bench [2306.05685](https://arxiv.org/abs/2306.05685)（"we can call it a tie"）、
  Chatbot Arena の Bradley–Terry 移行 [LMSYS 2023](https://www.lmsys.org/blog/2023-12-07-leaderboard/)、
  Arena-Hard の 2 ゲーム構成 [2406.11939](https://arxiv.org/html/2406.11939)、
  inconsistency-as-a-tie と同点領域の位置バイアス [2406.07791](https://arxiv.org/html/2406.07791)。
- 長さ：[2404.04475](https://arxiv.org/html/2404.04475)、[2407.06551](https://arxiv.org/html/2407.06551)、
  [2402.10669](https://arxiv.org/html/2402.10669)、[2411.07858](https://arxiv.org/html/2411.07858)、
  反対側の注意として [2606.01629](https://arxiv.org/html/2606.01629v1)。
- 決定論：[2605.29800](https://arxiv.org/html/2605.29800) Appendix K（SHA-256 による同点解消）。
- 固定順の否定：[2403.12031](https://arxiv.org/html/2403.12031)。
- 強制選択：[2602.06013](https://arxiv.org/html/2602.06013)（+29.0pp、ただし視覚言語）。
- 合成型を採らない根拠は 0011 のまま：Selection Bottleneck 2603.20324、MoA
  [2406.04692](https://arxiv.org/abs/2406.04692)。
- 実測（2026-09-06）：`serve` 11 リクエスト中 10 が 502、較正コーパス 400 run 中 217 が `no_candidate`
  （`no_majority` 175、`all_draws` 30、`invalid_output` 11、`cycle` 1）。

**根拠が無いと明示しておくもの**：3 つの自由文候補に対する Copeland の同点解消を測った文献は無く、この
構成は外挿である。提案者 3 体でスコアがちょうど並ぶ頻度は**未測定**である。ゲート比 1.5 を測った文献は
無い。短さが**選択**の精度を上げるという結果は無い（2411.07858 は生成側の測定である）。GenArena の利得の
テキスト審判への転移は未確認である。

## 検討した代替案

- **Copeland だけ（段階 1 を置かない）。** 不採用。「1足す100は？」は 3 体一致・全ペア引き分けなので、
  スコアが 1.0 で 3 つ並び、**全員が同じ正解を書いているのに同点解消の鍵が選ぶ**ことになる。一致は
  審判の判定より強い信号で（2409.04168）、しかも 6 コール分の待ち時間を丸ごと省ける。
- **Copeland + 強制選択の追加コール**（上位 2 候補に `tie` を外した 1 ペアを両順序で再度当てる）。
  不採用（いまは置かない）。GenArena の +29.0pp は視覚言語の審判での測定で転移は未確認、追加 2 コールが
  round の待ち時間に直に乗り、再度割れれば結局は決定論的な鍵に落ちる。**将来の選択肢として残す**——
  上位が 2 候補のときだけ、既定は off。
- **本来の MoA（アグリゲータが新しい答えを書く）。** 不採用。0011 D1 の「合成しない」を破り、
  「返るのは提案者が書いた答えそのもの」という製品の約束を壊す。審判が居なくなれば較正の対象も消える。
- **決められない残りは 200 で先頭の候補を返す。** 不採用。0011 の論法が完全に効く場所である
  （位置＝提示順を規則にすることになる）。残余の `no_candidate` は 502 のままにする。
- **候補を全部返してクライアントに選ばせる。** 不採用（0011 D2 のまま）。
- **審判に `tie` を出させない（`allow_tie: false` を `serve` の既定にする）。** 不採用。同点の禁止は
  同点を消すのではなく、位置バイアスに吐き出させるだけである（2406.07791：同点領域こそ位置バイアスが
  強い）。`allow_tie` は Task ごとの設定として残す。

## 影響とトレードオフ

- 得るもの：候補どうしの一致が**失敗ではなく最強の信号**として扱われる。よくある算術・事実質問は
  審判コール 0 で決まり、待ち時間が消える。引き分けが情報として数えられるので、`no_majority` の形
  （1 つの引き分けが首位を塞ぐ）が消える。同点は決定論的に解かれ、**どの鍵が効いたかがトレースに残る**。
- 較正の値は動く。これまで棄権していた大半が選択になるので、**スワップ κ・再実行 κ・人間 κ はいずれも
  変わる**。とくに「常に棄権」という退化した一致が消えるので、信頼性の κ は下がりうる。
  さらに、**一致で決まった項目は審判を一度も見ていない**——この変更のあとの較正報告は「審判の較正」では
  なく「選択器全体の較正」であり、そう名乗る必要がある。uzushio 側の較正文書（UZ-C-009）は**この変更の
  あとで測り直す**。
- 正規化はヒューリスティックである。短い散文が同じ末尾の数を共有すると**偽の一致**になりうる
  （160 runes の上限と「数が両方にある」条件はそのための緩衝であって、証明ではない）。逆に、
  半角カナや丸数字は畳まないので、一致を**見落とす**方には倒れる。見落としは 6 コールに落ちるだけなので、
  安全な向きに寄せてある。
- 提案者 3 体でスコアがちょうど並ぶ頻度は未測定なので、同点解消の鍵がどれくらい効くかは**これから測る量**
  である。`tie_break.key` の分布はそのための材料として記録される。
- 失わないもの：`outcome.kind` の語彙、`schema_version` 1、6 コールの記録の粒度、盲検、
  「どの提案者が勝ったかは応答に載せない」。

## 関連ADR

- 0011（supersede——D4 の結末規則と「決定論的な代替規則は置かない」だけを改め、残りは引き継ぐ）、
  0006（選択の直和型——新しい variant は足さない）、0007（トレースの追記規則——任意フィールドの追加は
  `schema_version` を上げない）、0003（ルータ）、0008（面と自律度）、0010（ハーネス注入）、
  uzushio ADR 0009（較正ログ——測り直しが要る）
