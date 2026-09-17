# Spec Delta

## ADDED Requirements

### Requirement: Failures are reported as tool errors
どのMCPツールも、通常の失敗（プロジェクトの外で呼ばれた、スロットがまだ無い、サービス名が無い、台帳の版が新しすぎる等）を、ツールのエラー結果（`isError: true`と、立て直し方を述べた文面）として返さなければならない（MUST）。失敗をプロトコルエラー（`tools/call`自体の失敗）にしてはならない（MUST NOT）。エージェントは、ツールのエラーなら文面を読んで1往復で立て直せるが、プロトコルエラーでは立て直せないためである。

由来: `render_env`は、失敗時に返す構造化出力の`env`がnullになり、SDKの出力スキーマ検証に落ちて、すべての失敗がプロトコルエラーになっていた（2026-09-18に発見して修正）。

#### Scenario: プロジェクトの外での呼び出し
- **WHEN** `port-keeper.toml`が無いディレクトリを`cwd`に指定して、作業ディレクトリを必要とする各ツール（`current_context`、`resolve_url`、`resolve_port`、`render_env`、`status`、`slot_new`、`slot_release`）を呼ぶ
- **THEN** どの呼び出しも`tools/call`としては成功し、結果は`isError: true`で、文面はプロジェクトの外であることと次に取れる行動を述べる
