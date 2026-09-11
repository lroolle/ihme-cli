# ihme

在终端里管理 iCloud 隐藏邮件地址。为一次注册生成一个地址，之后按标签找到它，不想再收信了就停掉。

```bash
ihme new github.com          # Apple 给几个候选地址，你选一个
ihme list --search github    # 按标签、地址或备注查找
ihme deactivate github.com   # 停止转发
ihme export -o backup.csv    # 导出一份自己的备份
```

需要 iCloud+ 订阅和开启了双重认证的 Apple ID。Apple 没有公开这个 API，ihme 调用的是 icloud.com 网页版用的同一套接口，Apple 随时可以改动或封掉它。

[English README](README.md)

## 安装

macOS，Apple 芯片：

```bash
curl -fL https://github.com/lroolle/ihme-cli/releases/latest/download/ihme_macOS_arm64.tar.gz -o ihme.tar.gz
tar -xzf ihme.tar.gz ihme
sudo install -m 755 ihme /usr/local/bin/ihme
```

macOS Intel、Linux、Windows 的 arm64 和 x86-64 版本在 [releases 页面](https://github.com/lroolle/ihme-cli/releases/latest)，附 SHA-256 校验值。有 Go 的话：`go install github.com/lroolle/ihme-cli/cmd/ihme@latest`。

然后登录，查看自己的地址：

```bash
ihme auth login    # Apple ID、密码、双重认证码。会话会保存在本地，密码和验证码不保存。
ihme list
```

## 脚本

所有地址相关的命令都支持 `--json`。`--jq` 用本机安装的 jq 处理结果。

```bash
ihme new github.com --json                        # 只生成候选，不预留
ihme new github.com --address <candidate> --json  # 预留其中一个
ihme new github.com --yes --json                  # 预留第一个
ihme list --json --jq '.addresses[].hme'
```

退出码 2 表示需要重新登录，1 是其他错误。

## 给 agent 用

ihme 可以给 agent 用，三种方式：

- `ihme agent "find my github address"`：内置助手，用你自己的模型 key，支持 Anthropic、DeepSeek 和任何 OpenAI 兼容接口。
- `ihme agent --via codex "find my github address"`：用你已经登录的编程 agent，也可以是 `claude` 或 `opencode`，不需要 API key。
- `npx skills add lroolle/ihme-cli -g`：把 [skill](skill/SKILL.md) 装给外部 agent。`ihme mcp` 是一个 stdio MCP 服务。

任何会改动账户的操作都会先问你。助手把偏好记在纯 Markdown 文件里，`ihme memory` 可以查看。agent 模式会把任务和结果发给你选的模型，普通命令不经过任何模型。

## 文档

以下文档为英文。

- [命令说明](docs/usage.md)：查找、创建、编辑、标签、导出、JSON、错误
- [Agent 说明](docs/agents.md)：模型、确认、记忆、MCP
- [路线图与更新记录](ROADMAP.md)
- [安全说明](SECURITY.md)：本地存了什么，存在哪
- [参与贡献](CONTRIBUTING.md)

## 开发

```bash
make check    # vet、测试、构建
make site     # 把网站构建到 _site/
```

基于 [Cobra](https://github.com/spf13/cobra) 和 [Charm](https://charm.sh/)。[MIT](LICENSE) 协议。与 Apple 无关。
