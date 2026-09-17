# pin-arbitration Specification

## Purpose
既存プロジェクトの移行で必要な固定番号を、横断管理の意味を壊さない形で許す。

## Requirements

### Requirement: Pins are arbitrated across projects
`pin`は台帳の全プロジェクトを対象に重複を検査し、既に貸出中・固定中の番号は所有者（project/slot/service）を示して拒否しなければならない（MUST）。

#### Scenario: Two projects want 3000
- **WHEN** プロジェクトAが3000を固定した後にプロジェクトBが3000を固定しようとする
- **THEN** Bの要求は`A/1/web`を名指しして拒否される

### Requirement: Pin constraints
固定は`--reason`必須、プロジェクトの`slot_default`スロットに限る、固定前に実在確認し台帳外のLISTENがあれば`--force`なしに拒否する（MUST）。

#### Scenario: Pin without a reason
- **WHEN** `--reason`なしで`pin`を実行する
- **THEN** 拒否され、固定が移行のための例外であることを伝える

#### Scenario: Pin in a non-default slot
- **WHEN** スロット2で`pin`を実行する
- **THEN** 拒否され、既定スロットでのみ固定できると伝える

### Requirement: Unpin returns to the pool
`unpin`は固定を解除し、同じ操作の中でプールから代わりの番号を貸さなければならない（MUST）。

#### Scenario: Unpin frees the number for others
- **WHEN** Aが`web`を`unpin`する
- **THEN** Bは3000を固定できる
