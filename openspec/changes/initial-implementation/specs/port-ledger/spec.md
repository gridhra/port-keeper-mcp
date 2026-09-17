## Purpose

ローカル端末で貸し出した開発用ポートを永続化し、二重割当を防ぎ、台帳と実際のLISTEN状態のズレを報告する。

## ADDED Requirements

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
