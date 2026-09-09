# CMoA: agent instructions

このファイルはリポジトリ全体の作業指針。詳細はリンク先を必要な範囲だけ読む。
配下により具体的な `AGENTS.md` がある場合は、その対象範囲の指示も確認する。

## 構成と参照先

- [README.md](README.md): セットアップ、CLI、設定とタスク形式。serve と monitor の Compose 起動も含む。
- [docs/roadmap.md](docs/roadmap.md): スコープと進捗。
- [docs/adr/](docs/adr/README.md): 設計判断。現行の判断はルートで `docdag query --binding` を実行して確認する。
- [docs/trace-schema.md](docs/trace-schema.md): uzushioやmonitorも読む永続化形式。
- `cmd/cmoa/`: CLI。`internal/`: 設定、タスク、生成、選択、judge、検証、HTTP、トレース。
- `cmoa.go`: 外部向けのsurface/autonomy語彙。`monitor/`: 独立したSvelteKitアプリ。
- `compose.yaml`、`deploy/`、`monitor/Dockerfile`: `cmoa serve` と monitor のコンテナイメージ。モデルサーバは含めない。

## 実装とレビューの要点

- CMoAは候補を生成・検証して選択するruntime。候補の合成や回答の書き直し、評価・自己改善ループは追加しない。uzushioへの依存を作らない。
- Go本体は標準ライブラリのみ。既存のパッケージ境界とエラー型を使い、変更したGoファイルに `gofmt` を適用する。
- codingでは全候補を検証し、設定順で最初の合格候補を選ぶ。chatの選択を変える場合は、現行ADRと `internal/judge/` のテストを確認する。
- 候補の不合格、実行基盤の障害、judgeのタイムアウト・不正出力・測定された引き分けを区別する。CLIの終了コードだけで「候補が選ばれた」と判断しない。
- トレースと選択結果の上書き防止・来歴を維持する。JSON、CLI、task/config形式を変える場合は、互換性、対応テスト、READMEとtrace schemaへの影響を確認する。
- monitorはトレースを観測し、chatを `cmoa serve` に中継する。トレースの更新や独自の候補選択を実装しない。
- acceptedなADRの決定内容を変えるときは、`supersedes` を持つ新しい記録を追加する。

## ビルドと検証

特記のないコマンドはリポジトリルートから実行する。Goは `go.mod`、lintは
`.golangci.yml`、CIの実行条件は `.github/workflows/` を参照する。

| 変更対象 | 検証 |
| --- | --- |
| Go実装 | 対象パッケージのテストで確認後、`make build test vet` と `make lint` |
| ADR・DocDag設定 | `make docdag`（このリポジトリのCIはDocDag v0.4.1） |
| monitor | `monitor/` で `pnpm check`、`pnpm test:unit`、`pnpm build`。UI動作を変えた場合は `pnpm test:e2e` も実行 |
| Docker・Compose | `sh -n deploy/up.sh` と `docker compose build`。起動経路やネットワークの判断を変えた場合は ADR 0017 も確認 |
| 文書のみ | 参照先・記載コマンド・差分を確認。ADR・設定を変えなければGoやブラウザのテストは不要 |

- `make build` は `bin/cmoa` を作る。起動・実行例はREADMEを参照する。
- `make lint` にはgolangci-lintが必要。単体テストはfixture/fakeを使い、実モデル・Docker・DocDagなしで動く。
- monitorの準備は `monitor/` で `pnpm install --frozen-lockfile`。pnpmは `package.json` の指定、NodeはCIの指定（現在24）に合わせる。詳細は [monitor/README.md](monitor/README.md)。
- monitorのE2Eはfake fleetを使う。初回は `pnpm exec playwright install --with-deps chromium` が必要。
- runtimeの実E2Eは `CMOA_E2E=1 CMOA_CONFIG=/path/to/cmoa.json go test -count=1 -timeout=30m -run '^TestE2E' -v ./...`。実モデル・judge・DocDag・Dockerが必要なため、統合動作の確認が必要な作業で実行する。

## 作業の完了条件

- 着手時に `git status --short` を確認し、既存の変更を保持する。
- 変更した振る舞いを再現できるテストを追加・更新し、必要な検証が通ったら差分をレビューする。実行できなかった確認は理由を報告する。
- `git diff --check` を実行し、変更内容・検証結果・残る制約を簡潔に報告する。
- `local/`、`docs/internal/`、生成されたrunやビルド成果物はGit管理外。共有手順をそれらや個人の絶対パスに依存させない。fixtureには合成データを使う。
- このファイルは継続的に有効な指示に絞る。一時的な作業計画や進捗は入れず、コマンドや契約を変更したら対応箇所を更新する。

作成時の公式資料（2026-09-07確認）:
[AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md)、
[Codex best practices](https://learn.chatgpt.com/guides/best-practices)。
