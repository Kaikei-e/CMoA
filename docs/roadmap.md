# uzushio・CMoA・DocDag 共同ロードマップ

更新日: 2026-09-09。3プロジェクトの実装と公開研究を調べ直し、今後の優先順位を再策定した。[調査記録](evolution-research-2026-09-09.md)にソース、一次資料、適用限界をまとめる。この文書を共同計画の参照先とし、プロジェクト別の実装・仕様・採否は各repositoryに記録する。

**P0完了（2026-09-09、リリース前の検証）:** D-01・U-01・C-01の動作基準を確認した。[再実施手順](baseline-protocol.md)を基準にP1へ進む。P0は品質改善の採用やjudge校正の合格を示さない。

**P1実装中（2026-09-09）:** chat は数値合意の誤一致を再現し、
[一変更の開発候補](proposals/0018-decimal-only-numeric-agreement.md)を実装した。
uzushio に匿名評価票、複数人ラベルの統合、固定 H の paired 採否評価を追加し、合成 fixture で検証した。
code は render hash と実際の注入本文が一致するよう両 runtime/runner を修正した。
実行中に再現した improve の相対パス不具合と、Docker 接続失敗の不合格扱いも修正・検証した。
chat の開発・回帰 pilot と code の既存 suite による doctor/baseline pilot を実施した。
二重 human annotation、H 評価、memory 一変更の両 split 判定、serve 採用確認は別 gate として残る。評価データ・生成器・ラベル・実行記録は非公開領域に保存し Git から除外する。

目標は、**実行結果から改善候補を作り、独立した評価で採否を決め、その理由と再現条件を追跡できる開発基盤**を完成させること。直近はchatとcodingでそれぞれ一巡を成立させ、その証拠をもとに文脈選別と計算予算の最適化へ進む。

これは将来の作業計画であり、acceptedなADR、仕様、autonomy、選択規則の変更そのものではない。設計を変える実装では該当repositoryに新しいADRを追加し、置換する決定を`supersedes`で示す。

## 3層の責任と受け渡し

| 層 | 主責任 | 受け渡すもの | 他層に委ねるもの |
| --- | --- | --- | --- |
| DocDag | 宣言された文書関係・状態・有効期間の整合性 | binding、resolution、context、validation findings | 統計的な採否、モデル実行、測定artifactの意味 |
| CMoA | 候補生成・検証・選択と実行記録 | CLI/HTTP応答、候補・選択trace、surface/autonomy | harness改善、suite管理、judge校正、採用判断 |
| uzushio | verifier健全性、評価、改善提案、採否と履歴 | render済みharness、評価記録、提案・採用文書 | runtimeの候補選択、DocDagのbinding計算 |

依存方向は`uzushio → CMoA / DocDag`と`CMoA → DocDag CLI`を維持する。CMoAのGo本体は標準ライブラリのみ。monitorはtraceの観測とserveへの中継を担当する。3プロジェクトのリリース時期を揃える必要はなく、対応するバージョンの組合せを適合性検証する。

データは「DocDagのbinding → uzushioのrender → CMoAの実行 → uzushioの評価・採否 → DocDagが読む新しい文書」と巡る。規範、実行記録、評価結果をそれぞれの所有層に残す。

## 現在地

| 項目 | 実装の状態 | 証拠の状態と残る仕事 |
| --- | --- | --- |
| coding runtime、verify、task doctor/mutate/calibrate | 実装済み | 実際に使うsuiteの健全性と環境を固定する |
| render、improve、paired run、逐次判定 | 実装済み | 共有可能な一つのharness変更について、両splitの採否を一巡記録する |
| chatのconsensus/Copeland、単一blind judge、serve | 実装済み | 選択カバレッジ、judge校正、fresh品質を分けて評価する |
| judge replay、D/R/Hを分離するbudgeted trial | 実装済み | replayとfreshを区別し、finalistから採用までの証拠を完成させる |
| serve性能ログ、trial再開fingerprint、コンテナ起動経路 | リリース前の検証済み | Go/monitor/Compose checksと再開動作を確認。リリース状態は各repositoryで管理 |
| DocDagのpublic config、lint、期間、append-only、context | 実装済み | 自身の6 ADRに本文を保持してfrontmatter追加。binding 6件、自己corpus CIを整備 |
| 3層共通の実行条件と採否を結ぶ証拠bundle | 部品は存在 | バイナリ・モデル・入力・render・評価・採否をまとめて再検証する契約は今後の仕事 |

