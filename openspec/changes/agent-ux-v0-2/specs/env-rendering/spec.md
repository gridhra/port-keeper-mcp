# Spec Delta

## ADDED Requirements

### Requirement: Drift of the rendered dotenv file is reported by name
port-keeperは、描画先のdotenvファイルがマニフェストと台帳に合っているかを、読み取りだけで判定できなければならない（MUST）。判定は、スロットが無い／リースの無いサービスがある（`--infra-from`で共有するinfraサービスは除く）／ファイルが無い／マーカーブロックが無い／ブロックのヘッダが別の`project/slot`／ブロック本文が今描画した結果と違う、の順で行い、理由は相対パスとサービス名だけの文で、ポート番号を含んではならない（MUST NOT）。`context --json`は`ready`のとき`env_stale`と`env_stale_reason`を持ち、案内文の末尾に`port-keeper env`で描き直す指示を足す。Claude Codeフックはこの指示をSessionStartの`additionalContext`とCwdChangedの`systemMessage`の両方に出す。`doctor`は`[warn]`にする。台帳にマニフェストのハッシュは持たない。

#### Scenario: サービスを足したがenvを打っていない
- **WHEN** `port-keeper env`の後にマニフェストへ`mail`サービスを足し、`context --json`を実行する
- **THEN** `"env_stale":true`と`"env_stale_reason":"service mail has no port yet"`があり、案内文に`Run \`port-keeper env\` to refresh .env.local`が含まれ、出力にポート番号は無い

#### Scenario: envで解消する
- **WHEN** 続けて`port-keeper env`を実行してから`context --json`を実行する
- **THEN** `env_stale`は出ない

#### Scenario: ファイルを消した
- **WHEN** `.env.local`を削除して、CwdChangedイベントでフックを実行する
- **THEN** `systemMessage`に`refresh .env.local (.env.local does not exist)`が含まれる
