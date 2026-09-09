# 3層の基準を固定する手順

[共同ロードマップ](roadmap.md)のP0で使う手順。評価器はuzushio、実行器はCMoA、文書のbindingと整合性検証はDocDagを使う。以下は実験記録の手順であり、runtimeやtrialのJSON契約を追加するものではない。

## 固定する情報

実行前に次の情報を一つの基準記録へ書く。JSONでもMarkdownでもよいが、digestの対象と取得時点を明記する。実験後にも同じ対象を確認する。

この手順書は共有用だが、取得する実機構成・入力識別子・実測値・digest・生ログは内部記録として保管する。値をこの文書や共有例へ転記しない。外部向けの再現例には合成入力とplaceholderを使い、実験を公開する場合は公開対象を別途確認する。

| 対象 | 記録 |
| --- | --- |
| 3つのrepository | commit、dirty状態、実行に使った変更のpatchまたはsource archiveのSHA-256 |
| 実行バイナリ | CMoA、uzushio、DocDagのversion出力とファイルのSHA-256。専用出力先へビルドし、実験中に置換しない |
| 文書 | DocDag版、preset版、vault revision、`as_of`、binding出力。dirtyなvaultは文書も保存してdigestを記録 |
| 評価入力 | suite、D/R manifest、採用したprefix、各task、会話、rubric、reference、候補、gold、ラベルのdigest |
| 条件 | configファイルのdigest、prompt/選択規則版、presentation seed、judge seed、temperature、token上限、並列数 |
| モデル | model名、重みファイルSHA-256と量子化。multimodal projector等の追加入力があれば同様に記録 |
| 実行環境 | 推論serverの版と実行image ID、起動引数、CPU/RAM/GPU、資源制限、稼働開始時刻、開始前後のhealth/slots |
| 結果 | 実行種別fresh/replay/reuse、停止理由、完了step数、未測定数、選択、call/retry数、wall time、結果journalのdigest |

versionが`dev`でもバイナリdigestとsource snapshotで識別できる。取得できない項目は`unknown`にして理由を書く。同じseed・model名・image tagだけで同じ実験条件とは判断しない。秘密のkey/header、会話本文、個人の絶対パスは共有用の要約に載せない。

## 対応版の検証

| consumer | 基準のDocDag | 検証 |
| --- | --- | --- |
| CMoA | v0.4.1 | `make docdag`、`docdag query --binding` |
| uzushio | v0.4.0 | `make docdag`、`make conform`、`uzushio judge status` |
| DocDag自身 | 自己corpusを整備したsourceからのbuild | `docdag validate`、`docdag lint`、bindingが意図したADR集合と一致 |

各コマンドは対象repositoryのルートで実行する。PATHの先頭に該当版のbinary directoryを置き、開始前に`docdag --version`とdigestを確認する。uzushioの生成configを変更しない確認では、indexへ作用する`make check`や不要な再生成は行わない。

`DOCDAG_AS_OF`に事前登録した実験の論理日付を明示し、文書gateとtrialで揃える。省略時はvalidateとqueryで日付の決め方が異なるため、同じ日に実行しただけでは同じbinding条件にならない。

DocDagのbindingは有効な記録を示す。uncalibratedという測定結果も有効な記録になりうるため、校正合格はuzushioのverdictで確認する。

## fresh A/A

既存のuzushio `examples/suite-chat/cards/control.json`を作業用ディレクトリへコピーして使う。card内の相対パスはcardから解決されるため、suite/manifests/config/binaryはコピー先を基準に設定し直す。入力の内容は変更しない。

初期の動作基準はD/Rから項目数とprefixを事前に固定し、各項目をAとAの2条件で実行する。両条件で同じconfigと同じCMoAバイナリ、同じseedを指定し、`reuse.kind`は`none`にする。反復確認だけで統計的独立性、代表品質、非劣性を証明したとは扱わない。Hは読まない。

予算は過去の項目所要時間から事前に設定する。評価中に変更する場合は時間予算の延長だけとし、入力・条件・順序・seedを変えない。計画された途中停止を使って再開を確認する場合は、初回cardの`planning.accept_cut: true`と停止予算も事前に記録する。実行中のjudge callは予算を超えて完了することがある。

```sh
# 変数は利用者が用意した固定バイナリ、実験専用のパス、論理日付。
export DOCDAG_AS_OF="${P0_AS_OF:?Set the preregistered date as YYYY-MM-DD}"
"$P0_UZUSHIO" judge trial --card "$P0_CARD" --cmoa "$P0_CMOA" \
  --vault "$P0_VAULT" --out "$P0_OUT" --dry-run
"$P0_UZUSHIO" judge trial --card "$P0_CARD" --cmoa "$P0_CMOA" \
  --vault "$P0_VAULT" --out "$P0_OUT"
# 途中終了した場合のみ、必要なら予算を延長して同じplanを再開する。
"$P0_UZUSHIO" judge trial --card "$P0_CARD" --cmoa "$P0_CMOA" \
  --vault "$P0_VAULT" --out "$P0_OUT" --resume
```

事前登録、途中結果、最終結果をそれぞれ保存する。CLI終了コードに加えて`trial.json`のstop reasonとjournalを読む。完了済みjournalの各行が再開前後で同一で、新規行だけ増えることを照合する。完了後にもう一度`--resume`してjournal・trace数が増えないことも確認する。

trialの`--dry-run`はplan確認であり、CMoAのconfig解析成功まで保証しない。実行前にCMoAが受け付ける設定項目を確認する。準備失敗があれば元のartifactとcall数を保存し、新しい実験IDでやり直す。

2条件の対応はitem IDで取る。選択一致率、選択経路、未測定、retry、各条件のwall timeを報告する。両者の設定が同一でもモデル出力は変動しうる。一致率が100%でなくても実装不良と即断せず、保存されたcallと選択規則を調べる。逆に一致しても正答とは認定しない。

resumeの入力・gold・config・binary変更拒否はfakeを使う回帰テストで確認する。実fleetで拒否を試す場合も、変更対象は実験専用コピーに限定し、既存の校正入力やモデルを変更しない。

現行fingerprintは入力条件を照合するもので、手動改変されたjournal全体の署名検証ではない。journalの保存・digest照合も併用する。

## serveの計測と実験後の照合

CMoAの対象テストで成功、選択失敗、queue/実行中キャンセルとterminal性能ログを検証する。monitorがserveのrequest IDを返すことも確認する。fresh serveの追加確認を行うときは実験専用port/processを使い、元のserveやfleetを再起動しない。

クライアントの受信完了までの時間と、handlerの`total_ms`は測定範囲が異なる。queue、propose、select等の内訳はサーバログから取り、request IDとrun IDで対応付ける。完了した回答と、途中キャンセルされた処理を混ぜて平均しない。

性能測定中はDocker build、重いテスト、別の推論実験を並走させない。実験前後でmodel digest、container ID/image/起動時刻、config、バイナリ、評価入力を照合し、変化があればその測定を同条件の基準として採用しない。ログに残った実験専用processだけを終了し、最後にfleetがidleへ戻ることを確認する。

## P0の完了条件

- DocDag自身のADRが機械可読で、本文を保持したまま期待するbindingを返す。
- 指定版の両consumer文書gateと必要な実装checksが通る。
- 固定したfresh A/Aの全planが完了し、途中再開と完了後resumeが同じjournalを保持する。
- 再現条件と結果が記録され、測定前後の環境差を確認できる。
- 不明な条件、少数項目での推論の限界、未実行checksを結果に明記する。

P0完了は品質改善の採用を意味しない。chatの独立評価とcodingの改善実証はロードマップP1で行う。