「実装済み」「実fleetで動作確認済み」「独立評価を通して採用済み」を別々に記録する。P0は動作基準の完了、P1以降は未完了。過去の実装実績は末尾に残す。

## 優先順位と依存関係

期間は着手後の目安であり、実測予算とgateの結果に合わせて見直す。P0後のchat/codingは独立に進められるが、同一GPUを使う性能測定は競合させない。既存suiteで判定できる小さな実装を優先し、文書基盤の全面改修を最初の改善実証の待ち条件にしない。

| 段階 | 目安 | 主担当 | 到達する状態 | 完了gate |
| --- | --- | --- | --- | --- |
| P0: 基準と再現条件 | 完了: 2026-09-09 | 全3層 | 何を測ったか、何が有効かを誤読しない | A/A・再開・環境照合、自己corpusのbinding、対応版gate |
| P1-chat: 選択品質と待ち時間 | 2–6週 | uzushio + CMoA | 一つの変更をfresh評価からserveまで追える | 未使用H、経路別品質、事前の許容劣化幅、実応答時間で採否を記録 |
| P1-code: 最初の改善ループ | 2–6週 | uzushio + CMoA | 一つのmemory変更を提案から採否まで再現 | doctor健全、両splitの既存gate、render hash一致、却下を含む記録 |
| P2: 証拠と採用履歴の接続 | 6–10週 | uzushio + DocDag | ある採用状態から根拠と実行条件を辿れる | bundleの再検証、失効・置換・欠落検出、既知のbaselineへの復元 |
| P3: 品質を保つ最適化 | 10–12週以降 | 全3層 | 同じ予算で品質を上げる、または品質を保って費用を下げる | 単一モデルを含むbaseline比較、未使用setで確認、費用対効果と失敗条件を公開 |

### P0 — 測定と文書の基準を固定する

以下の範囲を完了確認した。品質改善の採用、全体serve latency、代表splitの拡充はP1で扱う。

**CMoA:** 既存のserve計測・Compose変更を検証して基準バイナリを確定する。run IDとrequest IDを結び、queue、propose、select、書込みとクライアント受信時間を区別する。成功、候補不合格、judge不正出力、timeout、runner障害、キャンセルを保持する。

**uzushio:** trial再開の既存変更を確定する。同じ条件のfresh A/Aを事前に決めた項目と反復数で完了し、選択変動、測定時間、完了率、再開の一致を報告する。途中終了を品質の失敗と数えず、結果を見て項目を除くこともしない。入力・ラベル・gold・config・バイナリの変更時に再開を拒否する既存fingerprintを検証する。

**DocDag:** 6 ADRの既存本文・決定を保存してfrontmatterを整備し、自身のADR用設定とCI gateを追加する。実装時の逸脱は既存記述を残し、決定を改める場合のみ新ADRで扱う。完了条件はmissing frontmatterゼロ、意図した6件がbindingに出ること（新たなsupersessionを加えた場合は期待集合を更新）、validate/lint成功。移行前後の本文差分を確認する。

共通の基準記録には3 repositoryのrevisionとdirty状態、実行バイナリのdigest、DocDag/preset/schema版、suiteとsplitのdigest、modelと量子化・重み識別子、推論server版、prompt/seed/temperature、並列数、資源条件を含める。取得できない情報はunknownと書く。モデル名やtagだけで同一性を断定しない。

P0 の基準は CMoA が DocDag v0.4.1、uzushio が v0.4.0 だった。
P1 の開発作業で uzushio の依存と CI 指定も v0.4.1 に揃え、生成 config・lint fixture の
内容が変わらないことと DocDag/conformance gate を確認した。P0 の保存済みバイナリと記録は保持する。

### P1-chat — 回答を選べることと、正しく選べることを測る

