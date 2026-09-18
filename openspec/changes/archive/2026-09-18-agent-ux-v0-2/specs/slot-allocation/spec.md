# Spec Delta

## ADDED Requirements

### Requirement: Slot name from the git branch
`slot new --from-branch`は、現在のgitブランチ名（`git symbolic-ref --short -q HEAD`）を、小文字化し、`[a-z0-9]`以外の文字の連続を1つの`-`に置き換え、前後の`-`を除き、63バイトに切った名前にしてスロットを作らなければならない（MUST）。名前（または`--slot`）との併用、gitリポジトリの外、detached HEAD、使える文字が残らないブランチ名はエラーにする。導出した名前は`slot name "<name>" derived from branch "<branch>"`として表示する。

#### Scenario: ブランチ名の正規化
- **WHEN** ブランチ`Feature/Login`で`slot new --from-branch`を実行する
- **THEN** `slot name "feature-login" derived from branch "Feature/Login"`と`slot shop/feature-login created`が出る

#### Scenario: detached HEAD
- **WHEN** detached HEADで`slot new --from-branch`を実行する
- **THEN** `--from-branch: HEAD is detached; pass a slot name instead`で終了コード1
