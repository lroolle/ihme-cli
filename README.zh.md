# ihme

在终端管理 iCloud 隐藏邮件地址：注册一个，找得到，用完就停。

```bash
ihme new github.com          # 从候选中选一个
ihme list --search github    # 按标签、地址或备注查找
ihme deactivate              # 不带地址则从列表选择，最新在前
ihme share netflix           # 共享该地址的收件箱
ihme export -o backup.csv    # 导出备份
```

需要 iCloud+ 和已开启双重认证的 Apple ID。非官方工具：ihme 使用 icloud.com 网页版的接口，Apple 可随时改动或封禁。

[English](README.md)

## 安装

macOS（Apple 芯片）：

```bash
curl -fL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz -o ihme.tar.gz
tar -xzf ihme.tar.gz ihme
sudo install -m 755 ihme /usr/local/bin/ihme
```

macOS Intel、Linux、Windows（arm64、x86-64）见 [releases](https://github.com/lroolle/ihme-cli/releases/latest)，附 SHA-256 校验值。Go 用户：`go install github.com/lroolle/ihme-cli/cmd/ihme@latest`。

```bash
ihme auth login    # Apple ID、密码、双重认证码。保存会话，不保存密码和验证码。
ihme list
```

## 脚本

所有地址命令支持 `--json`，`--jq` 调用本机 jq 处理输出。无终端时不弹出任何提问。

```bash
ihme new github.com --json                        # 只列候选，不预留
ihme new github.com --address <candidate> --json  # 预留指定候选
ihme new github.com --yes --json                  # 预留第一个
ihme list --json --jq '.addresses[].hme'
```

退出码 2：需重新登录。1：其他错误。

## 共享一个收件箱

```bash
ihme share netflix    # 打印 key 和链接，只显示一次
ihme serve            # http://127.0.0.1:8025
```

key 只能访问该地址的邮件：收件箱和垃圾邮件，最近 30 天，纯文本。看不到其他地址和其他收件人，页面不加载外部内容。服务端用 app 专用密码经 IMAP 读信，不使用你的 iCloud 会话。`ihme share revoke netflix` 收回。[详细说明](docs/usage.md#share-an-inbox)（英文）。

## 给 agent 用

- `ihme agent "find my github address"`：内置助手，用你自己的模型 key（Anthropic、DeepSeek、任何 OpenAI 兼容接口）。
- `ihme agent --via codex "find my github address"`：用已登录的编程 agent，也支持 `claude`、`opencode`，不需要 API key。
- `npx skills add lroolle/ihme-cli -g`：给外部 agent 安装 [skill](skill/SKILL.md)。`ihme mcp`：stdio MCP 服务。

改动账户的操作都会先确认。偏好存在纯 Markdown 文件里，`ihme memory` 查看。agent 模式会把任务和结果发给你选的模型，普通命令不经过任何模型。

## 文档

以下文档为英文。

- [命令说明](docs/usage.md)：查找、创建、编辑、标签、共享、导出、JSON、错误
- [Agent 说明](docs/agents.md)：模型、确认、记忆、MCP
- [路线图与更新记录](ROADMAP.md)
- [安全说明](SECURITY.md)：本地存了什么，存在哪
- [参与贡献](CONTRIBUTING.md)

## 开发

```bash
make check    # vet、测试、构建
make site     # 构建网站到 _site/
```

基于 [Cobra](https://github.com/spf13/cobra) 和 [Charm](https://charm.sh/)。[MIT](LICENSE) 协议。与 Apple 无关。