着手順は「誤選択の診断 → 一変更の探索 → fresh確認 → 採用判定 → serve」である。schema長制限などの速度調整は候補に残すが、合意とtie-breakの品質診断を同じ段階で行う。[MT-Bench研究](https://arxiv.org/abs/2306.05685)と[位置バイアス研究](https://aclanthology.org/2025.ijcnlp-long.18/)を踏まえ、現fleetで確認する。

1. Dは利用分布を代表する開発set、Rは既知の失敗を再現する回帰set、Hは最終候補まで開封しないsetとして、ID・由来・重複・ラベル・構成比を固定する。日本語、数値・単位・否定、短文/長文、候補品質差、multi-turnを含める。人間ラベルの二重annotationと不一致の扱いも決める。
2. 合意、Condorcet、Copeland、tie-break別の正解率と件数、誤合意率、`selected`率、未測定率を出す。すべて誤答の候補集合も用意する。回答中のjudge向け指示や装飾だけを変える回帰項目も含める。
3. aggregation候補は保存応答をreplayして比較する。consensusで省略されたcallは存在しないので、別ルールに必要な応答が欠ける場合はfreshで収集する。judge prompt、schema、推論量、モデル変更の品質比較もfreshで行う。
4. D/Rで一つのfinalistを固定する。Hでは項目単位のpaired差と区間、事前の許容劣化幅、品質下限、必要な標本数、重大回帰の条件で採否を決める。十分な精度が出なければinconclusiveのまま残す。
5. 採用可能な候補をserve経由の固定会話で検証する。クライアントのp50/p95、サーバのqueue/select、token・call数、timeout、不正出力、キャンセルを報告する。warm/coldと並列数を固定する。

速度比較では実行順をcounterbalanceし、キャッシュとwarm/coldの扱いを先に決める。同条件でも生じる順序依存の時間差を、変更による速度改善と見なさない。

現行`judge trial`の`finalist`は点推定に基づく探索上の推奨であり、非劣性の統計的証明ではない。Hでの区間付き採用評価はuzushioの明示的な追加作業とする。固定標本のpaired評価を初期案とし、taskをcluster単位に扱う。必要な標本数はDの変動と狙う効果から先に設計し、Hを見て許容幅を広げない。

judge単体の校正とselector全体の評価は別成果物にする。`binding`なuncalibrated記録を校正合格と解釈しない。校正規則自体を変える場合はuzushioの仕様・適合性テストを更新する。採用した選択規則の変更はCMoA ADR 0013のsupersessionとして記録する。

### P1-code — 実際に注入される一変更を最後まで評価する

既存`task doctor → improve → run → render → propose/select`の経路を使う。初回はmemoryを対象にする。skill本文は現在CMoAに注入されず、tool/middleware/subagentにも実行点の制約があるため、語彙の存在を実装完了と扱わない。

1. 実際に評価するsuiteについてreferenceの誤検出、mutant検出、未適用・runner障害を確認し、healthyなverifierと環境を固定する。
2. held-inの失敗traceから一つのpatternと最小のmemory editを作り、改善予測を事前に記録する。held-outの問題・trace・正解を提案器に渡さない。
3. 同一task/seedのbaseline/candidateを既存`run`で比較し、両splitの判定とcap・未測定を記録する。既存promotion条件とsurfaceのautonomyに従う。
4. 採用なら新bindingからrenderしたhashをCMoAが読んだhashと照合する。却下・保留でもpattern、edit、run、理由を残す。

この段階の完了は「正当な採否が一巡し、再現できること」。改善成功は別に記録し、inconclusiveを採用へ読み替えない。反復seedを増やしても未知taskへの一般化の標本数は増えない。初回の結果が弱ければ、観測された障害に応じてtask・検出器・候補生成のどれを改善するかを決める。

### P2 — 証拠bundleと採用履歴をつなぐ

uzushioが評価の入力・出力manifestを所有する。既存のrun、render、doctor、trialのhashを再利用し、採用記録から次を辿れるようにする。最初は合成fixtureと小さな共有可能な例を使う。

| 接続 | bundleが示すもの | 検証担当 |
| --- | --- | --- |
| proposal → measurement | edit/pattern ID、事前予測、split・ラベル・評価規則のdigest | uzushio |
| measurement → execution | CMoA trace、render hash、モデル/server条件、バイナリdigest、live/replay/reuseの種別 | uzushioが照合、CMoAが実行時事実を出力 |
| measurement → decision | 品質・費用・不確実性・停止理由、採否、適用surface、baseline | uzushio |
| decision → binding | 元記録、置換関係、有効期間、現在の対象 | DocDag |

[SLSAのprovenance構造](https://slsa.dev/spec/v1.2/build-provenance)を参考に、入力・出力・実行条件を分離する。artifactの実在・digest照合はまずuzushioで行う。DocDagには既存のkind/edge/target/periodで表せる関係を宣言し、汎用エンジンへの追加は表せない最小例と別用途の需要が揃ってから行う。

期限切れ、config不一致、改変されたartifact、欠落したtrace、不正なsupersessionを合成fixtureで検出する。これらを正常な採用証拠として通さない。過去の採用を消すことなく、既知のbaselineを復元する記録と手順を用意する。

Hのdigest、用途、最初の開封、どの候補の判定に使ったかを記録する。一度開封したHを次の探索で「未使用」と扱わない。codingの継続的promotionにもheld-outを使うため、最終的な一般化の主張には別の監査setか、事前に定めた適応的評価手順が必要になる。[Reusable holdout研究](https://research.ibm.com/publications/the-reusable-holdout-preserving-validity-in-adaptive-data-analysis)はその区別の根拠であり、同論文の方式を実装済みとは扱わない。

### P3 — 文脈、pool、探索予算を実測で最適化する

P1の実証とP2の証拠接続を通した後、次の仮説を一つずつ比較する。各仮説の実験準備は先行できる。

| 仮説 | 変更の所有層 | baselineと評価 | 採用しない条件 |
| --- | --- | --- | --- |
| 必要なbinding文書だけを渡すと短く正確になる | DocDagの既存context + uzushio render | 現在の全注入と比較。遵守率、正解率、必須条項の欠落、実token数 | 重要な規則を落とす、本文予算を保証できない、品質差が不明 |
| poolの相補性が追加計算に見合う | uzushioで測定、CMoAの静的configとして提案 | 最良単体、現pool、候補pool。同じ総費用枠で比較 | 全候補失敗率や最終品質が改善しない、費用増に見合わない |
| 必要なケースだけ計算を増やせる | uzushioで評価、CMoAで明示的な方策 | 固定pool/固定judge protocolに対し品質・費用のPareto比較 | 失敗をtieへ隠す、省略した判定が結果を変えうる、Hで非劣性を確認できない |
| traceに基づく小規模探索で改善提案の効率が上がる | uzushio | 手動一変更、現mine/propose、限定的な反省・候補選別を比較 | 検証予算を消費して独立確認ができない、同じHへ過適応する |

poolでは`β = P(全候補が失敗)`、少なくとも一つ正解がある割合、正解を含む集合での選択成功率を測る。誤り相関も診断に使うが、βの代わりにはしない。モデルfamilyの違いは独立性の証明にならない。[judgeの相関研究](https://arxiv.org/html/2605.29800v1)の結果も、このfleetの測定値として流用しない。

文脈予算はDocDagの概算値と実token数を区別する。まず既存contextを使うuzushio側の選別実験とし、必要なら必須項目が予算を超えたことを明示する契約を提案する。DocDagへvector DBやLLMによるbinding解釈を加えない。

探索は[GEPA](https://arxiv.org/abs/2507.19457)と[Self-Harness](https://arxiv.org/html/2606.09498v3)を比較対象にする。最初は候補数・評価費用・変更surfaceを制限し、既存runと採用規則を使う。自動採用権限の拡大は別ADRと評価を要する。

## 着手できる作業単位

各行は一つのレビュー可能な成果物にまとめる。IDはこの計画用であり、既存の仕様IDや新CLI名ではない。

| ID | 優先 | repository | 成果物 | 依存・検証 |
| --- | --- | --- | --- | --- |
| D-01 | P0 完了 | DocDag | 自己ADRのmetadata移行、corpus設定、CI | 本文保持、binding 6件、validate/lint成功 |
| U-01 | P0 完了 | uzushio | 既存trial再開の確定とfresh A/A記録 | 変更拒否、予算延長での完走、journal保持を確認 |
| C-01 | P0 完了 | CMoA | 既存serve計測・起動経路の確定 | Go/monitor/Compose checks成功、request ID中継修正 |
| U-02 | P1 次着手 | uzushio | 代表D・回帰R・未使用Hのmanifestとラベル手順 | P0の既存prefixを基準に、重複・由来・strata・開封履歴を検証 |
| C-02 | P1 | CMoA | 合意・tie-breakの失敗再現と一変更 | 合成fixture、replay、規則変更時はADR 0013をsupersede |
| U-03 | P1 | uzushio | chat finalistの固定標本・paired採用評価 | 点推定と区間を分離、Hの事前規則、D/Rとの非混在 |
| U-04 | P1 | uzushio | memory一変更のdoctor/improve/run/render実証 | 両splitの既存gate、却下/保留も記録、CMoAとのhash一致 |
| U-05 | P2 | uzushio | 評価bundleと検証、H利用履歴、baseline復元例 | 改変・欠落・期限切れ・条件不一致のfixture |
| D-02 | P2 | DocDag + uzushio | 証拠関係を表す設定とlint fixtures | まずuzushioの設定生成で実装。エンジン変更は必要性が判明した場合 |
| U-06 | P3 | uzushio + CMoA | 単体/pool/context/限定探索の比較記録 | 同じ費用枠、別の監査set、採用を伴う変更は各所有層へ |

## 採用時に必ず残すもの

| 観点 | 記録・判定する内容 |
| --- | --- |
| 品質 | codingは既存split判定、chatは代表setのpaired品質差と不確実性。選択率、経路別品質、重大回帰は別表示 |
| 失敗 | candidate fail、runner障害、judge timeout/invalid output、測定されたdraw、未完了を区別。除外数・理由・両条件の内訳も記録 |
| 費用 | freshのwall time、p50/p95、GPU占有時間または取得可能な資源指標、tokens、calls。replay時間は推論高速化の指標にしない |
| データ | 独立task数と反復数、ラベルの由来、strata、split、Hの利用履歴。公開データのライセンスも保持 |
| 来歴 | baseline/candidate、版・digest、render、live/replay/reuse、DocDagのrevision/as-of、採否と理由 |
| 結果 | 採用、却下、保留を区別。予算切れや検出力不足なら保留を許す。結論に合わせて閾値を変えない |

許容品質差、速度目標、標本数、資源上限は各試験の事前登録で決める。本ロードマップに未測定の改善率を約束しない。最初のA/Aから必要費用と検出可能な差を見積もり、その範囲で実験を選ぶ。

## 維持する境界と後回しにすること

- CMoAは候補の合成・回答の書き直し・自己改善を行わない。codingの全候補検証と設定順の選択を維持する。
- chatは単一blind judge、両順提示、機械障害とdrawの区別、記録可能なtie-breakを維持する。JSON不正時の既存retry以外の再質問は追加しない。変更する場合は先に評価とADRを用意する。
- judgeパネル、動的routing、tool loop、skill本文実行、subagent実行はP1の完了条件ではない。既存の失敗分析から必要性が示されたものだけを次の設計対象にする。
- DocDagは有限の宣言的な関係検証を維持する。汎用workflow scheduler、統計エンジン、学習器を組み込まない。
- 外部observability連携は必要になった時点でversion固定の変換器を検討する。[OpenTelemetryのGenAI仕様](https://opentelemetry.io/docs/specs/semconv/)の変化を永続trace契約へ直接持ち込まない。
- serveはchatを提供する常駐HTTPプロセス。認証・TLS・schedulerは現行scopeに含めない。monitor/Composeの起動経路は[ADR 0017](adr/0017-monitor-host-network-all-interfaces.md)に従い、公開サービス化は別要件として扱う。

## 検証と計画の更新

実装時は各repositoryのAGENTS.md/CIを使う。CMoAのGo変更は対象テスト後に`make build test vet`と`make lint`、monitorはcheck/unit/buildと必要なE2E、Docker/Composeはshell構文とbuildを確認する。ADR・DocDag設定は対応版の文書gate、uzushioの仕様変更はconformanceと生成fixtureも確認する。

共同契約の変更には合成traceとrenderを使うproducer/consumer適合性確認を追加する。実モデル評価の成功を通常CIの前提にはしない。実fleetの確認では固定条件と保存artifactを成果物にする。

段階終了時に実装状態・採否と共有可能な要約を更新する。改善が見えなければ、失敗箇所に応じて次の項目を並べ替える。実機構成、個別の測定結果、実行ID、artifactのdigest、作業経緯は内部記録へ保存する。公開する評価記録は対象と条件を別途確認する。共有手順は公開文書と合成fixtureで再実施できる形にし、内部の保存先へ依存させない。

## これまでの実装実績

以下は従来ロードマップの実装履歴。shippedは当時の機能実装を示し、現在の運用品質・校正・改善成功を意味しない。

| step | layer | what lands | status |
| --- | --- | --- | --- |
| 1 | DocDag | public `config` package and YAML round-trip tests, so uzushio can build its vault configuration in Go | shipped ([DocDag v0.4.0](https://github.com/Kaikei-e/DocDag/releases/tag/v0.4.0), 2026-09-04) |
| 2 | uzushio | vault configuration written in Go; `docdag lint` and `lint --fixtures` pass | shipped ([uzushio#1](https://github.com/Kaikei-e/uzushio/pull/1), 2026-09-05) |
| 3 | uzushio | `task doctor`: kill rate against injected defects, false-positive rate against a reference solution; `task mutate` for Go mutants; `task calibrate` for banded verifiers. CMoA contributes `cmoa verify` with the `exit-code` and `band` kinds and `task.json` version 2 ([ADR 0009](adr/0009-add-verify-command-and-task-v2.md)) | shipped ([uzushio#2](https://github.com/Kaikei-e/uzushio/pull/2), [#4](https://github.com/Kaikei-e/uzushio/pull/4), [#5](https://github.com/Kaikei-e/uzushio/pull/5), 2026-09-05) |
| 4 | **CMoA** | **v0, coding face: `propose` and `select`, verifier-selected, no judge** | shipped (this repository) |
| 5 | uzushio | `run` and `improve`: held-in and held-out splits, sequential testing, edits accepted only when both pass. CMoA contributes `propose --harness`, `--seed` and `--temperature` ([ADR 0010](adr/0010-harness-directory.md)) | shipped ([uzushio#6](https://github.com/Kaikei-e/uzushio/pull/6), 2026-09-05) |
| 6 | uzushio | the first task manifest carries the constraints learned from the previous project: one milestone per session, a ceiling on test lines per product line, dogfooding kept off the critical path | shipped ([uzushio#6](https://github.com/Kaikei-e/uzushio/pull/6), clauses UZ-C-006 to UZ-C-008, 2026-09-05) |
| 7 | CMoA | chat face: a single blind judge of a different model family, position-swapped pairwise presentation with a seeded nonce, `cmoa judge` and `cmoa serve` ([ADR 0011](adr/0011-chat-face-blind-pairwise-judge-and-serve.md)), then consensus among the candidates before the judge and a Copeland score with a recorded tie-break after it ([ADR 0013](adr/0013-consensus-then-copeland-for-chat-selection.md)); uzushio's calibration log (three kappas with named tie handling, expiring `calibration` documents) | shipped ([CMoA#7](https://github.com/Kaikei-e/CMoA/pull/7), [uzushio#7](https://github.com/Kaikei-e/uzushio/pull/7), 2026-09-06; calibration re-aggregated from recorded answers against the new selector and remains uncalibrated, 2026-09-06) |


## Tooling

Alongside the numbered steps, and outside their order because it has no
verifier of its own:

- **CMoA Monitor** (`monitor/`, 2026-09-06): a single-screen web view of
  one round — proposer lanes, the judge's pair-by-order grid, the
  selection and a phase timeline — with the fleet's health, the run
  history and a file inspector beside it, and a chat panel that relays a
  conversation to `cmoa serve` and shows the round it produced. It reads
  run traces and each server's `/slots`; the round and its trace are
  `serve`'s
  ([ADR 0012](adr/0012-monitor-observes-traces-and-servers.md)).
- **Containers** (`compose.yaml`, 2026-09-08): `cmoa serve` and the monitor on the
  host network, monitor at `0.0.0.0:3999`, so an existing loopback `cmoa.json`
  starts both without rewriting URLs
  ([ADR 0017](adr/0017-monitor-host-network-all-interfaces.md)).
