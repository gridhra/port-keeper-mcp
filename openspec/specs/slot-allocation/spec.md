# slot-allocation Specification

## Purpose
マニフェストからスロットを作り、プールから連続ブロックを貸し、環境の数に導出式由来の上限を作らない。

## Requirements

### Requirement: Manifest holds names only
`port-keeper.toml`はサービス名・環境変数名・導出式・描画先だけを持ち、ポート番号を含んではならない（MUST NOT contain port numbers）。

#### Scenario: Skeleton
- **WHEN** `port-keeper init`が雛形を書く
- **THEN** 雛形に数字のポートは現れず、`.gitignore`に描画先が追加される

### Requirement: Contiguous block per slot
スロット作成時、プール（既定20000〜31999）からブロック長（既定32）の連続範囲をfirst-fitで貸さなければならない（MUST）。deny対象・既存リース・台帳外でLISTEN中の番号を含む候補は飛ばす。サービス数がブロック長を超えたら次のブロックを追加する。

#### Scenario: Fifth slot
- **WHEN** 4つのスロットが存在する状態で5つ目を作る
- **THEN** 既存と重ならないブロックが貸され、エラーにならない

### Requirement: Infra sharing
`--infra-from <slot>`で作られたスロットは`tier = "infra"`のサービスをリースせず、参照先スロットの番号で解決しなければならない（MUST）。参照されているスロットは`--cascade`なしに解放できない。

#### Scenario: Shared database
- **WHEN** スロット2が`--infra-from 1`で作られる
- **THEN** スロット2の`db`はスロット1の`db`と同じ番号になり、スロット2のリースはappサービス分だけになる

### Requirement: Slot resolution order
スロットは、明示引数 → 現在のgit worktreeに紐づくスロット → 環境変数`PORT_KEEPER_SLOT` → マニフェストの`slot_default`の順で決めなければならない（MUST）。環境変数はport-keeper自身が書き出すため、紐づけより上位に置いてはならない。

#### Scenario: Fresh checkout
- **WHEN** 作業コピーがどのスロットにも紐づいておらず、`slot_default`のスロットも存在しない
- **THEN** `slot_default`（既定`1`）が選ばれ、`env`が初回にそのスロットを作って紐づける

#### Scenario: Stale environment variable
- **WHEN** シェルに`PORT_KEEPER_SLOT=1`が残ったまま、この作業コピーを`slot new`でスロット2に紐づける
- **THEN** 以後の`env`はスロット2の番号を返し、環境変数が無視された旨の警告を出す

### Requirement: Unbound 作業コピー is not handed the default slot
作業コピーに紐づくスロットが無く、`slot_default`のスロットが別の作業コピーに紐づいている場合、`env`／`url`／`status`／MCPツールはそのスロットの番号を返してはならず（MUST NOT）、`slot new`を案内する。`--slot`で明示した場合は共有できる。

#### Scenario: Second 作業コピー
- **WHEN** 本線の作業コピーでスロット1を作った後、`git worktree add`した兄弟作業コピーで`env`を実行する
- **THEN** 本線の番号は書かれず、`slot new`を促すエラーになる

### Requirement: Slot name from the git branch
`slot new --from-branch`は、現在のgitブランチ名（`git symbolic-ref --short -q HEAD`）を、小文字化し、`[a-z0-9]`以外の文字の連続を1つの`-`に置き換え、前後の`-`を除き、63バイトに切った名前にしてスロットを作らなければならない（MUST）。名前（または`--slot`）との併用、gitリポジトリの外、detached HEAD、使える文字が残らないブランチ名はエラーにする。導出した名前は`slot name "<name>" derived from branch "<branch>"`として表示する。

#### Scenario: ブランチ名の正規化
- **WHEN** ブランチ`Feature/Login`で`slot new --from-branch`を実行する
- **THEN** `slot name "feature-login" derived from branch "Feature/Login"`と`slot shop/feature-login created`が出る

#### Scenario: detached HEAD
- **WHEN** detached HEADで`slot new --from-branch`を実行する
- **THEN** `--from-branch: HEAD is detached; pass a slot name instead`で終了コード1
