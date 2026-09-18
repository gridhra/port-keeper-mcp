# port-keeper-mcp

[English](README.md) | [日本語](README.ja.md) | **简体中文**

[![port-keeper-mcp MCP server – quality and maintenance score on Glama](https://glama.ai/mcp/servers/gridhra/port-keeper-mcp/badges/score.svg)](https://glama.ai/mcp/servers/gridhra/port-keeper-mcp)

一个管理开发端口的本地台账，并在其上提供 MCP 服务器，用 Go 编写。它给每个并行运行的编码 agent 一整套专属端口，让每个 agent 都能和其他 agent 同时把整套应用跑起来。

## 问题在哪里

编码 agent 要并行跑才划算：五个 agent，五个分支，五份工作副本。`git worktree` 给了每个 agent 一套自己的文件，却没有给它一套自己的端口。

对 Web 服务来说，事情就卡在这里。每份工作副本里都是同一份 `.env`，所以每份副本的开发服务器、API 和数据库要的都是同一批端口号。最先启动的 agent 拿到它们，其余的要么撞上 `address already in use`，要么更糟：在毫无察觉的情况下，对着另一个 agent 的服务器和数据库跑测试。

一个起不了自己那套应用的 agent，就无法检验自己的工作。它能写代码、跑静态分析、给纯函数跑单元测试，但跑不了端到端测试，也跑不了任何要经过数据库的测试：这些都需要应用在运行，也就需要每个 agent 一套环境，每套环境占有与服务数量相同的端口。缺了这一点，所有 agent 都只能排队等那唯一一套能用的环境。写个小的 CLI 工具不会察觉到这个问题，大型 Web 服务则一定会：工作在需要验证之前是并行的，到了验证就变成串行，并行跑 agent 的意义也就没有了。

agent 同时运行，彼此之间并不沟通，所以命名约定或者 wiki 上的一张端口范围表都撑不住。分配端口这件事必须像协议一样运作：所有 agent 都去问同一个地方，而且在几个 agent 同时来问时，它也能给出正确的答案。

## port-keeper 做什么

port-keeper 就是那个地方。

- **每个 agent 一套环境。** 每份工作副本得到一个*槽位*，每个槽位得到自己的端口块。在这台机器上的所有项目之间，任何两个租约都不会共用一个端口，即使几个 agent 在同一时刻来问也一样。
- **agent 去问，而不是去猜。** agent 通过 MCP 用服务的名字拿到 URL，hook 则在会话开始时把该槽位的端口放进 agent 的 shell。刚建好的 worktree 在拥有自己的槽位之前，拿不到主工作副本的端口。
- **没有需要一直运行的东西。** 没有守护进程，没有代理，没有网络监听。每条命令都是打开台账、干完活、退出。

项目在一份很小的清单里声明自己的服务（只写名字和环境变量名，绝不写数字）。port-keeper 从端口池中给每个*槽位*（项目的一份并行副本：每个工作副本一个，无论它是克隆还是 git worktree）分配一个端口块，把端口写进你的 `.env`，并从 CLI 或通过 MCP 回答“`shop/5/admin` 的 URL 是什么？”。你和你的编码 agent 都不必再记住任何端口号。它只在被调用时运行，除了台账和你在项目内指定给它的文件之外什么都不写，也从不打开网络监听。

完整设计见 [docs/DESIGN.md](docs/DESIGN.md)（日语）。

## 有意做得很小

在写 port-keeper 之前，我们调查过已有的工具。它们大致分为几类：把端口藏到主机名后面、必须常驻运行的本地代理；把端口管理当作会话、锁、消息等众多功能之一的 agent 协调平台；想替你启动开发服务器的包装器；不记录“谁占着哪个端口”的空闲端口查找器；以及既不懂并行工作副本、也不懂 `.env` 的面向 agent 的端口登记簿。其中有几个在各自擅长的事情上做得很好，但没有一个是“台账，并且只是台账”。

port-keeper 是能解决上述问题的最简单的工具：

- **只做一件事。** 它决定哪份工作副本的哪个服务拥有哪个端口，并在被问到时回答。启动服务器仍然是任务运行器的事；想要好看的主机名，那仍然是代理的事。port-keeper 可以给两者提供端口号，但不取代其中任何一个。
- **部件很少。** 一个静态二进制文件，一个 SQLite 文件，每个项目一份很小的清单。不需要守护进程、代理、DNS、证书，也不需要账号。
- **对 agent 暴露的面很小。** 一共 8 个 MCP 工具，默认启用其中 6 个。agent 一眼就能掌握整个接口，几乎不占上下文。
- **融入你现有的工具。** 它写出的只是普通的环境变量，去处是 `.env`、shell、direnv 或 mise。你的开发命令不用改。
- **想弃用也很容易。** 去掉 MCP 注册和 hook，再删掉二进制文件和台账文件即可。它写出的 `.env.local` 是普通文件，可以照常使用。

它不做的事情及其理由，都列在[非目标](#非目标)里。

## 安装

port-keeper 是一个没有运行时依赖的静态二进制文件。请把它放在 `PATH` 里：你的 shell、agent 的 hook 和 MCP 客户端调用的都是同一个 `port-keeper` 命令，所以安装一次三者都能用，机器上也始终只有一个版本。

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/gridhra/port-keeper-mcp/main/scripts/install.sh | sh
# Windows（PowerShell）。已在 CI 中构建并交叉编译，但尚未在真实的 Windows 机器上验证
irm https://raw.githubusercontent.com/gridhra/port-keeper-mcp/main/scripts/install.ps1 | iex
```

脚本会从最新的 [GitHub Release](https://github.com/gridhra/port-keeper-mcp/releases) 中选出与你的操作系统和 CPU 对应的压缩包；只有当它的 SHA-256 与该版本的 `checksums.txt` 一致时才会安装，并把 `port-keeper` 放到 `~/.local/bin`。它不会要求 `sudo`。用 `PORT_KEEPER_INSTALL_DIR` 指定安装目录，用 `PORT_KEEPER_VERSION` 固定版本。再运行一次就是更新：它只替换这一个二进制文件，不会碰你的台账和配置。

如果想手动完成同样的事，并确认压缩包确实是由本仓库的发布工作流、从打了标签的源码构建出来的：

```sh
gh release download --repo gridhra/port-keeper-mcp --pattern '*darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c --ignore-missing checksums.txt
gh attestation verify port-keeper_*_darwin_arm64.tar.gz --repo gridhra/port-keeper-mcp
tar -xzf port-keeper_*_darwin_arm64.tar.gz port-keeper && mv port-keeper ~/.local/bin/
```

如果有 Go 工具链（1.25 或更新）：

```sh
go install github.com/gridhra/port-keeper-mcp/cmd/port-keeper@latest
```

我们有意不提供容器镜像，也不提供 `npx` 启动器；原因见[非目标](#不提供容器镜像)。

## 快速开始

```sh
cd your-project
port-keeper init          # 写出 port-keeper.toml（只含名字），并把 .env.local 加入 gitignore
$EDITOR port-keeper.toml  # 项目需要的每个端口写一条 [[service]]
port-keeper env           # 租借一个端口块，写入 .env.local 中受管理的区块
port-keeper url web       # http://localhost:20000（加上 --open 可直接打开浏览器）
```

同一个项目的第二份工作副本会拿到属于它自己的槽位：

```sh
git worktree add ../your-project-hotfix -b hotfix && cd ../your-project-hotfix
port-keeper slot new      # 为这份工作副本租借一个新的端口块
port-keeper env           # 写出它自己的 .env.local；与第一份副本不会有任何冲突
```

让你的编码 agent 看到同样的视图：

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

然后按 [与编码 agent 协作 (Working with coding agents)](#与编码-agent-协作-working-with-coding-agents) 里的说明加上 hook 和那两行指示。

## 常见的变通办法为什么行不通

- **偏移量公式会失效。**“基准端口 + (槽位 − 1) × 1000”一直能用，直到某两个服务刚好相隔 4000；这时槽位 5 会和槽位 1 冲突，项目就被卡在四套环境上，同时还有成千个端口闲置。
- **跨项目冲突没人负责。** 笔记本上的每个项目都自己圈出一个千位区间，操作系统还会额外占掉几个约定端口（在 macOS 上，控制中心里的 AirPlay 接收器占用 5000 和 7000）。谁后启动谁就输。
- **数字会外泄。** 人们把 `localhost:3001` 粘贴到聊天、文档和 issue 里，就是因为不得不把它记住。而一个名字（`shop/3/admin`）什么都不泄露，并且在每台机器上都能解析。
- **agent 会瞎猜。** 需要起服务的编码 agent 会随手挑 8000 或 3000，把原本在那儿的东西踩掉。不如给它一个可以询问的工具。

## 使用场景

1. **第五份工作副本，无需任何算术**
   > “再给这个项目起一份副本，用于 hotfix 分支。”
   `port-keeper slot new hotfix` 租借一个新的端口块，`port-keeper env` 写入 `.env.local` 区块；你平时的 `mise run dev` 就能把所有东西起起来。既不会和另外四个槽位冲突，也不会和机器上的任何其他项目冲突。

2. **不用记任何东西就能打开正确的管理后台**
   > “打开槽位 3 的管理后台。”
   `port-keeper url shop/3/admin --open`，或者从你的 agent 调用 `resolve_url` MCP 工具。一次往返，一个 URL。

3. **让两套应用环境共用一个数据库**
   > “槽位 2 应该用槽位 1 的 MySQL 和 mail catcher。”
   `port-keeper slot new 2 --infra-from 1`。被标记为 `tier = "infra"` 的服务会解析到槽位 1 的端口；其他服务各用自己的。

4. **查出是谁占着你的端口**
   > “API 报 address already in use。”
   `port-keeper status` 会把台账和实际的监听情况做比对，并指出 PID。当监听进程的工作目录在这份工作副本之外（并且它不是容器运行时）时，该租约会被标记为 `hijacked`；否则就只是 `active`。它从不杀掉任何东西；要挪的话用 `port-keeper reassign <service>` 把那个服务换到一个空闲端口。

5. **迁移既有项目而不破坏书签**
   用 `port-keeper pin web=3000 admin=3001 api=8080 --reason "docs and bookmarks"` 把主环境保留在它历史上的端口号上。固定的端口会在台账中跨所有项目做仲裁，所以两个项目不可能同时声明 3000。其余每个槽位都改用端口池（用 `port-keeper url` 更新那些书签），而 `doctor` 会提醒你：一旦文档改成写 `port-keeper url`，就该 `unpin` 了。

## 与编码 agent 协作 (Working with coding agents)

port-keeper 对任何具体的 agent 都一无所知。所有客户端共用的契约就是两条命令：

- `port-keeper context --json`：我在哪里（项目、槽位、这份工作副本是否可以直接使用），以及接下来该做什么。不含端口号。
- `port-keeper env --format export --if-present`：当前槽位的环境，以 `export` 行输出；在项目之外则静默。

所有与具体客户端相关的东西都是架在这两条命令之上的适配器。`port-keeper hook claude` 就是 Claude Code hook 协议的适配器；它只占一个文件，其他 agent 也可以拥有自己的适配器，而不必改动内核。

### Claude Code

只需注册一次 MCP 服务器，作用于整个用户（配置里没有任何数字，只有命令）：

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

在 `~/.claude/settings.json` 里加上 hook。`SessionStart` 时它会把这个槽位的端口导出到 agent 的 shell 中，并把项目和槽位加入 Claude 的上下文；`CwdChanged` 时（Claude `cd` 进了另一份工作副本）它会把导出的端口换成那份工作副本所属槽位的端口。在项目之外，这条命令什么都不打印并以 0 退出，所以 hook 绝不会让会话失败。

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ],
    "CwdChanged": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ]
  }
}
```

### 告诉 agent 这个工具存在

只有 agent 知道它存在，这个工具才有用。放任自流的 agent 会去跑 `python -m http.server 8000` 或 `vite --port 3000`，把原本在那儿的东西踩掉。在你的**用户级**指示文件里写两行就够了（Claude Code 是 `~/.claude/CLAUDE.md`；其他 agent 有对应的文件），因为它对机器上的每个项目都生效：

下面这段指示是故意保留英文原文的——它是直接喂给 agent 的文本，改写会削弱其效力，请照原样复制：

```markdown
## Local ports

Local dev ports on this machine are managed by port-keeper (MCP server `port-keeper`).
Never choose a port number yourself and never start a server on an ad-hoc port.
To find where something runs, call the `resolve_url` tool
(or run `port-keeper url <project>/<slot>/<service>`).
If a task needs a new port, add a service to `port-keeper.toml` and run `port-keeper env`.
Refer to services by name (`shop/3/admin`), never by number, in docs, issues and chat.
```

为什么是用户级而不是按项目：真正会捣乱的服务，是 agent 在临时目录里、或者在还没有清单的项目里随手起的那些。项目级的文件永远触及不到它们。

如果你同时用好几个 agent（Claude Code、Codex、Cursor……），就把同一段内容放进每一个的全局指示里。MCP 注册是按客户端各自进行的；台账则是共享的。

## 保证

- **任何两个租约都不共用一个端口**，范围覆盖机器上的每个项目和槽位，包括被固定的历史端口。由台账里的 UNIQUE 约束以及每次分配都包在写事务中来强制保证。
- **只用端口池。** 分配绝不会越出配置的端口池（默认 20000–31999），该范围避开了约定端口、macOS 服务占用的端口以及操作系统的临时端口段。走出端口池的唯一途径是一次显式且有理由的 `pin`。
- **稳定。** 一个槽位会一直保有它的端口块，直到你释放它，重启也不变。
- **每份工作副本一个槽位。** 没有自己槽位的工作副本会被拒绝使用默认槽位的端口：`env`、`url`、`status` 和各个 MCP 工具都会要你先运行 `slot new`（或者，如果你就是想共用主槽位，则传 `--slot 1`）。正是这一点让刚刚 `git worktree add` 出来的副本不会在主工作副本的端口上起服务。
- **无监听、无守护进程。** 每条命令都是打开台账、干完活、退出。port-keeper 的任何东西都不可能通过网络访问到。
- **无密钥。** 台账里没有存放密码、令牌或连接字符串的列，`.env` 渲染也绝不会写进已被 git 跟踪的文件。

## 非目标

以下都是有意为之。在为其中某一项提 feature request 之前，请先读完本节；理由本身就是答案，而一个能与这些理由辩论的请求，远比只是把功能重述一遍的请求有价值。

### 不做反向代理 / 命名主机（`http://admin.shop.localhost`）

port-keeper 的目标是让你不必*去想*端口号，而不是让你永远*看不到*端口号。一旦 `port-keeper url shop/5/admin`（或 `resolve_url` 工具）给出 `http://localhost:23417`，事情就办完了；`--open` 甚至还替你打开它。

代理会引入一个所有槽位都依赖的常驻进程。它一停，所有环境同时变得不可达，这是比任何端口冲突都更糟的失效模式。它在 macOS 上需要特权端口（80/443），会把 TLS 和 WebSocket 转发拖进范围，并且与“port-keeper 从不监听”这条规则相矛盾。最后，浏览器按来源（origin）划分 cookie 和 local storage，*而来源包含端口*，所以不同的端口恰恰是把各槽位的会话隔开的东西；把它们藏到同一个主机名后面会消掉这层隔离。如果你还是想要好看的主机名，就把 `port-keeper env --format json` 喂给那些已经把这件事做得很好的代理（portless、localias、devenv）。port-keeper 不会长出这个功能。

### 不做进程管理（start / stop / restart / kill）

port-keeper 只负责预留一个号码并告诉你它是什么。谁在这个号码上起服务、什么时候起、在哪个 supervisor 下起，是你的任务运行器（`mise`、`just`、`direnv`、`docker compose`、IDE）的职责。port-keeper 会报告某个端口正被占用、以及占用它的 PID，但它绝不会杀进程：一个能被 AI agent 调用的工具，不应该有能力因为失误或提示注入而把另一套环境的开发服务器搞停。

### 不做守护进程

每条命令都打开 SQLite 台账，在一个写事务里干活，然后退出。并发的 CLI 或 MCP 进程不可能重复分配，因为 SQLite 会把写串行化，而 `port` 列是 UNIQUE 的。守护进程能换来 pub/sub 和带 TTL 的租约，代价是多出一条访问台账的路径（socket 或 HTTP），以及多一个需要一直保活的东西。需求并不需要它。

### 不做共享或同步的台账

台账列出的是*你这台机器*上哪些服务监听在哪些端口。这恰恰是攻击者最先要枚举的东西，所以它只存在本地、权限 0600，并且绝不同步、提交或上传。团队之间共享的是清单，里面只有名字和模板，没有数字。

### 台账里不放密钥

没有存放密码、令牌或连接字符串的列，将来也不会有。如果某个功能看起来需要密钥，那它属于你的密钥管理器。

### 不在端口池之外分配

`pin` 的存在只为迁移既有项目，别无他用。它要求填 `--reason`，每个项目限用一个槽位，并且会在台账中跨所有项目做仲裁；在你 `unpin` 之前，`doctor` 会一直念你。新项目永远不该 pin。

### 不提供容器镜像

port-keeper 必须直接看到属于你这台机器的四样东西：宿主机的网络栈（它通过实际 bind 来检查端口是否被占用）、宿主机的进程表（用 `lsof` 查出是谁在监听）、你的 shell 或 agent 当前所在的工作目录（它据此找到项目和 slot），以及你主目录下的台账。而容器存在的意义，恰恰就是隔离这四样东西。在容器里，port-keeper 探测到的是一个空的网络命名空间，会把所有端口都报告为空闲；它找不到你的项目；容器一退出，它就忘掉了所有租约。在 macOS 和 Windows 上，容器运行时本身跑在一个 Linux 虚拟机里，所以即使使用宿主机网络模式，也够不着你的开发服务器占用的端口。

在 Linux 上，挂载和各种参数可以勉强弥补其中一部分，但每加一项就是把宿主机的又一块交给容器，到最后隔离荡然无存。所以我们不提供镜像，port-keeper 也不会以 OCI 包的形式出现在任何地方。它只是一个静态文件，按[安装](#安装)一节的一条命令就能放进 `PATH`。

不提供 `npx` 启动器，原因与此相近。按需下载并启动服务器的启动器，能给你的 MCP 客户端一个服务器，却不能给你的 hook 和 shell 一个 `port-keeper` 命令，还会造成两个不同版本的二进制文件共用同一个台账。

### 如果你仍然想要其中某一项

请从上面的理由出发来提 issue，并说明其中哪一部分在你的场景里不成立。“这样会更方便”已经被考虑进去了；能改变答案的是我们没想到的失效模式，或者是非目标反而挡住了真正目标（不必去想端口号）的情形。

## 安全模型

port-keeper 是一个本地的、不联网的工具。台账是一张“机器上什么监听在哪里”的地图；它存放在 `~/.local/state/port-keeper/` 下，权限 0600，并且从不被传输。对于同一台机器上的另一个用户，这样就足够了。对于以*你的身份*运行的进程则不够，而且任何本地工具都做不到：这样的进程本来就能跑 `lsof -i`。port-keeper 对此所做的，是拒绝成为一张比 `lsof` 更丰富的地图（不存密钥、不存租户名、除了一个短标签之外不存任何描述），并且拒绝在没有被明确要求时向 agent 披露当前项目之外的信息。`port-keeper doctor` 会检查权限、`.gitignore`，以及 port-keeper 自己没有任何东西在监听。报告策略和涉及范围见 [SECURITY.md](SECURITY.md)。

## 参考

### 清单（`port-keeper.toml`）

放在仓库根目录，并且是打算提交进版本库的。它只包含名字和模板；数字只存在于你的本地台账里。

```toml
[project]
name = "shop"
block_size = 32          # 每个槽位的端口数；超出时会再追加一个端口块
slot_default = "1"       # 全新 checkout 解析到的槽位；也是唯一允许 pin 的槽位

[[service]]
name = "web"
env = "WEB_PORT"
proto = "http"
label = "Storefront"     # 可选，要短；送到 agent 之前会被截到 64 个字符

[[service]]
name = "admin"
env = "ADMIN_PORT"
proto = "http"

[[service]]
name = "api"
env = "API_PORT"
proto = "http"

[[service]]
name = "db"
env = "DB_PORT"
proto = "tcp"            # tcp 服务没有 URL
tier = "infra"           # 可通过 `slot new --infra-from` 共享

[[derive]]               # 由端口推导出来的值，绝不手工输入
env = "VITE_API_BASE"
value = "${url.api}"

[[derive]]
env = "ALLOWED_ORIGINS"
value = "${url.web},${url.admin}"

[render]
dotenv_path = ".env.local"   # 必须被 gitignore；`port-keeper env` 会拒绝已被跟踪的文件
dotenv_marker = "port-keeper" # 受管理区块的标记文本
host = "localhost"           # 所有 URL 中使用的主机名
```

模板变量：`${port.<service>}`、`${url.<service>}`（仅 http/https 服务）、`${slot}`、`${slot.infra}`、`${project}`、`${block.base}`。

### CLI

| 命令 | 作用 |
|---|---|
| `init [--name] [--here]` | 写出清单骨架和 `.gitignore` 条目。`--here` 写入当前目录而不是 git 顶层目录（monorepo 用） |
| `slot new [name] [--infra-from s] [--no-bind]` / `slot ls [--pins]` / `slot rm name [--force] [--cascade]` | 创建、列出、释放槽位。`new` 会把当前工作副本绑定到该槽位，除非它已经绑定到别的槽位 |
| `env [--format f] [--stdout] [--if-present]` | 渲染当前槽位；`dotenv`（默认）会重写 `.env.local` 里的标记区块。格式有：`dotenv`、`export`、`json`、`mise`、`direnv`、`claude-env`（`export` 的别名） |
| `url <service>` / `url <project>/<slot>/<service>` `[--open]` | 打印（或打开）一个 URL |
| `status` | 台账与实际监听情况的对比 |
| `context [--json] [--if-present]` | 这份工作副本的项目、槽位、就绪状态和下一步指引。不含数字。这是面向 agent 和 shell 的、与客户端无关的契约 |
| `gc [--yes]` | 列出（或释放）闲置超过 `stale_days` 的槽位，以及工作副本已不存在的槽位 |
| `doctor [--fix]` | 权限、`.gitignore`、端口池是否合理、过期槽位、固定端口、我们自己没有在监听 |
| `pin <service> <port> --reason t [--force]` 或 `pin web=3001 api=3002 --reason t` / `unpin <service>…` 或 `unpin --all` | 迁移辅助；见“保证”一节。批量形式可以用一条命令固定整套历史端口布局 |
| `reassign <service>` | 把一个服务挪到端口池中的另一个端口（在 `status` 报出 `hijacked` 之后） |
| `mcp` | 通过 stdio 提供 MCP 服务 |
| `hook claude` | 面向 `SessionStart` 和 `CwdChanged` 的 Claude Code 适配器：把 `context` + `env` 翻译成 `$CLAUDE_ENV_FILE` 和 hook JSON；在项目之外静默 |
| `version`（或 `--version`） | 打印版本 |
| `--slot <name>` | 全局参数：对指定槽位而不是解析出来的槽位执行操作 |

槽位的解析顺序是：`--slot`，然后是绑定到当前工作副本的槽位，然后是 `PORT_KEEPER_SLOT`，最后是清单里的 `slot_default`。当 shell 里遗留的 `PORT_KEEPER_SLOT` 与绑定不一致时，绑定优先，并会给出警告说明这一点。

### MCP 工具（8 个）

| 工具 | 作用 |
|---|---|
| `current_context` | 由工作目录解析出的项目和槽位，以及各服务的名字。不含数字（只读） |
| `resolve_url` | 某个服务的完整 URL，例如 `http://localhost:23417`。访问其他项目需要显式传 `project` 参数（只读） |
| `resolve_port` | 某个服务的纯端口号（只读） |
| `render_env` | 当前槽位的全部环境变量，按请求的格式返回。对还没有端口的服务会顺便租借端口（幂等） |
| `status` | 当前槽位的台账与实际情况对比：`leased`（没有任何东西在监听）、`active`、`stale` 或 `hijacked`，已知时附带占用进程的 PID（只读） |
| `slot_new` | 为一个新槽位租借端口块；如果名字已被占用、或工作副本已有绑定，则返回已存在的槽位（幂等） |
| `slot_release` | 释放一个槽位。只要还有东西在监听就拒绝。需要 `confirm:true`。**除非在配置中启用，否则不注册** |
| `list_all_projects` | 台账中每个项目和槽位的名字，不含数字。**除非在配置中启用，否则不注册** |

每个工具都接受一个可选的 `cwd`，这样在多个工作副本之间移动的会话总能拿到正确的槽位。只有 `resolve_url`、`resolve_port` 和 `render_env` 会带上端口号，而且只针对被问到的那一项；其余工具都用名字说话，以免数字在 agent 的对话记录里越堆越多。

### 配置（`~/.config/port-keeper/config.toml`）

全部可选：

```toml
stale_days = 30             # 顶层键；必须放在任何 [table] 之前

[pool]
ranges = [[20000, 31999]]
deny_ports = [27017, 28015, 29092]

[mcp]
enable_release = false      # 设为 true 才会注册 slot_release
enable_list_all = false     # 设为 true 才会注册 list_all_projects
```

未知的键会报错，这样放错位置的设置就不会悄无声息地不起作用。

## 开发

需要 Go 1.25 或更新版本（SQLite 驱动和 MCP SDK 都要求它；当 `GOTOOLCHAIN` 保持默认值时，`go` 会自动下载工具链）。

```sh
go test ./...                    # 单元测试、属性测试，以及进程内的 MCP 测试
go vet ./... && gofmt -l .
```

发布通过推送 `v*` 标签完成，步骤见 [RELEASING.md](RELEASING.md)（日语）。`sh scripts/install_test.sh` 用于测试安装脚本。

本项目用 OpenSpec（`openspec/`）管理变更提案；运行 `openspec list` 可以查看它们。

包结构：`internal/config`（端口池、路径）/ `internal/manifest` / `internal/ledger`（SQLite、分配）/ `internal/probe`（bind 检查、监听进程发现）/ `internal/gitx` / `internal/render`（dotenv、export、json、mise、direnv、claude-env）/ `internal/app`（解析、同步、状态、固定端口）/ `internal/cli`（命令；`hook_claude.go` 是 Claude Code 适配器）/ `internal/mcpserver`（stdio 服务器）/ `cmd/port-keeper`。

## 名字的由来

*keeper*（看守人、管钥匙的人）掌管钥匙和台账，告诉你哪扇门是哪扇；他不负责造门，也不替你开门。`-mcp` 后缀沿用 MCP 服务器的命名习惯；二进制文件就叫 `port-keeper`。

## 许可

MIT。见 [LICENSE](LICENSE)。
