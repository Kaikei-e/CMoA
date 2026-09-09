---
title: "数値合意を回答全体の十進数に限定する評価候補"
status: proposed
date: 2026-09-09
supersedes: ["0013"]
depends-on: ["0003", "0006", "0007", "0008", "0010"]
---

# 0018: 数値合意を回答全体の十進数に限定する評価候補

## 状態

Proposed。P1-chat の比較対象として実装した開発候補であり、H での採用評価と
serve 検証は未完了。0013 が現在の binding である。DocDag の ADR preset は proposed な supersedes も
status_drift の対象にするため、採用前の本記録は ADR corpus 外の proposals に置く。本候補の採用時に本記録を
accepted とし、0013 の履歴を保持して supersession を確定する。

## 調査と問題

0013 の短文の末尾数値による合意は、異なる単位、分数の分母、未対応の否定を
同じ答えとして扱いうる。合成回帰テストで数値の誤一致と、それによる同点解消の
centrality の誤加点を再現した。これは実利用分布での改善率の測定ではない。

[MT-Bench](https://arxiv.org/abs/2306.05685) と
[位置バイアス研究](https://aclanthology.org/2025.ijcnlp-long.18/) は judge の
判定と提示順の診断を支持するが、この fleet における合意の正しさや速度改善を
保証しない。採否は uzushio の D/R 診断、fresh 比較、未使用 H の固定標本評価で決める。

## 候補の決定

0013 の D1 にある数値の合意条件だけを改める。正規化後の回答全体が、160文字以内の
符号付き十進数である場合だけ、桁区切りと値を持たないゼロを除いて数値比較する。
桁区切りは ASCII comma の3桁組だけとする。単位、散文、分数、指数、列挙から
部分的に数値を抽出しない。正規化後の文字列完全一致は引き続き合意とする。

この同じ判定を consensus と同点解消の centrality に使う。版名は `nfkc-v2`。
`judge.json` の任意フィールド `normalisation` に全経路で記録し、consensus が
ある場合は既存の `consensus.normalisation` にも記録する。旧 trace の欠落を許容する。
schema version、outcome、単一 blind judge、両順提示、Copeland と他の同点解消規則、
0013 が引き継いだ他の決定は維持する。

## 評価と限界

数値表記の合意を見落とすと judge call が増えるため、品質と実応答時間を両方測る。
旧 consensus で省略された call を replay は復元できない。必要な call がない場合は
`judge_failed` として未測定を記録し、fresh 評価で補う。replay 時間を推論時間としない。

文字列の正規化そのものは既存のヒューリスティックであり、装飾と演算子の区別などに
限界が残る。完全一致は正解の証明ではない。候補全件が誤答のケースも別に測る。
許容劣化幅、品質下限、cluster数、重大回帰上限を H 開封前に固定し、精度不足は保留する。
採用評価・人間ラベル・データ・実行結果は非公開領域で保持し、Gitへ追加しない。
