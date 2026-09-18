# port-ledger Specification

## Purpose
ローカル端末で貸し出した開発用ポートを永続化し、二重割当を防ぎ、台帳と実際のLISTEN状態のズレを報告する。

## Requirements

### Requirement: Leases are unique per port
台帳（ledger）は、プロジェクト・スロット・固定の別を問わず、同じポート番号を同時に2つのリースへ与えてはならない（The ledger MUST NOT hold two live leases for the same port）。

#### Scenario: Two processes allocate at once
- **WHEN** 複数のCLIまたはMCPプロセスが同時にスロットを作成する
- **THEN** 得られたブロックは互いに重ならず、すべてプール内にある

#### Scenario: Pinned port collides with a pooled block
- **WHEN** プール内の番号が別プロジェクトで固定されている
- **THEN** 新しいブロックはその番号を含まない

### Requirement: Slot keeps its block until released
スロットは解放されるまで同じブロックを保持しなければならない（A slot SHALL keep its block across restarts until released）。

#### Scenario: Manifest reordered
- **WHEN** マニフェストのサービス順が変わる
- **THEN** 既存サービスのポートは変わらず、削除されたサービスのリースだけが消える

### Requirement: Status reflects reality
`status`は各サービスを`leased`（LISTENなし）、`active`（LISTENあり）、`stale`（設定日数以上LISTENなし）、`hijacked`（LISTENプロセスの作業ディレクトリが作業コピー外）のいずれかで報告しなければならない（MUST）。ツールはプロセスを停止してはならない（MUST NOT kill processes）。

#### Scenario: Nothing listening
- **WHEN** どのサービスもLISTENしていない
- **THEN** すべて`leased`と報告され、last_seenは更新されない

### Requirement: Ledger records its schema version
台帳は自分のスキーマの版を記録しなければならない（SHALL）。バイナリは、自分が知っている版より新しい版の台帳を開いたとき、読み取りも書き込みもせずに、バイナリの更新を案内するエラーで終了しなければならない（MUST）。自分が知っている版より古い台帳は、既存のリースを失わずに現在の版へ移行しなければならない（SHALL）。版の記録が無い台帳（本要件より前に作られたもの）は、最初の版として扱わなければならない（MUST）。

由来: 同じ端末に版の違うバイナリが同居しうる（インストールスクリプトで入れたものと`go install`で入れたもの、更新前から動き続けているMCPサーバーのプロセスと更新後のコマンド）。版の記録が無いと、古いバイナリが新しいスキーマの台帳を気づかずに読み書きする。

#### Scenario: 新しい台帳を古いバイナリが開く
- **WHEN** スキーマの版が2の台帳を、版1までしか知らないバイナリが開く
- **THEN** コマンドは0以外で終了し、エラーに台帳の版、バイナリが対応する版、更新の案内が含まれ、台帳の内容は1バイトも変わらない

#### Scenario: 版の記録が無い既存の台帳
- **WHEN** 本要件より前のバイナリが作った台帳を、本要件を満たすバイナリが開く
- **THEN** 既存のリースはすべて残り、台帳に最初の版が記録される

#### Scenario: 新規の台帳
- **WHEN** 台帳が無い状態で、状態を変えるコマンドを初めて実行する
- **THEN** 作られた台帳に現在のスキーマの版が記録される

#### Scenario: 動作中のMCPサーバーの下で台帳が新しくなる
- **WHEN** MCPサーバーの起動後に、より新しいバイナリが台帳を新しい版へ移行し、その後でMCPサーバーがツール呼び出しを受ける
- **THEN** どのツールもエラー結果（プロトコルエラーではなく、ツールのエラー）を返し、その文面にバイナリの更新とMCPサーバーの再起動の案内が含まれ、ポート番号は含まれない

#### Scenario: 新しい台帳に対するMCPサーバーの起動
- **WHEN** 自分より新しい版の台帳がある状態で`port-keeper mcp`を起動する
- **THEN** サーバーは起動せず、標準エラーに同じ案内を出して0以外で終了し、標準出力には何も書かない

### Requirement: Machine-readable status
`status --json`は、1行のJSONで`project`、`slot`、`slot_source`、`services`（各要素は`service`、`port`、`proto`、`tier`、`state`、`shared`、`pinned`、任意の`listener`、`listener_cwd`（`hijacked`のときだけ）、`last_seen`（RFC 3339））、任意の`warnings`を返さなければならない（MUST）。`services`は空でも配列（`null`ではない）。警告はstdoutの別行ではなく`warnings`に入れる。CLIの`status`は表でも番号を出しているので、この出力に番号があってよい。MCPの`status`ツールはこの変更に含まれず、番号を出さないままである。

#### Scenario: 2サービスのスロット
- **WHEN** `web`と`api`を持つプロジェクトで`env`の後に`status --json`を実行する
- **THEN** 1行のJSONで、`services`が2要素、それぞれ`state`が`leased`、`port`がプールの中
