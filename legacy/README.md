# legacy/

本目录归档已经被新版本替代或不再主动维护的历史代码。保留它们是为了：

- 保留过去版本的实现思路与协议参考
- 保证仓库内的旧 issue / blog 链接不会 404
- 在新方案出问题时仍可回滚到老的工作流（例如 GitHub Actions 仍依赖 TS 版补丁工具）

| 目录 | 状态 | 替代方案 |
|------|------|----------|
| [client-rust/](client-rust/README.md) | 旧版 Rust 客户端 | [`apps/client`](../apps/client/README.md) |
| [client-patch-ts/](client-patch-ts/README.md) | TypeScript 版固件补丁工具 | [`tools/client-patch`](../tools/client-patch/README.md) |
| [gemini/](gemini/README.md) | 早期 Gemini Live 示例（Python + Rust + PyO3） | [`apps/gemini`](../apps/gemini/README.md) |
| [migpt/](migpt/README.md) | MiGPT 完美版示例（Node.js + Rust + Neon） | [`apps/chat`](../apps/chat/README.md) |
| [xiaozhi/](xiaozhi/README.md) | 小爱音箱接入小智 AI（Python + Rust） | 无（功能尚未在新版重写） |
| [stereo/](stereo/README.md) | 小爱音箱组立体声实验 | 无（实验项目） |
| [kws/](kws/README.md) | 自定义唤醒词脚本 | 无（实用脚本，非新版的一部分） |

## 不建议在 legacy 目录新增内容

新功能请优先放到 [`apps/`](../apps/)、[`pkg/`](../pkg/) 或 [`tools/`](../tools/)。本目录里的代码可能与新版本依赖、路径或协议冲突，自行使用前建议先查看对应 README 中的注意事项。
