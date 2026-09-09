# 3プロジェクトの進化方針を支える調査 — 2026-09-09

[共同ロードマップ](roadmap.md)の根拠。3リポジトリのソース・ADR・テスト・CIと、公開研究・公式技術文書を照合した。コードから確認できる動作、推論、今後の提案を区別する。実機の測定値・構成・作業記録は含めない。後続検証は[基準固定の手順](baseline-protocol.md)に従う。

## 参照範囲

CMoAは本文の相対リンク、uzushioとDocDagはcommitを固定したソースリンクを根拠とする。開発環境のcheckout状態や未公開の実行artifactを、共有する設計根拠には用いない。

## ソース解析

| 対象 | 根拠 | 確認した動作と方針への含意 |
| --- | --- | --- |
| CMoAの実行境界 | [surface/autonomy](../cmoa.go)、[selection](../internal/selection/selection.go) | codingは全候補を検証し設定順で選ぶ。改善ループをruntimeへ移さない |
| chatの合意 | [consensus](../internal/judge/consensus.go)、[numeric](../internal/judge/numeric.go)、[normalization](../internal/judge/normalization.go) | 正規化文字列、または短文の末尾数値と否定の一致を使う。質問・単位・対象の意味は比較しない。誤合意率を測る必要がある |
| chatの集計 | [aggregate](../internal/judge/aggregate.go)、[tie-break](../internal/judge/tiebreak.go)、対応テスト | 合意、Condorcet、Copeland、記録付きtie-break。未測定とdrawを区別する。経路別品質と選択率を分けて測る |
| replay | [実装](../internal/judge/replay.go)、[CLI](../cmd/cmoa/replay.go) | 保存応答の再parse・再集計を行う。新promptや推論設定の品質・実推論速度はfreshで測る |
| harness | [harnessdir](../internal/harnessdir/harnessdir.go)、[trace schema](trace-schema.md) | 読んだtreeをhash化。skill本文は注入せず名前・descriptionを提示する。最初の改善実証にはmemoryが適する |
| serve計測 | [serve](../internal/serve/handler.go)、[performance](../internal/serve/performance.go) | queue/propose/select等の時間、request/run ID、キャンセルphaseを記録する。handler完了とクライアント受信完了を区別する |
| uzushio doctor | [doctor](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/doctor/doctor.go)、[environment](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/doctor/environment.go) | reference、mutant、runner障害を区別し、参照再利用を環境fingerprintで制約する |
| uzushio render | [render](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/render/render.go) | DocDagのbindingから日付・revision・renderer・tree hash付きで実体化。文脈選別の所有層はuzushio |
| codingの採用 | [loop](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/loop/loop.go)、[stats](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/stats/stats.go) | paired比較とe-process/区間を実装。両splitでhold/improve、少なくとも一方でimproveを要求。多数提案の適応的探索は別管理が必要 |
| chat trial | [trialreport](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/judge/trialreport.go)、[quality tests](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/judge/trialquality_test.go) | A/BはDが代表品質、Rは診断、CはHが代表品質。`finalist`は点推定による推奨であり`adopt`ではない |
| 校正の有効性 | [status](https://github.com/Kaikei-e/uzushio/blob/14decd568bd75d95f52e126e68196167af057289/internal/judge/status.go) | 期限内のuncalibrated記録もbindingになり警告する。bindingを校正合格へ読み替えない |
| DocDagの契約 | [public config](https://github.com/Kaikei-e/DocDag/blob/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/docs/adr/0006-public-config-yaml-roundtrip-and-append-only.md)、[projection](https://github.com/Kaikei-e/DocDag/blob/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/internal/graph/projection.go) | config/kind/edge/periodの既存機能を確認。これを利用し統計計算はuzushioに置く、というロードマップ上の判断をした |
| DocDagの履歴 | [immutable](https://github.com/Kaikei-e/DocDag/blob/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/internal/graph/immutable.go) | closed文書を履歴比較するが、status変更・inverse追加・本文末尾追記等は許す。完全なbyte不変やartifact検証とは異なる |
| DocDagのcontext | [brief](https://github.com/Kaikei-e/DocDag/blob/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/internal/brief/brief.go) | 関係と抜粋、as-of/revisionを返す。token予算はbyte数からの概算で、必須一覧だけで上限を超えうる。日本語で実token数を測る |
| DocDag自己corpus | [6件のADR](https://github.com/Kaikei-e/DocDag/tree/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/docs/adr)、[CI](https://github.com/Kaikei-e/DocDag/blob/5de3ec33f9ac74d6cd2bd5dddd335f2f6e8a6990/.github/workflows/ci.yml) | 参照commitのADRは本文のAccepted表記のみでfrontmatterがない。文書の決定を機械可読にする自己corpus整備をP0へ置いた |

数値合意のリスクはコード条件からの推論であり、実利用での発生率ではない。例えば「在庫は3個」と「必要数は3個」が同じ回答かは質問に依存する。合成fixtureで再現し、代表項目のfresh評価で影響を測る。

保存応答を再集計して選択カバレッジが変わっても、新しいjudgeの校正成功を意味しない。人間同士の一致、abstainの扱い、decided-onlyの母数を併記し、係数差を単純な性能差にしない。

## Web調査と適用限界

2026-09-09までの一次資料を優先した。各研究の効果量をこのstackの期待改善率に転用しない。

| 資料 | 読み取った根拠と採用方針 |
| --- | --- |
| [MT-Bench / Chatbot Arena](https://arxiv.org/abs/2306.05685)（NeurIPS 2023） | position・verbosity・self-enhancement等の限界。強いjudgeの人間一致は任意の小型judgeへの保証ではない |
| [Judging the Judges: Position Bias](https://aclanthology.org/2025.ijcnlp-long.18/)（2025） | judge・task・候補品質差によってバイアスが変わる。swapとstrata別集計を維持 |
| [Nine Judges, Two Effective Votes](https://arxiv.org/html/2605.29800v1)（2026-05 preprint） | 誤り相関はパネルの情報量を制約。主にNLI/pairwise preferenceの結果で、open-ended/codeへ一律に一般化しない |
| [GEPA](https://arxiv.org/abs/2507.19457)、[公式実装](https://github.com/gepa-ai/gepa) | 軌跡の自然言語分析と候補更新を限定探索の比較対象にする。全面移植を先行させない |
| [Self-Harness v3](https://arxiv.org/html/2606.09498v3)（2026-08-20） | weakness mining・最小変更・held-in/outによるpromotion。proposerへ非公開でもgateで反復利用するsetと、最終監査setを区別 |
| [The reusable holdout](https://research.ibm.com/publications/the-reusable-holdout-preserving-validity-in-adaptive-data-analysis)（Science 2015） | 適応的な分析選択は通常の検定の前提を壊しうる。Hの開封・利用履歴と再利用方針を記録 |
| [Time-uniform confidence sequences](https://arxiv.org/abs/1810.08240)（2021） | 前提を満たす逐次観測への区間。任意の依存や多数仮説の保証ではない。既存e-processの対象を明記 |
| [Demystifying evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)（2026-01-09、実装者の報告） | task/trial/grader/trace/最終状態を区別。代表品質suiteと回帰suiteを分ける |
| [Infrastructure noise](https://www.anthropic.com/engineering/infrastructure-noise)（2026-02-05、実験報告） | 資源条件と制約の与え方が結果に影響。A/A、資源、warm/cold、queueを記録 |
| [SLSA v1.2 Build Provenance](https://slsa.dev/spec/v1.2/build-provenance) | 入力・出力・実行条件をdigestで関連付ける構造を参考にする。独自評価記録をSLSA準拠とは称さない |
| [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/specs/semconv/)、[GenAI属性](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) | GenAI仕様の移動や属性の非推奨化がある。外部連携はversion固定の変換器とし、永続traceへ直結しない |

## 再策定した方向

最初に、変更を評価して採否と再現条件を追える一巡を完成させる。既存実装を利用し、chatの選択率・回答品質・judge妥当性・応答時間を分離する。codingは実際に注入できるmemory一変更で改善ループを実証する。

DocDagは自身のADRと利用者側の証拠関係から整備する。artifactの実在とdigest検証はまずuzushioで行い、汎用部分だけをDocDagの拡張候補にする。graphの整合性は測定値の正しさを証明しない。

poolは単体品質、全候補失敗率、正解を含む候補集合の割合、selectorがその正解を選ぶ率を測ってから拡張する。familyの違いやpairwise相関だけで全候補の同時失敗率を推定しない。日本語の利用分布、ラベル品質、モデル重み・量子化・serverの識別、適応的な多数提案の誤採用管理は追加検証が必要である。
