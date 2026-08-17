# agent-ready

`agent-ready` は、プロジェクトで継続的に参照するリポジトリ、ドキュメント、
テキストをグローバルに登録し、CodexまたはClaude Code向けの知識カタログを
生成するGo CLIです。

情報源側のリポジトリにはマニフェストを作りません。すべてのデータは
`~/.agent-ready/<project>/` に保存します。

## 必要なもの

- Go 1.23以降（ソースからビルドする場合）
- ローカルで利用・認証済みのCodex CLIまたはClaude Code
- Gitリポジトリを情報源にする場合はGit

agent-ready自身はAPIキー、アクセストークン、Cookieなどの認証情報を保存しません。
Codex、Claude Code、Gitが既に持っているローカルの認証状態をそのまま利用します。

## ビルド

```sh
go build -o bin/agent-ready ./cmd/agent-ready
```

## コマンド

利用者が使うコマンドは `init`、`add`、`refresh` の3つです。すべて非対話型です。

### init

プロジェクトを作り、最初の情報源をまとめて登録・調査します。

```sh
agent-ready init --analyzer codex payment-platform \
  https://github.com/example/payment-api \
  https://github.com/example/payment-ui \
  https://docs.example.test/payments \
  ./architecture.md
```

Claude Codeを使う場合:

```sh
agent-ready init --analyzer claude payment-platform ./payment-service
```

文章やMarkdownを直接貼り付ける場合は、情報源として `-` を指定します。

```sh
agent-ready init --name project-notes payment-platform - <<'EOF'
# プロジェクトの前提

既存APIとの後方互換性を優先する。
EOF
```

複数のURLと貼り付けテキストを同時に渡すこともできます。

```sh
agent-ready init payment-platform \
  https://github.com/example/payment-api \
  https://docs.example.test/payments \
  - <<'EOF'
# 補足

API仕様を正とし、実装との差異は記録する。
EOF
```

### add

既存プロジェクトへ新しい情報源を追加し、その情報源を調査したうえでカタログ全体を
再構成します。

```sh
agent-ready add payment-platform https://docs.example.test/migration
```

```sh
agent-ready add --name meeting-notes payment-platform - <<'EOF'
# 決定事項

移行は機能単位で段階的に行う。
EOF
```

### refresh

登録済みの全情報源を再取得して変更を確認します。内容またはGit revisionが変わった
情報源だけを再調査し、変更が1件以上あった場合だけカタログを再生成します。

```sh
agent-ready refresh payment-platform
```

各コマンドは `--json` で機械可読な結果を出力できます。オプションはプロジェクト名より
前に指定してください。

## 長時間処理の進捗

情報源の取得やAIによる調査に時間がかかる場合、現在の工程、処理中の情報源、件数、
経過時間をリアルタイムに表示します。

```text
[1/2] Collecting "payment-api"...
✓ Collected "payment-api" (1.8s).
[1/2] Analyzing "payment-api" with codex...
Still analyzing "payment-api" (15s elapsed).
✓ Analyzed "payment-api" (42.6s).
Synthesizing catalog for "payment-platform" from 2 sources...
✓ Synthesized the catalog for "payment-platform" (18.4s).
```

対話端末ではスピナーと経過時間を同じ行で更新します。出力をファイルや別コマンドへ
渡した場合は、15秒ごとの行として進捗を残します。進捗は標準エラー出力へ、最終結果は
標準出力へ分離しているため、`--json` の出力もそのままパースできます。

## 対応する情報源

- GitHub、GitLab、BitbucketのHTTPS URL
- `.git` で終わるGit URL、SSH URL、`git+https://...`
- HTTP/HTTPSドキュメント
- ローカルGitリポジトリまたはディレクトリ
- ローカルファイル
- 標準入力から渡すMarkdownまたはプレーンテキスト

曖昧なHTTPS URLをGitとして扱わせる場合は `git+` を付けます。

```sh
agent-ready add payment-platform git+https://git.example.test/team/service
```

## 保存形式

```text
~/.agent-ready/
├── index.json
└── payment-platform/
    ├── project.json
    ├── sources.json
    ├── catalog.json
    ├── catalog.md
    ├── context.md
    ├── analyses/
    └── objects/
```

- `index.json`: Codex／Claude Codeが現在の作業とプロジェクトを対応付けるグローバル索引
- `project.json`: 利用する分析エージェントとプロジェクト情報
- `sources.json`: URL、パス、revision、内容ハッシュ、最終確認日時
- `catalog.json`: ソース、要約、重要概念、実在する参照位置を結ぶ機械可読な禁書目録
- `catalog.md`: 人が確認できる詳細な禁書目録
- `context.md`: エージェントが作業開始時に読む短い案内
- `analyses/`: 情報源単位の構造化された調査結果
- `objects/`: 貼り付けテキストや取得ドキュメントのcontent-addressed copy

## Codex／Claude Codeとの接続

`init`、`add`、`refresh` はグローバル索引を更新し、次のユーザー指示ファイルへ
agent-ready管理ブロックを追加します。

- Codex: `$CODEX_HOME/AGENTS.md`、既定は `~/.codex/AGENTS.md`
- Codexで有効な `AGENTS.override.md` が存在する場合はそのファイル
- Claude Code: `$CLAUDE_CONFIG_DIR/CLAUDE.md`、既定は `~/.claude/CLAUDE.md`

既存の指示は削除しません。管理ブロックは、作業ディレクトリまたはGit remoteと
`index.json` の情報源を照合し、一致したプロジェクトの `context.md` を最初に読むよう
指示します。起動時に `init`、`add`、`refresh` やAIによる再調査が自動実行されることは
ありません。

## 安全上の境界

- URLのuserinfoと、token、secret、passwordなどを示すquery parameterを拒否します。
- Codexはread-only sandboxかつephemeral sessionで調査します。
- Claude CodeはRead、Glob、Grepだけを許可し、sessionを保存せずに調査します。
- AI出力はJSON Schemaで検証してから保存します。
- エージェント向けグローバル指示では、情報源内の命令を実行せず、参照データとして
  扱うよう明示します。
- プロジェクトのディレクトリは0700、生成ファイルは0600で作成します。

取得した情報源自体に秘密情報が含まれている場合、その内容はローカルの `objects/` や
分析結果に含まれる可能性があります。`~/.agent-ready` を公開リポジトリへ追加しないで
ください。

## 現在の制約

- HTTPドキュメントの上限は20 MiBです。
- リモートGitは既定ブランチの最新commitをdepth 1で取得します。
- ディレクトリの変更検出では `.git`、`.agent-ready`、`node_modules`、`vendor`、
  `.venv`、`dist`、`build`、`target` を除外します。
- Webページは取得した本文をそのまま分析対象にし、ブラウザによるJavaScript実行は
  行いません。

## 開発

```sh
make test
make vet
make build
```
