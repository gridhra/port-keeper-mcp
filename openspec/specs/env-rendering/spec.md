# env-rendering Specification

## Purpose
割当結果を、利用者側の道具（mise、direnv、docker compose、Claude Code）が読む形に描画する。

## Requirements

### Requirement: Formats
`env`は`dotenv`、`export`、`json`、`mise`、`direnv`、`claude-env`を提供しなければならない（MUST）。変数はマニフェストの宣言順にサービス、続いて導出値、最後に`PORT_KEEPER_PROJECT`と`PORT_KEEPER_SLOT`。

#### Scenario: Golden output
- **WHEN** 同じマニフェストと同じ割当で描画する
- **THEN** 出力はバイト単位で同一である

### Requirement: Template variables
導出式は`${port.<service>}` `${url.<service>}` `${slot}` `${slot.infra}` `${project}` `${block.base}`を展開しなければならない（MUST）。未知の変数、未知のサービス、tcpサービスへの`${url}`はエラーにする。

#### Scenario: URL of a tcp service
- **WHEN** `${url.db}`で`db`が`proto = "tcp"`
- **THEN** 描画は失敗し、`${port.db}`を使うよう案内する

### Requirement: Dotenv block is idempotent and never committed
`dotenv`形式はマーカー行で囲んだブロックを描画先ファイルに冪等に書き換えなければならない（MUST）。描画先がgitに追跡されている、シンボリックリンクである、マニフェストのディレクトリの外にある場合は書き込みを拒否しなければならない（MUST refuse）。

#### Scenario: Tracked target
- **WHEN** `.env.local`がgitに追跡されている
- **THEN** `env`は書き込まずにエラーを返し、`.gitignore`への追加を案内する

### Requirement: Hook-friendly no-op
`env --if-present`はマニフェストの無いディレクトリで何も出力せず終了コード0で終わらなければならない（MUST）。

#### Scenario: Session hook outside a project
- **WHEN** マニフェストの無いディレクトリで`env --if-present`を実行する
- **THEN** 何も出力せず終了コード0で終わる
